package repository

import (
	"context"

	"cloudfog/internal/model"
)

// ── 渠道可见性/路由候选（B2-3 路由层消费；02 §3.4/§4.2/§4.3）────────────────

// RouteCandidate 某分组下一个可调度渠道及其供应商信息。
// Channel 携带调度字段与 Credentials（jsonb）；Provider 提供 protocol/base_url/auth_type
// 及能力集。BaseURL/AuthType 的渠道级覆盖解析归 Adapter 层（04 §4.1）。
type RouteCandidate struct {
	Channel  *model.Channel
	Provider *model.Provider
}

// RouteCandidatesByGroup 返回某分组可见且当前可调度的渠道（按 priority 升序、weight 降序）。
// 过滤语义（02 §4.2/§4.3）：渠道须 status='active' 且 schedulable=true；
// 其供应商须 status='active'（供应商停用则其下全部渠道退出调度）；
// 分组可见性 = channel_groups 有 (channel_id, group_id) 记录。
// 熔断/冷却/过载等运行时判定属调度层（B2-3），不在此过滤。
func (r *Repository) RouteCandidatesByGroup(ctx context.Context, groupID int64) ([]*RouteCandidate, error) {
	visible := r.db.Model(&model.ChannelGroup{}).Select("channel_id").Where("group_id = ?", groupID)

	var chans []*model.Channel
	err := r.db.WithContext(ctx).
		Where("id IN (?)", visible).
		Where("status = ? AND schedulable = ?", "active", true).
		Order("priority ASC, weight DESC, id ASC").
		Find(&chans).Error
	if err != nil {
		return nil, err
	}
	if len(chans) == 0 {
		return nil, nil
	}

	// 供应商独立读取（避免跨表 SELECT 的列名歧义），再按 provider_id 归并。
	pidSet := make(map[int64]struct{}, len(chans))
	for _, c := range chans {
		pidSet[c.ProviderID] = struct{}{}
	}
	provIDs := make([]int64, 0, len(pidSet))
	for pid := range pidSet {
		provIDs = append(provIDs, pid)
	}
	var provs []*model.Provider
	if err := r.db.WithContext(ctx).Where("id IN ?", provIDs).Find(&provs).Error; err != nil {
		return nil, err
	}
	provByID := make(map[int64]*model.Provider, len(provs))
	for _, p := range provs {
		provByID[p.ID] = p
	}

	out := make([]*RouteCandidate, 0, len(chans))
	for _, c := range chans {
		if p, ok := provByID[c.ProviderID]; ok {
			out = append(out, &RouteCandidate{Channel: c, Provider: p})
		}
		// 供应商缺失（理论上 FK 防得住）→ 跳过该渠道，不静默保留
	}
	return out, nil
}
