//go:build integration

// B3-3-2 认证基座 E2E（真实 PG）：注册（默认分组/argon2id/重复 409）、
// 登录（错误密码 401、失败 5 次锁定 15min、成功后下发 sess_ token + Set-Cookie）、
// 登出吊销会话（token 再登出幂等、库中 token_hash 删除）。
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"cloudfog/internal/auth"
	"cloudfog/internal/httpserver"
	"cloudfog/internal/model"
	"cloudfog/internal/payment"
	"cloudfog/internal/pkg/password"
	"cloudfog/internal/repository"
	"cloudfog/internal/task"
)

func TestPortalAuthManage(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := repository.New(db)

	n := time.Now().UnixNano()
	salt := "portal-test-salt"

	// 种子：一个 active 分组（注册默认分组来源）
	g := &model.Group{Name: fmt.Sprintf("b33-g-%d", n), Status: "active"}
	if err := db.Create(g).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	(&httpserver.Portal{Repo: repo, Salt: salt, RegistrationEnabled: true}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	do := func(token, method, path string, body any) *http.Response {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, rd)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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

	cleanupUser := func(uid int64) {
		_ = db.Exec("DELETE FROM user_allowed_groups WHERE user_id = ?", uid).Error
		_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", uid).Error
		_ = db.Exec("DELETE FROM api_keys WHERE user_id = ?", uid).Error
		_ = db.Unscoped().Delete(&model.User{}, uid).Error
	}
	t.Cleanup(func() { _ = db.Unscoped().Delete(&model.Group{}, g.ID).Error })

	user := fmt.Sprintf("b33-u-%d", n)
	email := user + "@t.cn"
	registerBody := map[string]any{"username": user, "email": email, "password": "S3cret-2026"}

	t.Run("注册-默认分组与哈希", func(t *testing.T) {
		reg := do("", http.MethodPost, "/api/v1/auth/register", registerBody)
		raw, _ := io.ReadAll(reg.Body)
		reg.Body.Close()
		if reg.StatusCode != http.StatusOK {
			t.Fatalf("register = %d %s", reg.StatusCode, raw)
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &created); err != nil || created.ID == 0 {
			t.Fatalf("parse register: %s", raw)
		}
		t.Cleanup(func() { cleanupUser(created.ID) })
		var u model.User
		if err := db.First(&u, created.ID).Error; err != nil {
			t.Fatal(err)
		}
		if u.Status != "active" || u.Role != "user" || u.DefaultGroupID == nil {
			t.Fatalf("注册默认状态/分组异常: %+v", u)
		}
		if u.PasswordHash == nil || !strings.HasPrefix(*u.PasswordHash, "$argon2id$") {
			t.Fatalf("注册应 argon2id 哈希")
		}
		// 默认分组须为启用的首个分组，且已进可见集合（共享 dev DB 有更早 active 组，故不绑定本 g）
		var dg model.Group
		if err := db.First(&dg, *u.DefaultGroupID).Error; err != nil || dg.Status != "active" {
			t.Fatalf("默认分组应存在且 active: err=%v g=%+v", err, dg)
		}
		var allowed int64
		_ = db.Model(&model.UserAllowedGroup{}).Where("user_id = ? AND group_id = ?", created.ID, *u.DefaultGroupID).Count(&allowed)
		if allowed != 1 {
			t.Fatalf("默认分组应写入可见集合")
		}
		// 重复用户名 → 409
		must("", http.MethodPost, "/api/v1/auth/register", map[string]any{
			"username": user, "email": "other-" + email, "password": "S3cret-2026",
		}, http.StatusConflict)
	})

	t.Run("登录-错误密码与锁定", func(t *testing.T) {
		lockUser := fmt.Sprintf("b33-lk-%d", n)
		lockReg := do("", http.MethodPost, "/api/v1/auth/register",
			map[string]any{"username": lockUser, "email": lockUser + "@t.cn", "password": "S3cret-2026"})
		var lk struct {
			ID int64 `json:"id"`
		}
		rawL, _ := io.ReadAll(lockReg.Body)
		lockReg.Body.Close()
		if lockReg.StatusCode != http.StatusOK {
			t.Fatalf("register lock user = %d %s", lockReg.StatusCode, rawL)
		}
		_ = json.Unmarshal(rawL, &lk)
		t.Cleanup(func() { cleanupUser(lk.ID) })
		// 错误密码 → 401（统一文案）
		for i := 0; i < 5; i++ {
			bad := do("", http.MethodPost, "/api/v1/auth/login",
				map[string]any{"login": lockUser, "password": "wrong-pass"})
			bad.Body.Close()
			if bad.StatusCode != http.StatusUnauthorized {
				t.Fatalf("第 %d 次错误密码应 401, got %d", i+1, bad.StatusCode)
			}
		}
		// 第 6 次（错密）→ 429 锁定（15min）；第 7 次（正密）同样被锁
		locked := do("", http.MethodPost, "/api/v1/auth/login",
			map[string]any{"login": lockUser, "password": "wrong-pass-6"})
		locked.Body.Close()
		if locked.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("5 次失败后第 6 次应 429, got %d", locked.StatusCode)
		}
		locked7 := do("", http.MethodPost, "/api/v1/auth/login",
			map[string]any{"login": lockUser, "password": "S3cret-2026"})
		locked7.Body.Close()
		if locked7.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("锁定窗口内正确密码也应 429, got %d", locked7.StatusCode)
		}
	})

	t.Run("登录-成功下发与登出吊销", func(t *testing.T) {
		okUser := fmt.Sprintf("b33-ln-%d", n)
		reg := do("", http.MethodPost, "/api/v1/auth/register",
			map[string]any{"username": okUser, "email": okUser + "@t.cn", "password": "S3cret-2026"})
		var ou struct {
			ID int64 `json:"id"`
		}
		raw, _ := io.ReadAll(reg.Body)
		reg.Body.Close()
		if reg.StatusCode != http.StatusOK {
			t.Fatalf("register = %d", reg.StatusCode)
		}
		_ = json.Unmarshal(raw, &ou)
		t.Cleanup(func() { cleanupUser(ou.ID) })

		lg := do("", http.MethodPost, "/api/v1/auth/login",
			map[string]any{"login": okUser, "password": "S3cret-2026"})
		lraw, _ := io.ReadAll(lg.Body)
		cookies := lg.Cookies()
		lg.Body.Close()
		if lg.StatusCode != http.StatusOK {
			t.Fatalf("login = %d %s", lg.StatusCode, lraw)
		}
		var got struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(lraw, &got); err != nil || !strings.HasPrefix(got.Token, "sess_") {
			t.Fatalf("登录应返回 sess_ token: %s", lraw)
		}
		hasCookie := false
		for _, ck := range cookies {
			if ck.Name == "cloudfog_session" {
				hasCookie = true
			}
		}
		if !hasCookie {
			t.Fatal("登录应 Set-Cookie cloudfog_session（HttpOnly）")
		}
		// 登出吊销会话：token 从库中删除
		var cntBefore int64
		_ = db.Model(&model.AuthSession{}).Where("user_id = ?", ou.ID).Count(&cntBefore)
		if cntBefore != 1 {
			t.Fatalf("登录应产生 1 条会话, got %d", cntBefore)
		}
		must(got.Token, http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent)
		var cntAfter int64
		_ = db.Model(&model.AuthSession{}).Where("user_id = ?", ou.ID).Count(&cntAfter)
		if cntAfter != 0 {
			t.Fatalf("登出应吊销会话, got %d 条残留", cntAfter)
		}
		// 使用吊销后的 token 访问受保护路由 → 401 会话不存在（以 b33-3 /me 挂载前用 logout 幂等验证鉴权链）
		again := do(got.Token, http.MethodPost, "/api/v1/auth/logout", nil)
		again.Body.Close()
		if again.StatusCode != http.StatusUnauthorized {
			t.Fatalf("吊销 token 应 401, got %d", again.StatusCode)
		}
	})
}

