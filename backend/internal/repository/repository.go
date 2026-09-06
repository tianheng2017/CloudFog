// Package repository 数据访问层（01 §6 依赖方向：service → repository → GORM）。
// 仅实现 bootstrap/种子与 MVP 所需的最小方法集；后续模块在各自实现时按需扩展。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"cloudfog/internal/model"
)

// Repository 聚合全部仓储（按实体拆分后可为接口，MVP 阶段保持单结构直连）。
type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

// DB 暴露底层连接（事务/复杂查询用；谨慎）。
func (r *Repository) DB() *gorm.DB { return r.db }

// ── users ─────────────────────────────────────────────────────

// CreateUser 创建用户（调用方保证 email/username 唯一）。
func (r *Repository) CreateUser(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// FindUserByEmail 查指定邮箱用户（含软删过滤）。
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ── builtin 种子（source='builtin' 幂等，02 §16 / 01 §10.4）────────────────

// UpsertBuiltinProvider 内置供应商种子：
//   - 不存在 → 创建（source=builtin）；
//   - 存在且 source=builtin → 用种子覆盖（配置演进）；
//   - 存在但 source≠builtin（运营/迁移改过）→ 跳过，绝不覆盖运营数据。
func (r *Repository) UpsertBuiltinProvider(ctx context.Context, seed *model.Provider) error {
	var cur model.Provider
	err := r.db.WithContext(ctx).Where("code = ?", seed.Code).First(&cur).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		if cur.Source.Source != "builtin" {
			return nil // 运营或迁移数据：不覆盖
		}
		cur.Name = seed.Name
		cur.Protocol = seed.Protocol
		cur.BaseURL = seed.BaseURL
		cur.AuthType = seed.AuthType
		cur.Capabilities = seed.Capabilities
		cur.BillPartialStream = seed.BillPartialStream
		cur.UsageReliable = seed.UsageReliable
		cur.Status = seed.Status
		return r.db.WithContext(ctx).Save(&cur).Error
	}
	seed.Source.Source = "builtin"
	return r.db.WithContext(ctx).Create(seed).Error
}

// UpsertBuiltinModel 内置模型种子（语义同上）。
func (r *Repository) UpsertBuiltinModel(ctx context.Context, seed *model.Model) error {
	var cur model.Model
	err := r.db.WithContext(ctx).Where("name = ?", seed.Name).First(&cur).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		if cur.Source.Source != "builtin" {
			return nil
		}
		cur.ProviderCode = seed.ProviderCode
		cur.DisplayName = seed.DisplayName
		cur.ContextWindow = seed.ContextWindow
		cur.MaxOutputTokens = seed.MaxOutputTokens
		cur.Capabilities = seed.Capabilities
		cur.BillingMode = seed.BillingMode
		cur.Status = seed.Status
		return r.db.WithContext(ctx).Save(&cur).Error
	}
	seed.Source.Source = "builtin"
	return r.db.WithContext(ctx).Create(seed).Error
}
