package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudfog/internal/model"
)

// ── 管理面数据操作（B3：07 §4 管理端接口的 repository 层）───────

// ErrNegativeBalance 手动调账扣减超出余额（余额恒非负，CK 兜底）。
var ErrNegativeBalance = errors.New("repository: 调账扣减超出可用余额")

// UserListFilter 用户列表筛选。
type UserListFilter struct {
	Role    string
	Status  string
	Keyword string // email/username 模糊
	GroupID *int64 // 存在于 user_allowed_groups 或 default_group
	Offset  int
	Limit   int
}

// ListUsers 用户分页列表（管理 07 §4.1）。limit 上限保护。
func (r *Repository) ListUsers(ctx context.Context, f UserListFilter) ([]model.User, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.User{})
	if f.Role != "" {
		q = q.Where("role = ?", f.Role)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if k := strings.TrimSpace(f.Keyword); k != "" {
		like := "%" + k + "%"
		q = q.Where("email ILIKE ? OR username ILIKE ?", like, like)
	}
	if f.GroupID != nil {
		q = q.Where(`id IN (SELECT user_id FROM user_allowed_groups WHERE group_id = ?)
			OR default_group_id = ?`, *f.GroupID, *f.GroupID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var users []model.User
	if err := q.Order("id ASC").Offset(f.Offset).Limit(f.Limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// UserProfilePatch 管理员可改的用户资料字段（07 §4.1 PATCH）。
type UserProfilePatch struct {
	Username         *string `json:"username,omitempty"`
	Phone            *string `json:"phone,omitempty"`
	Timezone         *string `json:"timezone,omitempty"`
	ConcurrencyLimit *int    `json:"concurrency_limit,omitempty"`
	DefaultGroupID   *int64  `json:"default_group_id,omitempty"`
}

// Apply 组装非零 updates（电话空串→NULL）。
func (p UserProfilePatch) Apply() (map[string]any, error) {
	m := map[string]any{}
	if p.Username != nil {
		if strings.TrimSpace(*p.Username) == "" {
			return nil, errors.New("repository: username 不能为空")
		}
		m["username"] = *p.Username
	}
	if p.Phone != nil {
		if strings.TrimSpace(*p.Phone) == "" {
			m["phone"] = nil
		} else {
			m["phone"] = *p.Phone
		}
	}
	if p.Timezone != nil {
		m["timezone"] = *p.Timezone
	}
	if p.ConcurrencyLimit != nil {
		if *p.ConcurrencyLimit < 0 {
			return nil, errors.New("repository: concurrency_limit 不能为负")
		}
		m["concurrency_limit"] = *p.ConcurrencyLimit
	}
	if p.DefaultGroupID != nil {
		m["default_group_id"] = *p.DefaultGroupID
	}
	return m, nil
}

// UpdateUserProfile 更新用户资料并递增 version（权限/风控敏感字段一律随变更失效缓存）。
func (r *Repository) UpdateUserProfile(ctx context.Context, id int64, p UserProfilePatch) error {
	m, err := p.Apply()
	if err != nil {
		return err
	}
	if len(m) == 0 {
		return nil
	}
	m["version"] = gorm.Expr("version + 1")
	m["updated_at"] = time.Now().UTC()
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(m).Error
}

// SetUserStatus 启用/禁用（禁用需 reason——reason 不落 users，由调用方写审计）。
// 每次变更递增 version 使该用户全部鉴权缓存失效（08 §3.2）。
func (r *Repository) SetUserStatus(ctx context.Context, id int64, status string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1"),
			"updated_at": time.Now().UTC()}).Error
}

// SetUserRole 修改角色（super_admin 专属，07 §4.1 / 08 §5.2）。
func (r *Repository) SetUserRole(ctx context.Context, id int64, role string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{"role": role, "version": gorm.Expr("version + 1"),
			"updated_at": time.Now().UTC()}).Error
}

// ReplaceAllowedGroups 全量替换用户可用分组（PUT 幂等，07 §4.1）。
func (r *Repository) ReplaceAllowedGroups(ctx context.Context, userID int64, groupIDs []int64) error {
	seen := map[int64]bool{}
	uniq := groupIDs[:0]
	for _, g := range groupIDs {
		if !seen[g] {
			seen[g] = true
			uniq = append(uniq, g)
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserAllowedGroup{}).Error; err != nil {
			return err
		}
		for _, g := range uniq {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&model.UserAllowedGroup{UserID: userID, GroupID: g}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ManualBalanceAdjust 手动调账（07 §4.1 / 4.3 billing.adjust）：
// 同事务内余额变动 + billing_ledger(adjust) 流水；amount>0 充值补发，<0 扣减（不可致负）。
// operator 与 reason 落入流水（审计另由调用方写）。
func (r *Repository) ManualBalanceAdjust(ctx context.Context, userID int64, amount decimal.Decimal, reason string, operatorID *int64) error {
	if amount.IsZero() {
		return errors.New("repository: 调账金额不能为 0")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// amount>0 增加 / <0 扣减，两者同为 balance = balance + ?；
		// 条件 balance + ? >= 0 保证余额恒非负（CK 兜底；READ COMMITTED 行锁串行化并发）。
		res := tx.Exec(
			`UPDATE user_balances SET balance = balance + ?, updated_at = now()
			 WHERE user_id = ? AND balance + ? >= 0`,
			amount, userID, amount)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrNegativeBalance
		}
		var bal string
		if err := tx.Raw(`SELECT balance::text FROM user_balances WHERE user_id = ?`, userID).Scan(&bal).Error; err != nil {
			return err
		}
		balDec, err := decimal.NewFromString(bal)
		if err != nil {
			return err
		}
		l := &model.BillingLedger{
			UserID: userID, Type: "adjust", Amount: model.Decimal{Decimal: amount},
			BalanceAfter: model.Decimal{Decimal: balDec}, OperatorID: operatorID,
			Description: "手动调账：" + reason,
		}
		return tx.Create(l).Error
	})
}

// InsertAudit 写入审计（只追加；before/after 已由调用方脱敏，client_ip 掩码由调用方处理）。
func (r *Repository) InsertAudit(ctx context.Context, a *model.AuditLog) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// GroupListFilter 分组筛选。
type GroupListFilter struct {
	Status string
	Offset int
	Limit  int
}

// ListGroups 分组分页列表。
func (r *Repository) ListGroups(ctx context.Context, f GroupListFilter) ([]model.Group, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Group{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	var gs []model.Group
	if err := q.Order("sort_order ASC, id ASC").Offset(maxInt(f.Offset, 0)).Limit(f.Limit).Find(&gs).Error; err != nil {
		return nil, 0, err
	}
	return gs, total, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