func TestPortalMeAndKeys(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := repository.New(db)
	n := time.Now().UnixNano()
	salt := "portal-me-salt"

	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	(&httpserver.Portal{Repo: repo, Salt: salt, RegistrationEnabled: true}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	do := func(token, method, path string, body any) *http.Response {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, rd)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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

	user := fmt.Sprintf("b33-me-%d", n)
	reg := do("", http.MethodPost, "/api/v1/auth/register",
		map[string]any{"username": user, "email": user + "@t.cn", "password": "S3cret-2026"})
	var meUser struct {
		ID int64 `json:"id"`
	}
	rawReg, _ := io.ReadAll(reg.Body)
	reg.Body.Close()
	if reg.StatusCode != http.StatusOK {
		t.Fatalf("register = %d %s", reg.StatusCode, rawReg)
	}
	_ = json.Unmarshal(rawReg, &meUser)
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM user_allowed_groups WHERE user_id = ?", meUser.ID).Error
		_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", meUser.ID).Error
		_ = db.Exec("DELETE FROM api_keys WHERE user_id = ?", meUser.ID).Error
		_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", meUser.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, meUser.ID).Error
	})
	lg := do("", http.MethodPost, "/api/v1/auth/login",
		map[string]any{"login": user, "password": "S3cret-2026"})
	var sess struct {
		Token string `json:"token"`
	}
	rawL, _ := io.ReadAll(lg.Body)
	lg.Body.Close()
	if lg.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", lg.StatusCode, rawL)
	}
	_ = json.Unmarshal(rawL, &sess)
	tok := sess.Token

	// 默认分组（注册自动进可见集合）
	var u model.User
	if err := db.First(&u, meUser.ID).Error; err != nil {
		t.Fatal(err)
	}
	// /me 资料
	mr := do(tok, http.MethodGet, "/api/v1/me", nil)
	mraw, _ := io.ReadAll(mr.Body)
	mr.Body.Close()
	if mr.StatusCode != http.StatusOK || !strings.Contains(string(mraw), user) {
		t.Fatalf("/me = %d %s", mr.StatusCode, mraw)
	}
	// /me/groups 应含默认组
	gr := do(tok, http.MethodGet, "/api/v1/me/groups", nil)
	gra, _ := io.ReadAll(gr.Body)
	gr.Body.Close()
	if gr.StatusCode != http.StatusOK || !strings.Contains(string(gra), `"id":`+fmt.Sprint(*u.DefaultGroupID)) {
		t.Fatalf("/me/groups 应含默认组 %d: %d %s", *u.DefaultGroupID, gr.StatusCode, gra)
	}
	// 充值后 /me/balance 反映余额（无行 → EnsureBalance 懒建 + 加 100）
	if err := repo.EnsureBalance(ctx, meUser.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := repo.ApplyBalanceDelta(ctx, meUser.ID, repository.BalanceDelta{Balance: decimalFrom("100")}); !ok {
		t.Fatal("充值失败")
	}
	br := do(tok, http.MethodGet, "/api/v1/me/balance", nil)
	bra, _ := io.ReadAll(br.Body)
	br.Body.Close()
	if br.StatusCode != http.StatusOK || !strings.Contains(string(bra), `"balance":"100"`) {
		t.Fatalf("/me/balance = %d %s", br.StatusCode, bra)
	}

	// ── API Key 自助 CRUD ──
	// 创建：唯一一次返回明文 sk-cf- 且前缀 10 位
	kc := do(tok, http.MethodPost, "/api/v1/me/keys", map[string]any{"name": "prod-key"})
	kcraw, _ := io.ReadAll(kc.Body)
	kc.Body.Close()
	if kc.StatusCode != http.StatusOK {
		t.Fatalf("create key = %d %s", kc.StatusCode, kcraw)
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(kcraw, &created); err != nil || created.ID == 0 ||
		!strings.HasPrefix(created.Token, "sk-cf-") || len(created.Token) != len("sk-cf-")+22 {
		t.Fatalf("创建应返回唯一明文: %s", kcraw)
	}
	// 库中只存哈希
	var krow model.APIKey
	if err := db.First(&krow, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if krow.KeyHash != auth.HashKey(salt, created.Token) || strings.Contains(krow.KeyHash, "sk-cf-") {
		t.Fatalf("库中应只存 SHA-256 哈希: %+v", krow)
	}
	// 自助闭环：该 Key 可经与 /v1 网关同源的 Authenticate 鉴权链命中（key+user+group 全 active）
	pr, err := auth.Authenticate(ctx, repo, salt, created.Token, "")
	if err != nil || pr == nil || pr.User.ID != meUser.ID {
		t.Fatalf("自助 Key 应通过 Authenticate 鉴权: pr=%+v err=%v", pr, err)
	}
	// 列表脱敏：含前缀与状态、不含明文/哈希字段
	kl := do(tok, http.MethodGet, "/api/v1/me/keys", nil)
	klraw, _ := io.ReadAll(kl.Body)
	kl.Body.Close()
	if kl.StatusCode != http.StatusOK || !strings.Contains(string(klraw), `"key_prefix":"`+created.Token[:10]) ||
		strings.Contains(string(klraw), `"key_hash"`) || strings.Contains(string(klraw), created.Token) {
		t.Fatalf("key 列表应脱敏含前缀且不含明文: %s", klraw)
	}
	// 无权分组 → 403
	forbiddenG := &model.Group{Name: fmt.Sprintf("b33-xg-%d", n), Status: "active"}
	if err := db.Create(forbiddenG).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Unscoped().Delete(&model.Group{}, forbiddenG.ID).Error })
	bad := do(tok, http.MethodPost, "/api/v1/me/keys", map[string]any{"name": "x", "group_id": forbiddenG.ID})
	bad.Body.Close()
	if bad.StatusCode != http.StatusForbidden {
		t.Fatalf("无权分组应 403, got %d", bad.StatusCode)
	}
	// 有效期超 2 年 → 400
	long := do(tok, http.MethodPost, "/api/v1/me/keys",
		map[string]any{"name": "x", "expires_at": time.Now().UTC().AddDate(3, 0, 0).Format(time.RFC3339)})
	long.Body.Close()
	if long.StatusCode != http.StatusBadRequest {
		t.Fatalf("超 2 年有效期应 400, got %d", long.StatusCode)
	}
	// PATCH 停用 → 200；鉴权即时失败（key disabled）；PATCH 他人/不存在 → 404
	must(tok, http.MethodPatch, fmt.Sprintf("/api/v1/me/keys/%d", created.ID), map[string]string{"status": "disabled"}, http.StatusOK)
	if _, err := auth.Authenticate(ctx, repo, salt, created.Token, ""); err == nil {
		t.Fatal("停用后 Authenticate 应失败（吊销即时生效）")
	}
	must(tok, http.MethodPatch, "/api/v1/me/keys/99999999", map[string]string{"status": "disabled"}, http.StatusNotFound)
	kl2 := do(tok, http.MethodGet, "/api/v1/me/keys", nil)
	kl2raw, _ := io.ReadAll(kl2.Body)
	kl2.Body.Close()
	if !strings.Contains(string(kl2raw), `"status":"disabled"`) {
		t.Fatalf("PATCH 后列表应反映 disabled: %s", kl2raw)
	}
	// DELETE 自身 → 204；鉴权即时失败；再删 → 404
	must(tok, http.MethodDelete, fmt.Sprintf("/api/v1/me/keys/%d", created.ID), nil, http.StatusNoContent)
	if _, err := auth.Authenticate(ctx, repo, salt, created.Token, ""); err == nil {
		t.Fatal("删除后 Authenticate 应失败（吊销即时生效）")
	}
	must(tok, http.MethodDelete, fmt.Sprintf("/api/v1/me/keys/%d", created.ID), nil, http.StatusNotFound)
	// ── 审查回归 ──
	// F1：大写 username/email 注册 → 统一小写存储 → 任意大小写均可登录（无变体歧义）
	upUser := fmt.Sprintf("B33Up-%d", n)
	upEmail := fmt.Sprintf("B33Up-%d@T.CN", n)
	ureg := do("", http.MethodPost, "/api/v1/auth/register",
		map[string]any{"username": upUser, "email": upEmail, "password": "S3cret-2026"})
	ureg.Body.Close()
	if ureg.StatusCode != http.StatusOK {
		t.Fatalf("大写注册应 200, got %d", ureg.StatusCode)
	}
	var upU model.User
	if err := db.Where("email = ?", strings.ToLower(upEmail)).First(&upU).Error; err != nil {
		t.Fatalf("email 应以小写存储: %v", err)
	}
	if upU.Username != strings.ToLower(upUser) {
		t.Fatalf("username 应以小写存储: %q", upU.Username)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM user_allowed_groups WHERE user_id = ?", upU.ID).Error
		_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", upU.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, upU.ID).Error
	})
	upLog := do("", http.MethodPost, "/api/v1/auth/login",
		map[string]any{"login": strings.ToLower(upEmail), "password": "S3cret-2026"})
	upLog.Body.Close()
	if upLog.StatusCode != http.StatusOK {
		t.Fatalf("小写 email 登录应 200, got %d", upLog.StatusCode)
	}
	upLog2 := do("", http.MethodPost, "/api/v1/auth/login",
		map[string]any{"login": upUser, "password": "S3cret-2026"}) // 原始大小写也应可登录（lower 匹配）
	upLog2.Body.Close()
	if upLog2.StatusCode != http.StatusOK {
		t.Fatalf("原始大小写 username 登录应 200, got %d", upLog2.StatusCode)
	}
	// F2/F3：管理风格建用户（无 allowed 行）——默认组 active 应见于 /me/groups；默认组 disabled 建 Key 应 400
	gActive := &model.Group{Name: fmt.Sprintf("b33-ga-%d", n), Status: "active"}
	gDisabled := &model.Group{Name: fmt.Sprintf("b33-gd-%d", n), Status: "disabled"}
	if err := db.Create(gActive).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(gDisabled).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Unscoped().Delete(&model.Group{}, gActive.ID).Error
		_ = db.Unscoped().Delete(&model.Group{}, gDisabled.ID).Error
	})
	adminStyleUser := func(suffix string, dg *model.Group) int64 {
		h, err := password.Hash("S3cret-2026")
		if err != nil {
			t.Fatal(err)
		}
		nm := fmt.Sprintf("b33as-%s-%d", suffix, n)
		uu := &model.User{Username: nm, Email: nm + "@t.cn", PasswordHash: &h,
			PasswordAlgo: "argon2id", Role: "user", Status: "active", DefaultGroupID: &dg.ID, Timezone: "UTC"}
		if err := db.Create(uu).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", uu.ID).Error
			_ = db.Exec("DELETE FROM api_keys WHERE user_id = ?", uu.ID).Error
			_ = db.Unscoped().Delete(&model.User{}, uu.ID).Error
		})
		return uu.ID
	}
	lgTok := func(uid int64) string {
		var one model.User
		if err := db.First(&one, uid).Error; err != nil {
			t.Fatal(err)
		}
		l := do("", http.MethodPost, "/api/v1/auth/login",
			map[string]any{"login": one.Username, "password": "S3cret-2026"})
		var st struct {
			Token string `json:"token"`
		}
		raw, _ := io.ReadAll(l.Body)
		l.Body.Close()
		if l.StatusCode != http.StatusOK {
			t.Fatalf("login admin-style user = %d %s", l.StatusCode, raw)
		}
		_ = json.Unmarshal(raw, &st)
		return st.Token
	}
	actID := adminStyleUser("a", gActive)
	tokAct := lgTok(actID)
	mg := do(tokAct, http.MethodGet, "/api/v1/me/groups", nil)
	mgraw, _ := io.ReadAll(mg.Body)
	mg.Body.Close()
	if mg.StatusCode != http.StatusOK || !strings.Contains(string(mgraw),
		fmt.Sprintf(`"id":%d`, gActive.ID)) || !strings.Contains(string(mgraw), `"is_default":true`) {
		t.Fatalf("默认组（未在 allowed）应见于 /me/groups: %d %s", mg.StatusCode, mgraw)
	}
	disID := adminStyleUser("d", gDisabled)
	tokDis := lgTok(disID)
	mgd := do(tokDis, http.MethodGet, "/api/v1/me/groups", nil)
	mgd.Body.Close()
	if mgd.StatusCode != http.StatusOK {
		t.Fatalf("/me/groups disabled default = %d", mgd.StatusCode)
	}
	kc2 := do(tokDis, http.MethodPost, "/api/v1/me/keys", map[string]any{"name": "x"})
	kc2.Body.Close()
	if kc2.StatusCode != http.StatusBadRequest {
		t.Fatalf("默认组停用时建 Key 应 400, got %d", kc2.StatusCode)
	}
}

