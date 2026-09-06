// Package bootstrap 首次引导（01 §10.4 / 02 §16 溯源语义）：
//   - 超管：由 config.Security.AdminEmail/AdminPassword 创建（source=user 语义但为引导属主）；
//   - 内置供应商与模型：source='builtin'，幂等 upsert（仅覆盖 builtin，不覆盖运营/迁移数据）。
//
// 幂等：可重复执行（启动/发布脚本/运维重跑均安全）。
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/pkg/password"
	"cloudfog/internal/repository"
)

// Config 引导参数（来自 config.Security + 少量固定种子定义）。
type Config struct {
	AdminEmail    string
	AdminPassword string
}

// Run 执行引导。可安全重复调用。
func Run(ctx context.Context, db *gorm.DB, cfg Config, log *slog.Logger) error {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return fmt.Errorf("bootstrap: 缺少超管凭据（CLOUDFOG_ADMIN_EMAIL/CLOUDFOG_ADMIN_PASSWORD）")
	}
	if log == nil {
		log = slog.Default()
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := repository.New(tx)
		if err := ensureSuperAdmin(ctx, repo, cfg); err != nil {
			return err
		}
		for _, p := range builtinProviders() {
			if err := repo.UpsertBuiltinProvider(ctx, p); err != nil {
				return fmt.Errorf("bootstrap: 内置供应商 %s 种子失败: %w", p.Code, err)
			}
		}
		for _, m := range builtinModels() {
			if err := repo.UpsertBuiltinModel(ctx, m); err != nil {
				return fmt.Errorf("bootstrap: 内置模型 %s 种子失败: %w", m.Name, err)
			}
		}
		log.Info("bootstrap 完成", "admin", cfg.AdminEmail)
		return nil
	})
}

// ensureSuperAdmin 幂等：存在即跳过（含运营改密/禁用场景——不回写覆盖）。
func ensureSuperAdmin(ctx context.Context, repo *repository.Repository, cfg Config) error {
	cur, err := repo.FindUserByEmail(ctx, cfg.AdminEmail)
	if err != nil {
		return fmt.Errorf("bootstrap: 查询超管失败: %w", err)
	}
	if cur != nil {
		return nil
	}
	hash, err := password.Hash(cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("bootstrap: 哈希超管密码失败: %w", err)
	}
	admin := &model.User{
		Email:        cfg.AdminEmail,
		Username:     cfg.AdminEmail,
		PasswordHash: &hash,
		PasswordAlgo: "argon2id",
		Role:         "super_admin",
		Status:       "active",
		Timezone:     "UTC",
	}
	admin.Source.Source = "user" // 引导创建的属主账号视作平台自建（02 §16：builtin|migration|user）
	admin.SourceID = nil         // 显式：引导自建账号无外部系统 ID（02 §16，SourceID 可空）
	return repo.CreateUser(ctx, admin)
}

func builtinProviders() []*model.Provider {
	return []*model.Provider{
		{
			Code: "openai", Name: "OpenAI", Protocol: "openai_compat",
			BaseURL: "https://api.openai.com/v1", AuthType: "bearer",
			Capabilities:      []string{"stream", "tools", "vision", "reasoning", "embedding", "image_gen"},
			BillPartialStream: true, UsageReliable: true, Status: "active",
		},
		{
			Code: "anthropic", Name: "Anthropic", Protocol: "anthropic",
			BaseURL: "https://api.anthropic.com", AuthType: "bearer",
			Capabilities:      []string{"stream", "tools", "vision", "reasoning"},
			BillPartialStream: true, UsageReliable: true, Status: "active",
		},
	}
}

func builtinModels() []*model.Model {
	return []*model.Model{
		{
			Name: "gpt-4o", ProviderCode: "openai", DisplayName: "GPT-4o",
			ContextWindow: 128000, MaxOutputTokens: 16384,
			Capabilities: []string{"stream", "tools", "vision", "reasoning"},
			BillingMode:  "token", Status: "active",
		},
		{
			Name: "claude-sonnet-4", ProviderCode: "anthropic", DisplayName: "Claude Sonnet 4",
			ContextWindow: 200000, MaxOutputTokens: 64000,
			Capabilities: []string{"stream", "tools", "vision", "reasoning"},
			BillingMode:  "token", Status: "active",
		},
	}
}
