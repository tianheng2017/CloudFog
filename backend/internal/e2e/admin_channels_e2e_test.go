//go:build integration

// B3-2 验收：管理端经 API 建「信封密文渠道」→ 用户真实对话走该渠道（gateway 解密读侧）→
// 凭证永不明文出响应；enable/disable 影响调度；连通测试；模型/定价 CRUD。
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
	"cloudfog/internal/gateway"
	"cloudfog/internal/httpserver"
	"cloudfog/internal/model"
	"cloudfog/internal/repository"
)

const sealedKeyToken = "sk-b3-sealed-secret" // 渠道明文只应存在于请求体/内存，永不出现在响应

func TestB3ChannelsAdmin(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	repo := repository.New(db)
	n := time.Now().UnixNano()
	modelName := fmt.Sprintf("b3gpt-%d", n)

	// ── 种子：超管（走 admin API）+ 普通用户（走 chat）─────────
	seedB3User := func(tag, role, token string) (*model.User, *model.Group) {
		g := &model.Group{Name: fmt.Sprintf("b3g-%s-%d", tag, n), Status: "active"}
		if err := db.Create(g).Error; err != nil {
			t.Fatalf("seed group: %v", err)
		}
		u := &model.User{Email: fmt.Sprintf("%s-%d@cf.local", tag, n), Username: fmt.Sprintf("%s-%d", tag, n),
			Status: "active", Role: role, DefaultGroupID: &g.ID}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := repo.EnsureBalance(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		key := &model.APIKey{UserID: u.ID, GroupID: g.ID, Name: "adm", KeyPrefix: token,
			KeyHash: auth.HashKey(adminSalt, token), Status: "active"}
		if err := db.Create(key).Error; err != nil {
			t.Fatalf("seed key: %v", err)
		}
		return u, g
	}
	super, _ := seedB3User("sup", "super_admin", "sk-b3-adm")
	user, ug := seedB3User("usr", "user", "sk-b3-user")
	if ok, _ := repo.ApplyBalanceDelta(ctx, user.ID, repository.BalanceDelta{Balance: decimal.NewFromInt(100)}); !ok {
		t.Fatal("充值失败")
	}
	t.Cleanup(func() {
		for _, u := range []*model.User{super, user} {
			_ = db.Exec("DELETE FROM api_keys WHERE user_id = ?", u.ID).Error
			_ = db.Exec("DELETE FROM user_balances WHERE user_id = ?", u.ID).Error
			_ = db.Exec("DELETE FROM billing_ledger WHERE user_id = ?", u.ID).Error
			_ = db.Unscoped().Delete(&model.User{}, u.ID).Error
		}
		_ = db.Exec("DELETE FROM audit_logs WHERE actor_id IN (?,?) OR target_id IN (?,?)",
			fmt.Sprint(super.ID), fmt.Sprint(user.ID), fmt.Sprint(super.ID), fmt.Sprint(user.ID)).Error
	})

	// provider openai 就绪（B2/bootstrap 可能已建；缺失则补）
	var prov model.Provider
	if err := db.Where("code = 'openai'").First(&prov).Error; err != nil {
		prov = model.Provider{Code: "openai", Name: "openai", Protocol: "openai_compat",
			BaseURL: "https://api.openai.com", AuthType: "bearer", Status: "active", Capabilities: []string{"stream"}}
		if err := db.Create(&prov).Error; err != nil {
			t.Fatalf("seed provider: %v", err)
		}
	}
	// 模型 + 价格
	m := &model.Model{Name: modelName, ProviderCode: "openai", Status: "active", BillingMode: "token"}
	if err := db.Create(m).Error; err != nil {
		t.Fatal(err)
	}
	price := model.ModelPrice{ModelID: m.ID, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Currency: "USD", InputPricePer1K: model.Decimal{Decimal: decimal.NewFromFloat(0.01)},
		OutputPricePer1K: model.Decimal{Decimal: decimal.NewFromFloat(0.03)}}
	if err := db.Create(&price).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM model_prices WHERE model_id = ?", m.ID).Error
		_ = db.Unscoped().Delete(&model.Model{}, m.ID).Error
	})

	// ── mock 上游：校验 Bearer 确为密封渠道明文 → 证明 gateway 解密读侧生效 ──
	var authedReq bool
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+sealedKeyToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authedReq = true
		_, _ = io.WriteString(w, `{"id":"cmpl-b3","model":"`+modelName+`","choices":[{"index":0,"message":{"role":"assistant","content":"sealed ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer mock.Close()

	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	_ = eng.SetTrustedProxies(nil)
	gw := &gateway.Gateway{Cat: repo, Bal: repo, List: repo, CredMK: adminMasterMK}
	(&httpserver.API{Store: repo, Salt: adminSalt, Gw: gw}).Register(eng)
	(&httpserver.Admin{Repo: repo, Store: repo, Salt: adminSalt, MK: adminMasterMK, MKID: "k_e2e"}).Register(eng)
	srv := httptest.NewServer(eng)
	defer srv.Close()

	call := func(token, method, path string, body any) *http.Response {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	admPath := func(p string) string { return "/api/v1/admin" + p }

	t.Run("建密文渠道并脱敏", func(t *testing.T) {
		resp := call("sk-b3-adm", http.MethodPost, admPath("/channels"), map[string]any{
			"name": fmt.Sprintf("b3-sealed-%d", n), "provider_code": "openai", "base_url": mock.URL,
			"rate_multiplier": "1.2", "credentials": map[string]any{"api_key": sealedKeyToken},
		})
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create channel = %d body=%s", resp.StatusCode, raw)
		}
		if strings.Contains(string(raw), sealedKeyToken) {
			t.Fatal("创建响应不得含凭证明文")
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &created); err != nil || created.ID == 0 {
			t.Fatalf("create channel 解析失败: %s", raw)
		}
		chPath := admPath(fmt.Sprintf("/channels/%d", created.ID))
		// 绑定可见分组
		gresp := call("sk-b3-adm", http.MethodPut, chPath+"/groups", map[string]any{"group_ids": []int64{ug.ID}})
		gresp.Body.Close()
		if gresp.StatusCode != http.StatusOK {
			t.Fatalf("bind groups = %d", gresp.StatusCode)
		}
		// 列表不得含凭证（repo Omit 双保险）
		lresp := call("sk-b3-adm", http.MethodGet, admPath("/channels"), nil)
		rawList, _ := io.ReadAll(lresp.Body)
		lresp.Body.Close()
		if strings.Contains(string(rawList), sealedKeyToken) {
			t.Fatal("渠道列表不得含凭证密文/明文")
		}
		// 用户经密封渠道真实对话（gateway 解密）→ mock 收到正确 Bearer
		cresp := call("sk-b3-user", http.MethodPost, "/v1/chat/completions",
			map[string]any{"model": modelName, "messages": []map[string]string{{"role": "user", "content": "hi"}}})
		craw, _ := io.ReadAll(cresp.Body)
		cresp.Body.Close()
		if cresp.StatusCode != http.StatusOK {
			t.Fatalf("chat = %d body=%s", cresp.StatusCode, craw)
		}
		if !authedReq {
			t.Fatal("mock 应收到密封渠道解密后的 Bearer")
		}
		// 连通测试（GET /models）
		tr := call("sk-b3-adm", http.MethodPost, chPath+"/test", nil)
		traw, _ := io.ReadAll(tr.Body)
		tr.Body.Close()
		if !strings.Contains(string(traw), `"ok":true`) {
			t.Fatalf("连通测试应 ok: %s", traw)
		}
		// disable → 调度零命中；enable → 恢复
		call("sk-b3-adm", http.MethodPost, chPath+"/disable", nil).Body.Close()
		dresp := call("sk-b3-user", http.MethodPost, "/v1/chat/completions",
			map[string]any{"model": modelName, "messages": []map[string]string{{"role": "user", "content": "hi"}}})
		dresp.Body.Close()
		if dresp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("disable 后应 503 no_available_channel, got %d", dresp.StatusCode)
		}
		call("sk-b3-adm", http.MethodPost, chPath+"/enable", nil).Body.Close()
		eresp := call("sk-b3-user", http.MethodPost, "/v1/chat/completions",
			map[string]any{"model": modelName, "messages": []map[string]string{{"role": "user", "content": "hi"}}})
		eresp.Body.Close()
		if eresp.StatusCode != http.StatusOK {
			t.Fatalf("enable 后应恢复 200, got %d", eresp.StatusCode)
		}
		// 空 PATCH → 400（曾走 repo 错误变 500）；仅凭证轮换 → 200 且响应无明文
		ep := call("sk-b3-adm", http.MethodPatch, chPath, map[string]any{})
		ep.Body.Close()
		if ep.StatusCode != http.StatusBadRequest {
			t.Fatalf("空 PATCH 应 400, got %d", ep.StatusCode)
		}
		cr := call("sk-b3-adm", http.MethodPatch, chPath, map[string]any{"credentials": map[string]any{"api_key": "sk-rotated"}})
		craw2, _ := io.ReadAll(cr.Body)
		cr.Body.Close()
		if cr.StatusCode != http.StatusOK || strings.Contains(string(craw2), "sk-rotated") {
			t.Fatalf("凭证轮换应 200 且响应无明文: %d %s", cr.StatusCode, craw2)
		}
	})

	t.Run("模型与定价 CRUD", func(t *testing.T) {
		mName := fmt.Sprintf("b3crud-%d", n)
		resp := call("sk-b3-adm", http.MethodPost, admPath("/models"),
			map[string]any{"name": mName, "provider_code": "openai", "billing_mode": "token", "context_window": 8000})
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create model = %d %s", resp.StatusCode, raw)
		}
		var created struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(raw, &created)
		// 定价创建（先列表空 → create → dup 409）
		plist := call("sk-b3-adm", http.MethodGet, fmt.Sprintf("%s/model-prices?model_id=%d", admPath(""), created.ID), nil)
		plist.Body.Close()
		preq := map[string]any{"currency": "USD", "input_price_per_1k": "0.02", "output_price_per_1k": "0.05",
			"per_request_price": "0", "effective_from": "2026-06-01"}
		p1 := call("sk-b3-adm", http.MethodPost, fmt.Sprintf("%s/model-prices?model_id=%d", admPath(""), created.ID), preq)
		p1raw, _ := io.ReadAll(p1.Body)
		p1.Body.Close()
		if p1.StatusCode != http.StatusOK {
			t.Fatalf("create price = %d", p1.StatusCode)
		}
		var pcreated struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(p1raw, &pcreated)
		// PATCH 模型：仅改 display_name → name/context 保留（防清零）
		mp := call("sk-b3-adm", http.MethodPatch, admPath(fmt.Sprintf("/models/%d", created.ID)),
			map[string]any{"display_name": "renamed"})
		mp.Body.Close()
		if mp.StatusCode != http.StatusOK {
			t.Fatalf("patch model = %d", mp.StatusCode)
		}
		var gotM model.Model
		if err := db.First(&gotM, created.ID).Error; err != nil {
			t.Fatal(err)
		}
		if gotM.Name != mName || gotM.ContextWindow != 8000 || gotM.DisplayName != "renamed" {
			t.Fatalf("模型 PATCH 应保留未提供字段: name=%q ctx=%d disp=%q", gotM.Name, gotM.ContextWindow, gotM.DisplayName)
		}
		// PATCH 价格：仅改 effective_from → input 0.02 保留（曾因非指针结构把金额清 0）
		pp := call("sk-b3-adm", http.MethodPatch, admPath(fmt.Sprintf("/model-prices/%d", pcreated.ID)),
			map[string]string{"effective_from": "2026-07-01"})
		pp.Body.Close()
		if pp.StatusCode != http.StatusOK {
			t.Fatalf("patch price = %d", pp.StatusCode)
		}
		var gotP model.ModelPrice
		if err := db.First(&gotP, pcreated.ID).Error; err != nil {
			t.Fatal(err)
		}
		if gotP.InputPricePer1K.String() != "0.02" || gotP.OutputPricePer1K.String() != "0.05" {
			t.Fatalf("价格 PATCH 应保留金额现值: in=%s out=%s", gotP.InputPricePer1K, gotP.OutputPricePer1K)
		}
		// 与 patch 后同生效日（2026-07-01）重复创建 → 409
		dupReq := map[string]any{"currency": "USD", "input_price_per_1k": "0.02", "output_price_per_1k": "0.05",
			"per_request_price": "0", "effective_from": "2026-07-01"}
		p2 := call("sk-b3-adm", http.MethodPost, fmt.Sprintf("%s/model-prices?model_id=%d", admPath(""), created.ID), dupReq)
		p2.Body.Close()
		if p2.StatusCode != http.StatusConflict {
			t.Fatalf("重复生效日应 409, got %d", p2.StatusCode)
		}
		// 清理
		_ = db.Exec("DELETE FROM model_prices WHERE model_id = ?", created.ID).Error
		call("sk-b3-adm", http.MethodDelete, admPath(fmt.Sprintf("/models/%d", created.ID)), nil).Body.Close()
	})
}