func decimalFrom(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestPortalPaymentRecharge(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := repository.New(db)
	n := time.Now().UnixNano()
	salt := "portal-pay-salt"

	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	// payment:confirm 由进程内 memory enqueuer → 引擎 handler 模拟 worker 消费（b34-3 幂等语义）
	confirmEngine := &payment.ConfirmEngine{Repo: repo}
	enq := task.NewMemoryEnqueuer(map[task.TaskType]func(context.Context, task.Task) error{
		task.TaskPaymentConfirm: confirmEngine.HandleConfirm,
	})
	paySvc := payment.NewService(nil, nil)
	paySvc.RegisterProvider(&payment.MockProvider{}) // 测试显式注入（与生产安全约定一致）
	(&httpserver.Portal{Repo: repo, Salt: salt, RegistrationEnabled: true,
		Pay: paySvc, PayEnq: enq}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	do := func(token, method, path string, body any) *http.Response {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, rd)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	user := fmt.Sprintf("b34-u-%d", n)
	reg := do("", http.MethodPost, "/api/v1/auth/register",
		map[string]any{"username": user, "email": user + "@t.cn", "password": "S3cret-2026"})
	var ru struct {
		ID int64 `json:"id"`
	}
	rawR, _ := io.ReadAll(reg.Body)
	reg.Body.Close()
	if reg.StatusCode != http.StatusOK {
		t.Fatalf("register = %d %s", reg.StatusCode, rawR)
	}
	_ = json.Unmarshal(rawR, &ru)
	cleanupU := func() {
		_ = db.Exec("DELETE FROM user_allowed_groups WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM api_keys WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM payment_orders WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM billing_ledger WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", ru.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, ru.ID).Error
	}
	t.Cleanup(cleanupU)
	lg := do("", http.MethodPost, "/api/v1/auth/login",
		map[string]any{"login": user, "password": "S3cret-2026"})
	var st struct {
		Token string `json:"token"`
	}
	rawL, _ := io.ReadAll(lg.Body)
	lg.Body.Close()
	if lg.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", lg.StatusCode)
	}
	_ = json.Unmarshal(rawL, &st)
	tok := st.Token

	// 渠道列表含 mock
	pv := do(tok, http.MethodGet, "/api/v1/payment/providers", nil)
	pvraw, _ := io.ReadAll(pv.Body)
	pv.Body.Close()
	if pv.StatusCode != http.StatusOK || !strings.Contains(string(pvraw), `"code":"mock"`) {
		t.Fatalf("providers = %d %s", pv.StatusCode, pvraw)
	}
	// 非法参数：0 / 负 / 超上限 / 非 USD → 400
	for _, amt := range []string{"0", "-1", "5000.01"} {
		bad := do(tok, http.MethodPost, "/api/v1/payment/orders", map[string]any{"amount": amt})
		bad.Body.Close()
		if bad.StatusCode != http.StatusBadRequest {
			t.Fatalf("amount=%s 应 400, got %d", amt, bad.StatusCode)
		}
	}
	cny := do(tok, http.MethodPost, "/api/v1/payment/orders",
		map[string]any{"amount": "10", "currency": "CNY"})
	cny.Body.Close()
	if cny.StatusCode != http.StatusBadRequest {
		t.Fatalf("CNY 应 400（MVP 仅 USD）, got %d", cny.StatusCode)
	}
	// 下单 → pending + pay_url
	oc := do(tok, http.MethodPost, "/api/v1/payment/orders", map[string]any{"amount": "12.34"})
	ocraw, _ := io.ReadAll(oc.Body)
	oc.Body.Close()
	if oc.StatusCode != http.StatusOK {
		t.Fatalf("create order = %d %s", oc.StatusCode, ocraw)
	}
	var ores struct {
		OrderNo string `json:"order_no"`
		Status  string `json:"status"`
		PayURL  string `json:"pay_url"`
	}
	if err := json.Unmarshal(ocraw, &ores); err != nil || ores.OrderNo == "" ||
		ores.Status != "pending" || !strings.Contains(ores.PayURL, "mock://pay/") {
		t.Fatalf("下单响应异常: %s", ocraw)
	}
	var row model.PaymentOrder
	if err := db.Where("order_no = ?", ores.OrderNo).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending" || row.Amount.String() != "12.34" || row.UserID != ru.ID || row.Type != "recharge" {
		t.Fatalf("订单行异常: %+v", row)
	}
	// 查询本人订单
	gq := do(tok, http.MethodGet, "/api/v1/payment/orders/"+ores.OrderNo, nil)
	graw, _ := io.ReadAll(gq.Body)
	gq.Body.Close()
	if gq.StatusCode != http.StatusOK || !strings.Contains(string(graw), `"status":"pending"`) {
		t.Fatalf("query own order = %d %s", gq.StatusCode, graw)
	}
	// 非本人订单 → 404（他人账号查询）
	other := fmt.Sprintf("b34-o-%d", n)
	reg2 := do("", http.MethodPost, "/api/v1/auth/register",
		map[string]any{"username": other, "email": other + "@t.cn", "password": "S3cret-2026"})
	var r2 struct {
		ID int64 `json:"id"`
	}
	raw2, _ := io.ReadAll(reg2.Body)
	reg2.Body.Close()
	if reg2.StatusCode != http.StatusOK {
		t.Fatalf("register2 = %d", reg2.StatusCode)
	}
	_ = json.Unmarshal(raw2, &r2)
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM user_allowed_groups WHERE user_id = ?", r2.ID).Error
		_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", r2.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, r2.ID).Error
	})
	lg2 := do("", http.MethodPost, "/api/v1/auth/login",
		map[string]any{"login": other, "password": "S3cret-2026"})
	var st2 struct {
		Token string `json:"token"`
	}
	rawL2, _ := io.ReadAll(lg2.Body)
	lg2.Body.Close()
	_ = json.Unmarshal(rawL2, &st2)
	otherQ := do(st2.Token, http.MethodGet, "/api/v1/payment/orders/"+ores.OrderNo, nil)
	otherQ.Body.Close()
	if otherQ.StatusCode != http.StatusNotFound {
		t.Fatalf("他人查询应 404, got %d", otherQ.StatusCode)
	}

	// ── 全链路（b34-2/3/5）：回调验签→投递→worker 幂等入账──
	// 金额不符 → 400（防篡改 06 §7.3），订单保持 pending
	wrongAmt := do("", http.MethodPost, "/api/v1/payment/notify/mock", map[string]any{
		"order_no": ores.OrderNo, "provider_trade_no": "mock-" + ores.OrderNo, "amount": "12.33",
	})
	wrongAmt.Body.Close()
	if wrongAmt.StatusCode != http.StatusBadRequest {
		t.Fatalf("金额不符应 400, got %d", wrongAmt.StatusCode)
	}
	notifyBody := func() map[string]any {
		return map[string]any{"order_no": ores.OrderNo, "provider_trade_no": "mock-" + ores.OrderNo, "amount": "12.34"}
	}
	nt := do("", http.MethodPost, "/api/v1/payment/notify/mock", notifyBody())
	ntraw, _ := io.ReadAll(nt.Body)
	nt.Body.Close()
	if nt.StatusCode != http.StatusOK || !strings.Contains(string(ntraw), "success") {
		t.Fatalf("notify = %d %s", nt.StatusCode, ntraw)
	}
	// 入账：余额 + 12.34、ledger 一条 recharge、订单 paid
	balAfter := do(tok, http.MethodGet, "/api/v1/me/balance", nil)
	balraw, _ := io.ReadAll(balAfter.Body)
	balAfter.Body.Close()
	if !strings.Contains(string(balraw), `"balance":"12.34"`) {
		t.Fatalf("充值后余额应 12.34: %s", balraw)
	}
	var ledCnt int64
	_ = db.Model(&model.BillingLedger{}).Where("user_id = ? AND type = 'recharge'", ru.ID).Count(&ledCnt)
	if ledCnt != 1 {
		t.Fatalf("应 1 条 recharge ledger, got %d", ledCnt)
	}
	qPaid := do(tok, http.MethodGet, "/api/v1/payment/orders/"+ores.OrderNo, nil)
	qPaidraw, _ := io.ReadAll(qPaid.Body)
	qPaid.Body.Close()
	if !strings.Contains(string(qPaidraw), `"status":"paid"`) {
		t.Fatalf("订单应 paid: %s", qPaidraw)
	}
	// 重复回调（同流水号）→ 200 幂等，余额不叠加、ledger 仍 1 条
	nt2 := do("", http.MethodPost, "/api/v1/payment/notify/mock", notifyBody())
	nt2raw, _ := io.ReadAll(nt2.Body)
	nt2.Body.Close()
	if nt2.StatusCode != http.StatusOK || !strings.Contains(string(nt2raw), "duplicate") {
		t.Fatalf("重复回调应 success(duplicate): %d %s", nt2.StatusCode, nt2raw)
	}
	balAfter2 := do(tok, http.MethodGet, "/api/v1/me/balance", nil)
	balraw2, _ := io.ReadAll(balAfter2.Body)
	balAfter2.Body.Close()
	if !strings.Contains(string(balraw2), `"balance":"12.34"`) {
		t.Fatalf("重复回调不得叠加余额: %s", balraw2)
	}
	_ = db.Model(&model.BillingLedger{}).Where("user_id = ? AND type = 'recharge'", ru.ID).Count(&ledCnt)
	if ledCnt != 1 {
		t.Fatalf("重复回调后 ledger 应仍 1 条, got %d", ledCnt)
	}
}

