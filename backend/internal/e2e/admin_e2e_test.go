//go:build integration

// B3-1 管理端 E2E（真实 PG）：角色鉴权矩阵、用户禁用/启用（version 递增 + 审计）、
// 手动调账（限额/超余额拒绝）、角色变更（超管专属）、分组 CRUD 与用户分组授权。
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"cloudfog/internal/auth"
	"cloudfog/internal/httpserver"
	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

const (
	adminSalt     = "admin-salt-0123456789abcdef"
	adminMasterMK = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func supID(id int64) string { return fmt.Sprint(id) }

// seedAdminUser 建管理测试用户（active + 归属组 + 余额行）。
func seedAdminUser(t *testing.T, db *gorm.DB, tag, role string) *model.User {
	t.Helper()
	g := &model.Group{Name: tag + "-g", Status: "active"}
	if err := db.Create(g).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	u := &model.User{Email: fmt.Sprintf("%s@cf.local", tag), Username: tag,
		Status: "active", Role: role, DefaultGroupID: &g.ID}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	repo := repository.New(db)
	if err := repo.EnsureBalance(context.Background(), u.ID); err != nil {
		t.Fatalf("ensure balance: %v", err)
	}
	return u
}

func TestB3AdminManage(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := repository.New(db)

	n := time.Now().UnixNano()
	super := seedAdminUser(t, db, fmt.Sprintf("b3-sup-%d", n), "super_admin")
	admin := seedAdminUser(t, db, fmt.Sprintf("b3-adm-%d", n), "admin")
	target := seedAdminUser(t, db, fmt.Sprintf("b3-tgt-%d", n), "user")
	seedKey := func(u *model.User, token string) {
		k := &model.APIKey{UserID: u.ID, GroupID: *u.DefaultGroupID, Name: "adm-key", KeyPrefix: token,
			KeyHash: auth.HashKey(adminSalt, token), Status: "active"}
		if err := db.Create(k).Error; err != nil {
			t.Fatalf("seed key: %v", err)
		}
	}
	seedKey(super, "sk-b3-super")
	seedKey(admin, "sk-b3-admin")
	seedKey(target, "sk-b3-user")
	t.Cleanup(func() {
		for _, u := range []*model.User{super, admin, target} {
			_ = db.Exec("DELETE FROM billing_ledger WHERE user_id = ?", u.ID).Error
			_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", u.ID).Error
			_ = db.Exec("DELETE FROM api_keys WHERE user_id = ?", u.ID).Error
			_ = db.Unscoped().Delete(&model.User{}, u.ID).Error
		}
		_ = db.Exec("DELETE FROM audit_logs WHERE target_id IN (?,?,?) OR actor_id IN (?,?,?)",
			fmt.Sprint(super.ID), fmt.Sprint(admin.ID), fmt.Sprint(target.ID),
			supID(super.ID), supID(admin.ID), supID(target.ID)).Error
	})

	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	(&httpserver.Admin{Repo: repo, Store: repo, Salt: adminSalt, MK: adminMasterMK}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	do := func(token, method, path string, body any) *http.Response {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+"/api/v1/admin"+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	must := func(token, method, path string, body any, want int) {
		t.Helper()
		resp := do(token, method, path, body)
		defer resp.Body.Close()
		if resp.StatusCode != want {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			t.Fatalf("%s %s = %d, want %d body=%s", method, path, resp.StatusCode, want, raw)
		}
	}
	uPath := fmt.Sprintf("/users/%d", target.ID)

	t.Run("角色矩阵", func(t *testing.T) {
		must("sk-b3-user", http.MethodGet, "/users", nil, http.StatusForbidden) // user 角色 → 403
		must("sk-b3-admin", http.MethodGet, "/users", nil, http.StatusOK)
		must("sk-b3-super", http.MethodGet, "/users", nil, http.StatusOK)
		must("sk-b3-nope", http.MethodGet, "/users", nil, http.StatusUnauthorized)
	})

	t.Run("禁用/启用与审计", func(t *testing.T) {
		must("sk-b3-admin", http.MethodPost, uPath+"/disable", map[string]string{}, http.StatusBadRequest) // 无原因
		must("sk-b3-admin", http.MethodPost, uPath+"/disable", map[string]string{"reason": "测试禁用"}, http.StatusOK)
		var u model.User
		if err := db.First(&u, target.ID).Error; err != nil {
			t.Fatal(err)
		}
		if u.Status != "disabled" || u.Version <= 0 {
			t.Fatalf("禁用应 status=disabled 且 version 递增: %+v", u)
		}
		var audits int64
		if err := db.Model(&model.AuditLog{}).
			Where("action='user.disable' AND target_id = ?", fmt.Sprint(target.ID)).Count(&audits).Error; err != nil {
			t.Fatal(err)
		}
		if audits != 1 {
			t.Fatalf("应 1 条 user.disable 审计, got %d", audits)
		}
		must("sk-b3-admin", http.MethodPost, uPath+"/enable", nil, http.StatusOK)
		if err := db.First(&u, target.ID).Error; err != nil {
			t.Fatal(err)
		}
		if u.Status != "active" {
			t.Fatalf("启用应恢复 active: %+v", u)
		}
	})

	t.Run("角色变更仅超管", func(t *testing.T) {
		must("sk-b3-admin", http.MethodPatch, uPath+"/role", map[string]string{"role": "admin"}, http.StatusForbidden)
		must("sk-b3-super", http.MethodPatch, uPath+"/role", map[string]string{"role": "admin"}, http.StatusOK)
		var u model.User
		if err := db.First(&u, target.ID).Error; err != nil {
			t.Fatal(err)
		}
		if u.Role != "admin" {
			t.Fatalf("目标角色应为 admin: %+v", u)
		}
	})

	t.Run("手动调账", func(t *testing.T) {
		must("sk-b3-admin", http.MethodPost, uPath+"/balance", map[string]string{"amount": "88.5", "reason": "客服补偿"}, http.StatusOK)
		bal, err := repo.BalanceByUserID(ctx, target.ID)
		if err != nil || bal == nil {
			t.Fatalf("读余额失败: %v", err)
		}
		if bal.Balance.String() != "88.5" {
			t.Fatalf("余额应 88.5, got %s", bal.Balance)
		}
		// 扣减超余额 → 400；无原因 → 400；超限额 admin → 403；超限额 super → 200
		must("sk-b3-admin", http.MethodPost, uPath+"/balance", map[string]string{"amount": "-100", "reason": "误扣"}, http.StatusBadRequest)
		must("sk-b3-admin", http.MethodPost, uPath+"/balance", map[string]string{"amount": "1"}, http.StatusBadRequest)
		must("sk-b3-admin", http.MethodPost, uPath+"/balance", map[string]string{"amount": "999", "reason": "大额"}, http.StatusForbidden)
		must("sk-b3-super", http.MethodPost, uPath+"/balance", map[string]string{"amount": "999", "reason": "超管补发"}, http.StatusOK)
		var nLedger int64
		if err := db.Model(&model.BillingLedger{}).Where("user_id = ? AND type='adjust'", target.ID).Count(&nLedger).Error; err != nil {
			t.Fatal(err)
		}
		if nLedger != 2 {
			t.Fatalf("应 2 条 adjust 流水, got %d", nLedger)
		}
	})

	t.Run("分组 CRUD 与用户授权", func(t *testing.T) {
		// 建组
		resp := do("sk-b3-admin", http.MethodPost, "/groups",
			map[string]any{"name": fmt.Sprintf("b3-g-%d", time.Now().UnixNano()),
				"rate_multiplier": "1.5", "allowed_models": []string{"gpt-4o"}})
		var created struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&created)
		resp.Body.Close()
		if created.ID == 0 {
			t.Fatal("组创建失败")
		}
		// 授权用户可用组
		must("sk-b3-admin", http.MethodPut, uPath+"/groups", map[string]any{"group_ids": []int64{created.ID}}, http.StatusOK)
		gResp := do("sk-b3-admin", http.MethodGet, uPath+"/groups", nil)
		var gb struct {
			GroupIDs []int64 `json:"group_ids"`
		}
		_ = json.NewDecoder(gResp.Body).Decode(&gb)
		gResp.Body.Close()
		if len(gb.GroupIDs) != 1 || gb.GroupIDs[0] != created.ID {
			t.Fatalf("授权组不符: %+v", gb)
		}
		// 解除授权后再删组（软删）
		must("sk-b3-admin", http.MethodPut, uPath+"/groups", map[string]any{"group_ids": []int64{}}, http.StatusOK)
		must("sk-b3-admin", http.MethodDelete, fmt.Sprintf("/groups/%d", created.ID), nil, http.StatusNoContent)
		// 软删后访问 → 404
		must("sk-b3-admin", http.MethodPatch, fmt.Sprintf("/groups/%d", created.ID),
			map[string]string{"name": "x"}, http.StatusNotFound)
	})
}
