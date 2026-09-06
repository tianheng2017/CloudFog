package repository

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// ── 鉴权读取（B2-2 中间件消费；此处只做纯数据查找，状态/过期判定属中间件层）────────

// ListUserKeys 用户全部密钥（管理查看 07 §4.1 与自助列表共用数据源），按 id 倒序；
// 调用方负责脱敏投影（key_hash 绝不出 repository）。
func (r *Repository) ListUserKeys(ctx context.Context, userID int64) ([]model.APIKey, error) {
	var ks []model.APIKey
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("id DESC").Find(&ks).Error
	return ks, err
}

// CreateAPIKey 落库新密钥（key_hash 唯一；明文不出现在参数中）。
func (r *Repository) CreateAPIKey(ctx context.Context, k *model.APIKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

// CountUserKeys 用户密钥数（08 §3.3 单用户上限 50 判定）。
func (r *Repository) CountUserKeys(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.APIKey{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// APIKeySelfPatch 用户自助修改自己的 Key 字段（08 §3.3）。
type APIKeySelfPatch struct {
	Name           *string
	Status         *string
	IPWhitelist    *[]string
	ModelWhitelist *[]string
}

// Apply 转 map（[]string 字段序列化为 jsonb）。空片表示清空。
func (p APIKeySelfPatch) Apply() (map[string]any, error) {
	m := map[string]any{}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.Status != nil {
		m["status"] = *p.Status
	}
	for col, v := range map[string]*[]string{"ip_whitelist": p.IPWhitelist, "model_whitelist": p.ModelWhitelist} {
		if v != nil {
			b, err := json.Marshal(*v)
			if err != nil {
				return nil, err
			}
			m[col] = json.RawMessage(b)
		}
	}
	return m, nil
}

// UpdateAPIKeySelf 更新自己的 Key（归属强校验：id 且 user_id）。目标不存在/不属于本人返回 false。
func (r *Repository) UpdateAPIKeySelf(ctx context.Context, id, userID int64, m map[string]any) (bool, error) {
	if len(m) == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.APIKey{}).
		Where("id = ? AND user_id = ?", id, userID).Updates(m)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// DeleteAPIKeyByUser 删除自己的 Key（软删；鉴权查不到即即时失效）。目标不存在/不属于本人返回 false。
func (r *Repository) DeleteAPIKeyByUser(ctx context.Context, id, userID int64) (bool, error) {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.APIKey{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UserAllowedGroups 用户可用分组完整信息（自助 /me/groups 与 Key 创建分组选择，02 §3.4）。
func (r *Repository) UserAllowedGroups(ctx context.Context, userID int64) ([]model.Group, error) {
	ids, err := r.UserAllowedGroupIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []model.Group{}, nil
	}
	var gs []model.Group
	if err := r.db.WithContext(ctx).Where("id IN ? AND status = 'active'", ids).Order("id ASC").Find(&gs).Error; err != nil {
		return nil, err
	}
	return gs, nil
}

// APIKeyByHash 按 key_hash 精确查密钥。key_hash 唯一（uk_api_keys_key_hash）；
// 明文密钥不落库、不可按明文查。未命中返回 (nil, nil)。
func (r *Repository) APIKeyByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	var k model.APIKey
	err := r.db.WithContext(ctx).Where("key_hash = ?", hash).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// UserByID 按主键查用户。未命中返回 (nil, nil)。
func (r *Repository) UserByID(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GroupByID 按主键查分组（含 rate_multiplier / allowed_models 等路由与计费依据）。未命中返回 (nil, nil)。
func (r *Repository) GroupByID(ctx context.Context, id int64) (*model.Group, error) {
	var g model.Group
	err := r.db.WithContext(ctx).First(&g, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}
