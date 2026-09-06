package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudfog/internal/model"
)

// ── 会话（07 §3.0 auth_sessions）与注册/登录数据访问 ──────────────

// CreateAuthSession 落库会话（token_hash 唯一；token 明文仅下发一次）。
func (r *Repository) CreateAuthSession(ctx context.Context, s *model.AuthSession) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// AuthSessionByTokenHash 按 token_hash 查会话；未命中返回 (nil, nil)。
func (r *Repository) AuthSessionByTokenHash(ctx context.Context, hash string) (*model.AuthSession, error) {
	var s model.AuthSession
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteAuthSessionByHash 吊销指定会话（登出）。
func (r *Repository) DeleteAuthSessionByHash(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", hash).Delete(&model.AuthSession{}).Error
}

// DeleteExpiredUserSessions 懒清理该用户过期会话（登录时触发，避免积压）。
func (r *Repository) DeleteExpiredUserSessions(ctx context.Context, userID int64, now time.Time) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND expires_at < ?", userID, now).Delete(&model.AuthSession{}).Error
}

// TouchAuthSession 更新最后活跃（距上次超过阈值才调用，避免每请求写库）。
func (r *Repository) TouchAuthSession(ctx context.Context, id int64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.AuthSession{}).
		Where("id = ?", id).Update("last_active_at", at).Error
}

// UserByLogin 登录凭据解析：email 或 username 精确匹配（唯一部分索引兜底重复）。
// 未命中返回 (nil, nil)。
func (r *Repository) UserByLogin(ctx context.Context, login string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).
		Where("lower(email) = lower(?) OR lower(username) = lower(?)", login, login).
		First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FirstActiveGroup 系统当前首个启用分组（注册用户默认分组来源）。
// config.default_group_id 接线前（B4 系统配置）以此为 MVP 兜底；无启用分组返回 (nil, nil)。
func (r *Repository) FirstActiveGroup(ctx context.Context) (*model.Group, error) {
	var g model.Group
	err := r.db.WithContext(ctx).Where("status = 'active'").Order("id ASC").First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// AllowUserGroup 幂等添加用户可用分组（注册默认分组自动可见，Key 创建即可选）。
func (r *Repository) AllowUserGroup(ctx context.Context, userID, groupID int64) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.UserAllowedGroup{UserID: userID, GroupID: groupID}).Error
}