func TestPortalUsageBilling(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := repository.New(db)
	n := time.Now().UnixNano()
	salt := "portal-usage-salt"

	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	(&httpserver.Portal{Repo: repo, Salt: salt, RegistrationEnabled: true}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	do := func(token, method, path string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	user := fmt.Sprintf("b35-u-%d", n)
	// POST 需 body，统一走 doReg
	doReg := func(method, path string, body any) *http.Response {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	r1 := doReg(http.MethodPost, "/api/v1/auth/register",
		map[string]any{"username": user, "email": user + "@t.cn", "password": "S3cret-2026"})
	var ru struct {
		ID int64 `json:"id"`
	}
	raw, _ := io.ReadAll(r1.Body)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("register = %d %s", r1.StatusCode, raw)
	}
	_ = json.Unmarshal(raw, &ru)
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM user_allowed_groups WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM billing_ledger WHERE user_id = ?", ru.ID).Error
		_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", ru.ID).Error
		_ = db.Unscoped().Delete(&model.User{}, ru.ID).Error
		// usage_logs 为 append-only（触发器禁止 DELETE）：request_id 唯一，逐运行自愈不清理
	})
	lg := doReg(http.MethodPost, "/api/v1/auth/login",
		map[string]any{"login": user, "password": "S3cret-2026"})
	var st struct {
		Token string `json:"token"`
	}
	rawL, _ := io.ReadAll(lg.Body)
	lg.Body.Close()
	if lg.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", lg.StatusCode)
	}
	_ = json.Unmarshal(rawL, &st)
	tok := st.Token

	// 预置两笔用量（模拟一次真实调用的 usage_logs 产物）
	now := time.Now().UTC()
	seedUsage := func(modelName string, in, out int) string {
		rid := fmt.Sprintf("req-b35-%s-%d", modelName, n)
		ul := &model.UsageLog{RequestID: rid, UserID: ru.ID, APIKeyID: 1, ChannelID: 1,
			Model: modelName, ProviderCode: "openai", InputTokens: in, OutputTokens: out,
			TotalCost:  model.Decimal{Decimal: decimal.RequireFromString("0.0110000000")},
			StatusCode: 200, CreatedAt: now}
		if err := db.Create(ul).Error; err != nil {
			t.Fatalf("seed usage: %v", err)
		}
		return rid
	}
	seedUsage("gpt-x", 100, 50)
	seedUsage("gpt-y", 40, 10)

	// 用量明细
	ul := do(tok, http.MethodGet, "/api/v1/me/usage")
	ulraw, _ := io.ReadAll(ul.Body)
	ul.Body.Close()
	if ul.StatusCode != http.StatusOK || !strings.Contains(string(ulraw), `"total":2`) ||
		!strings.Contains(string(ulraw), `"model":"gpt-x"`) {
		t.Fatalf("/me/usage = %d %s", ul.StatusCode, ulraw)
	}
	// 统计：requests=2、gpt-x tokens 正确
	us := do(tok, http.MethodGet, "/api/v1/me/usage/stats")
	usraw, _ := io.ReadAll(us.Body)
	us.Body.Close()
	if us.StatusCode != http.StatusOK || !strings.Contains(string(usraw), `"requests":2`) ||
		!strings.Contains(string(usraw), `"model":"gpt-x"`) ||
		!strings.Contains(string(usraw), `"total_cost":"0.022"`) {
		t.Fatalf("/me/usage/stats = %d %s", us.StatusCode, usraw)
	}
	// 调账产生 ledger 后账单流水可见
	if err := repo.EnsureBalance(ctx, ru.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ManualBalanceAdjust(ctx, ru.ID, decimal.NewFromInt(5), "e2e 账单对账", nil); err != nil {
		t.Fatal(err)
	}
	bl := do(tok, http.MethodGet, "/api/v1/me/billing?type=adjust")
	blraw, _ := io.ReadAll(bl.Body)
	bl.Body.Close()
	if bl.StatusCode != http.StatusOK || !strings.Contains(string(blraw), `"type":"adjust"`) ||
		!strings.Contains(string(blraw), `"amount":"5"`) {
		t.Fatalf("/me/billing = %d %s", bl.StatusCode, blraw)
	}
}
