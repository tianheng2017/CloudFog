package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"cloudfog/internal/repository"
)

// Reserve Redis 原子预扣（06 §3.2/§5.2）。
// 余额缓存键 balance:{user_id}（hash avail/frozen）只在请求期存在，结算提交后由 Engine 重置
// （DEL），下次预扣从 PG 重新加载——避免"缓存不失效导致二次消费"。
//
// 精度说明（P2 已登记）：Lua 经 HINCRBYFLOAT 用 double 运算 decimal 字符串，存在极小浮点误差。
// 该缓存仅作**并发闸门**（防超卖）而非账本：① 结算扣费始终走 PG numeric 条件更新（DB 是唯一账本）；
// ② 每个请求结算提交即 DEL 重载，误差不跨会话累积。浮点仅可能造成临界值少允/多允千亿分之一，
// 由 DB 条件更新最终兜底。若后续要消除，改为整数微单位或 Lua 内字符串十进制定点。
type Reserve struct {
	cli  *redis.Client
	repo *repository.Repository
}

func NewReserve(cli *redis.Client, repo *repository.Repository) *Reserve {
	return &Reserve{cli: cli, repo: repo}
}

func balanceKey(userID int64) string { return fmt.Sprintf("balance:%d", userID) }
func auditKey(requestID string) string {
	return "frozen:" + requestID
}

// ErrInsufficient 预扣失败：可用余额不足（402）。
var ErrInsufficient = errors.New("billing: 余额不足")

// reserveScript 与 06 §3.2 语义一致：未加载返回 -1，不足返回 0，成功 1。
var reserveScript = redis.NewScript(`
local bal = tonumber(redis.call('HGET', KEYS[1], 'avail') or '-1')
if bal < 0 then return -1 end
local amt = tonumber(ARGV[1])
if bal < amt then return 0 end
redis.call('HINCRBYFLOAT', KEYS[1], 'avail', -amt)
redis.call('HINCRBYFLOAT', KEYS[1], 'frozen', amt)
return 1
`)

// reclaimScript 死信补偿释放：把已超时冻结归还可用（取 min(冻结存量, 应还额)），
// 缓存键不存在返回 -1（调用方仅清理审计键）。归还不超量，杜绝重复释放造成余额虚增。
var reclaimScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 0 then return -1 end
local f = tonumber(redis.call('HGET', key, 'frozen') or '0')
local want = tonumber(ARGV[1])
local rel = want
if f < rel then rel = f end
if rel > 0 then
  redis.call('HINCRBYFLOAT', key, 'avail', rel)
  redis.call('HINCRBYFLOAT', key, 'frozen', -rel)
end
return rel
`)

// auditRec 审计键 frozen:<request_id> 的值（06 §3.3 L2）：归属与金额 + 写入时刻（reclaim 过期判定用）。
type auditRec struct {
	UserID int64  `json:"user_id"`
	Amount string `json:"amount"` // decimal 字符串
	At     int64  `json:"at"`     // unix 秒
}

// Reserve 预扣并登记审计键 frozen:<request_id>（EX 60min，06 §3.3 L2；仅审计/对账用，非释放依据）。
// 缓存未加载时回源 PG 一次后重试（防击穿：幂等 SET，同值覆盖无竞争损失）。
func (r *Reserve) Reserve(ctx context.Context, userID int64, requestID string, amount decimal.Decimal) error {
	if amount.IsNegative() {
		return errors.New("billing: 预扣金额为负")
	}
	if amount.IsZero() {
		return nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		res, err := reserveScript.Run(ctx, r.cli, []string{balanceKey(userID)}, amount.String()).Int()
		if err != nil {
			return err
		}
		switch res {
		case 1:
			// 审计键（TTL 60min > reclaim 阈值 45min，06 §3.3）；值带写入时刻供 reclaim 过期判定。
			rec, _ := json.Marshal(auditRec{UserID: userID, Amount: amount.String(), At: time.Now().UTC().Unix()})
			_ = r.cli.Set(ctx, auditKey(requestID), rec, 60*time.Minute).Err() // 审计非释放依据，失败忽略
			return nil
		case 0:
			return ErrInsufficient
		default: // -1：缓存未加载 → 回源 PG
			if err := r.reload(ctx, userID); err != nil {
				return err
			}
		}
	}
	return errors.New("billing: 预扣重试仍失败（缓存加载异常）")
}

// reload 从 PG 余额行回填缓存（无行按 0）。
func (r *Reserve) reload(ctx context.Context, userID int64) error {
	avail := "0"
	if b, err := r.repo.BalanceByUserID(ctx, userID); err != nil {
		return err
	} else if b != nil {
		avail = b.Balance.String()
	}
	return r.cli.HSet(ctx, balanceKey(userID), "avail", avail, "frozen", "0").Err()
}

// Reset 结算/退款提交后重置请求的预扣状态：删除余额缓存与审计键（下次预扣从 PG 重新加载）。
// ErrNil 视为成功（键不存在）。
func (r *Reserve) Reset(ctx context.Context, userID int64, requestID string) error {
	pipe := r.cli.Pipeline()
	pipe.Del(ctx, balanceKey(userID))
	pipe.Del(ctx, auditKey(requestID))
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

// Reclaim 死信/泄漏补偿（06 §3.3 L3，billing:reclaim handler 调用）：
// SCAN frozen:* 找出写入已超 olderThan 的预扣审计记录，逐条判定：
//   - billing_ledger 已有该 request_id 流水（settle/refund 已落地）→ 仅清理审计键；
//   - 否则视为泄漏冻结：把 min(缓存 frozen, 审计金额) 归还 avail（余额键已删则仅清审计键）。
//
// 阈值 45min > settle 总重试窗口 ~41min（06 §3.3），避免误释放仍在合法重试期的冻结。
// 归还量取 min 防重复释放虚增余额；审计值缺 At（旧格式）保守跳过不释放。
func (r *Reserve) Reclaim(ctx context.Context, olderThan time.Duration) (int, error) {
	var cursor uint64
	handled := 0
	var errs []error
	deadline := time.Now().UTC().Add(-olderThan)
	for {
		keys, next, err := r.cli.Scan(ctx, cursor, "frozen:*", 500).Result()
		if err != nil {
			return handled, fmt.Errorf("billing: reclaim 扫描失败: %w", err)
		}
		for _, k := range keys {
			requestID := strings.TrimPrefix(k, "frozen:")
			if requestID == "" || requestID == k {
				continue
			}
			raw, err := r.cli.Get(ctx, k).Bytes()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue
				}
				errs = append(errs, err)
				continue
			}
			var rec auditRec
			if err := json.Unmarshal(raw, &rec); err != nil || rec.UserID <= 0 || rec.Amount == "" || rec.At <= 0 {
				continue // 旧格式/损坏：保守跳过（不释放不删除）
			}
			if time.Unix(rec.At, 0).After(deadline) {
				continue // 未超阈值：仍在合法结算/重试窗口内
			}
			exists, err := r.repo.LedgerExistsByRequestID(ctx, requestID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !exists {
				if _, err := reclaimScript.Run(ctx, r.cli, []string{balanceKey(rec.UserID)}, rec.Amount).Result(); err != nil {
					errs = append(errs, err)
					continue
				}
			}
			_ = r.cli.Del(ctx, k).Err() // 已处理（释放或已有流水）均清理审计键
			handled++
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	return handled, errors.Join(errs...)
}

// Ping 连通性检查（readiness/健康）。
func (r *Reserve) Ping(ctx context.Context) error {
	return r.cli.Ping(ctx).Err()
}
