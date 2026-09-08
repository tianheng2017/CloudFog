// Package router 模型解析与路由（03 §4.2 / 05：调度与容灾）。
// 职责：把"对外模型名 + 分组可见渠道"解析为有序可执行候选（含上游映射名），
// 供请求编排层逐个尝试（故障转移循环 03 §5）。熔断/冷却等状态变化以 DB 行为准，
// 每次请求重读目录（B1 无进程内状态机；05 的监控任务仍按计划在后续演进）。
package router

import (
	"context"
	"errors"
	"time"

	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

// Catalog 路由层所需数据访问（*repository.Repository 实现；consumer-side 接口便于单测）。
type Catalog interface {
	ModelByName(ctx context.Context, name string) (*model.Model, error)
	CurrentModelPrice(ctx context.Context, modelID int64, at time.Time) (*model.ModelPrice, error)
	MappingRows(ctx context.Context, alias string) ([]model.ModelMapping, error)
	RouteCandidatesByGroup(ctx context.Context, groupID int64) ([]*repository.RouteCandidate, error)
}

// ErrModelNotFound 请求的对外模型在目录中不存在（01 §5 映射缺失 → 404 model_not_found）。
var ErrModelNotFound = errors.New("router: 模型不存在")

// ResolvedModel 一次模型解析结果：规格 + at 时刻生效价格（可能无生效价，价格为 0 语义由调用方处理）。
type ResolvedModel struct {
	Model *model.Model
	Price *model.ModelPrice // effective_from<=now 最近生效；nil = 无生效价
}

// Router 路由决策器。
type Router struct {
	cat Catalog
	now func() time.Time
}

func NewRouter(cat Catalog) *Router {
	return &Router{cat: cat, now: time.Now}
}

// Resolve 解析模型并固化价格（PriceSnapshot 由调用方据此构建，避免调价漂移 03 §4.2）。
func (r *Router) Resolve(ctx context.Context, name string) (*ResolvedModel, error) {
	m, err := r.cat.ModelByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrModelNotFound
	}
	p, err := r.cat.CurrentModelPrice(ctx, m.ID, r.now().UTC())
	if err != nil {
		return nil, err
	}
	return &ResolvedModel{Model: m, Price: p}, nil
}

// Attempt 单个可执行尝试（故障转移循环的一个候选项，03 §5）。
type Attempt struct {
	ModelName     string                     // 出口层当前模型名（fallback 链元素）
	Candidate     *repository.RouteCandidate // 渠道 + 供应商
	UpstreamModel string                     // 上游实际模型名（映射后；无映射=透传同名）
}

// Plan 生成有序候选：模型链 × 可见且当前可用的渠道（按渠道 priority/weight 序），
// 逐个解析上游模型名。models 为已展开的去重降级链（由调用方按 05 §9 优先级合并后传入）。
func (r *Router) Plan(ctx context.Context, groupID int64, models []string) ([]*Attempt, error) {
	chans, err := r.cat.RouteCandidatesByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 || len(chans) == 0 {
		return nil, nil
	}
	now := r.now().UTC()
	// 映射按 alias 记忆化：同一 alias 在「模型链 × 渠道」双层循环中会被重复查询 N 次
	// （2026-09-08 优化：M×N 次 DB 往返 → M 次）。
	memo := make(map[string][]model.ModelMapping, len(models))
	rowsFor := func(alias string) ([]model.ModelMapping, error) {
		if rows, ok := memo[alias]; ok {
			return rows, nil
		}
		rows, err := r.cat.MappingRows(ctx, alias)
		if err != nil {
			return nil, err
		}
		memo[alias] = rows
		return rows, nil
	}
	var attempts []*Attempt
	for _, name := range models {
		for _, c := range chans {
			if !usableAt(c.Channel, now) {
				continue
			}
			rows, err := rowsFor(name)
			if err != nil {
				return nil, err
			}
			up := resolveUpstream(rows, c.Channel.ID, name)
			attempts = append(attempts, &Attempt{ModelName: name, Candidate: c, UpstreamModel: up})
		}
	}
	return attempts, nil
}

// resolveUpstream 映射解析：渠道级精确 → 全局精确 → 原样透传（02 §5.3）。
// 通配（alias 含 *）依赖模式查询，当前版本按精确匹配实现并透传兜底（演进见注释）。
func resolveUpstream(rows []model.ModelMapping, channelID int64, alias string) string {
	for _, row := range rows {
		if row.ChannelID != nil && *row.ChannelID == channelID {
			return row.UpstreamModel
		}
	}
	for _, row := range rows {
		if row.ChannelID == nil {
			return row.UpstreamModel
		}
	}
	return alias // 无映射：原样透传
}

// usableAt 当前时刻渠道是否可用（状态机字段取自 DB，05 §6 运行时维护）。
// 开放（open）或处于任一冷却窗口（熔断开启/限流/过载/临时停排/过期）的渠道不参与本次调度。
func usableAt(c *model.Channel, now time.Time) bool {
	if c == nil || c.Status != "active" || !c.Schedulable {
		return false
	}
	if c.CircuitState == "open" {
		return false
	}
	if c.CircuitOpenUntil != nil && now.Before(*c.CircuitOpenUntil) {
		return false
	}
	if c.RateLimitResetAt != nil && now.Before(*c.RateLimitResetAt) {
		return false
	}
	if c.OverloadUntil != nil && now.Before(*c.OverloadUntil) {
		return false
	}
	if c.TempUnschedulableUntil != nil && now.Before(*c.TempUnschedulableUntil) {
		return false
	}
	if c.ExpiresAt != nil && now.After(*c.ExpiresAt) {
		return false
	}
	return true
}
