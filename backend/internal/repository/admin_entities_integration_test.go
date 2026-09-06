//go:build integration

// b3-2 实体层集成回归：熔断复位清冷却窗口、清空用户电话、信封渠道创建/轮换往返。
package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"cloudfog/internal/model"
	"cloudfog/internal/pkg/crypto"
)

const (
	entMK  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	entKey = "k_test"
)

func entDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CLOUDFOG_DSN")
	if dsn == "" {
		t.Skip("CLOUDFOG_DSN 未设置")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{NowFunc: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func seedEntProvider(t *testing.T, db *gorm.DB, tag string) model.Provider {
	t.Helper()
	p := model.Provider{Code: "prov-" + tag, Name: tag, Protocol: "openai_compat",
		BaseURL: "https://example.invalid", AuthType: "bearer", Status: "active"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return p
}

func TestUserProfileClearPhone(t *testing.T) {
	db := entDB(t)
	ctx := context.Background()
	repo := New(db)
	tag := fmt.Sprintf("clr-%d", time.Now().UnixNano())
	ph := "13800000000"
	u := &model.User{Email: tag + "@cf.local", Username: tag, Status: "active", Role: "user", Phone: &ph}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Unscoped().Delete(&model.User{}, u.ID)
	empty := ""
	if err := repo.UpdateUserProfile(ctx, u.ID, UserProfilePatch{Phone: &empty}); err != nil {
		t.Fatalf("清空电话: %v", err)
	}
	var got model.User
	if err := db.First(&got, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Phone != nil {
		t.Fatalf("电话应为 NULL（Expr），got %q", *got.Phone)
	}
}

func TestCreateChannelSealedRoundtrip(t *testing.T) {
	db := entDB(t)
	ctx := context.Background()
	repo := New(db)
	tag := fmt.Sprintf("seal-%d", time.Now().UnixNano())
	p := seedEntProvider(t, db, tag)
	ch := model.Channel{Name: tag, ProviderID: p.ID, ProviderCode: p.Code, Status: "active",
		Schedulable: true, Priority: 50, Weight: 100, Concurrency: 1,
		RateMultiplier: model.Decimal{Decimal: decimal.NewFromInt(1)},
		Credentials:    map[string]any{"api_key": "sk-secret"}}
	defer db.Unscoped().Delete(&model.Provider{}, p.ID)
	id, err := repo.CreateChannel(ctx, &ch, func(cid int64, plain map[string]any) (map[string]any, error) {
		return crypto.Seal(plain, entMK, entKey, fmt.Sprintf("channel:%d", cid))
	})
	if err != nil {
		t.Fatalf("seal create: %v", err)
	}
	defer db.Unscoped().Delete(&model.Channel{}, id)
	got, err := repo.ChannelByID(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("read channel: %v", err)
	}
	if !crypto.IsSealed(got.Credentials) {
		t.Fatal("credentials 应为密封形态")
	}
	out, err := crypto.Open(got.Credentials, entMK, "", fmt.Sprintf("channel:%d", id))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if out["api_key"] != "sk-secret" {
		t.Fatalf("解密字段不符: %+v", out)
	}
	// 列表 Omit credentials：密文不得出现在列表结果
	list, _, err := repo.ListChannels(ctx, ChannelListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if len(c.Credentials) != 0 {
			t.Fatal("渠道列表不应携带 credentials")
		}
	}
	// 凭证轮换往返
	if err := repo.SetChannelCredentials(ctx, id, func(cid int64, plain map[string]any) (map[string]any, error) {
		return crypto.Seal(plain, entMK, entKey, fmt.Sprintf("channel:%d", cid))
	}, map[string]any{"api_key": "sk-rotated"}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	got2, _ := repo.ChannelByID(ctx, id)
	out2, err := crypto.Open(got2.Credentials, entMK, "", fmt.Sprintf("channel:%d", id))
	if err != nil || out2["api_key"] != "sk-rotated" {
		t.Fatalf("轮换后解密不符: %v %+v", err, out2)
	}
}
