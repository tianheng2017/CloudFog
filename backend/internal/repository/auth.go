package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// ── 鉴权读取（B2-2 中间件消费；此处只做纯数据查找，状态/过期判定属中间件层）────────

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
