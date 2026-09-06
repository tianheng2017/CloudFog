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
		// 分组绑定读回（修复：曾误用 group 侧查询（group_id=渠道 id）导致 group_ids 恒错）
		gr := call("sk-b3-adm", http.MethodGet, chPath, nil)
		gra, _ := io.ReadAll(gr.Body)
		gr.Body.Close()
		if gr.StatusCode != http.StatusOK || !strings.Contains(string(gra), fmt.Sprintf(`"group_ids":[%d]`, ug.ID)) {
			t.Fatalf("渠道分组读回应含 %d: %d %s", ug.ID, gr.StatusCode, gra)
		}
		// PUT 含不存在分组 → 404（FK 违例映射，曾 500）
		gb := call("sk-b3-adm", http.MethodPut, chPath+"/groups", map[string]any{"group_ids": []int64{ug.ID, 99999999}})
		gb.Body.Close()
		if gb.StatusCode != http.StatusNotFound {
			t.Fatalf("含不存在分组应 404, got %d", gb.StatusCode)
		}
		// 删除渠道 → 204；再查 404
		dl := call("sk-b3-adm", http.MethodDelete, chPath, nil)
		dl.Body.Close()
		if dl.StatusCode != http.StatusNoContent {
			t.Fatalf("delete channel = %d", dl.StatusCode)
		}
		g404 := call("sk-b3-adm", http.MethodGet, chPath, nil)
		g404.Body.Close()
		if g404.StatusCode != http.StatusNotFound {
			t.Fatalf("删除后 GET 应 404, got %d", g404.StatusCode)
		}
	})

	t.Run("供应商 upsert bill_on_failure 持久化", func(t *testing.T) {
		pcode := fmt.Sprintf("b3prov-%d", n)
		pu := call("sk-b3-adm", http.MethodPost, admPath("/providers"), map[string]any{
			"code": pcode, "name": "b3-prov", "protocol": "openai_compat", "base_url": "https://prov.example",
			"auth_type": "bearer", "status": "active", "capabilities": []string{"stream"}, "bill_on_failure": true,
		})
		pu.Body.Close()
		if pu.StatusCode != http.StatusOK {
			t.Fatalf("upsert provider = %d", pu.StatusCode)
		}
		var pv model.Provider
		if err := db.Where("code = ?", pcode).First(&pv).Error; err != nil || !pv.BillOnFailure {
			t.Fatalf("bill_on_failure 应持久化 true（修复前 DoUpdates 漏该列假成功）: err=%v bill=%v", err, pv.BillOnFailure)
		}
		t.Cleanup(func() { _ = db.Unscoped().Delete(&model.Provider{}, pv.ID).Error })
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
		// 价格删除 → 204；模型删除 → 204；列表不再含（此前清理未断言状态码）
		dp := call("sk-b3-adm", http.MethodDelete, admPath(fmt.Sprintf("/model-prices/%d", pcreated.ID)), nil)
		dp.Body.Close()
		if dp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete price = %d", dp.StatusCode)
		}
		dm := call("sk-b3-adm", http.MethodDelete, admPath(fmt.Sprintf("/models/%d", created.ID)), nil)
		dm.Body.Close()
		if dm.StatusCode != http.StatusNoContent {
			t.Fatalf("delete model = %d", dm.StatusCode)
		}
		ml := call("sk-b3-adm", http.MethodGet, admPath(fmt.Sprintf("/models?name=%s", mName)), nil)
		mlraw, _ := io.ReadAll(ml.Body)
		ml.Body.Close()
		if strings.Contains(string(mlraw), mName) {
			t.Fatalf("删除后模型不应在列表: %s", mlraw)
		}
	})

	t.Run("模型映射与删除闭环", func(t *testing.T) {
		alias := fmt.Sprintf("b3alias-%d", n)
		upstream := "gpt-4o"
		c1 := call("sk-b3-adm", http.MethodPost, admPath("/model-mappings"),
			map[string]any{"alias": alias, "upstream_model": upstream, "priority": 10})
		raw1, _ := io.ReadAll(c1.Body)
		c1.Body.Close()
		if c1.StatusCode != http.StatusOK {
			t.Fatalf("create mapping = %d %s", c1.StatusCode, raw1)
		}
		var mid struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw1, &mid); err != nil || mid.ID == 0 {
			t.Fatalf("parse mapping: %s", raw1)
		}
		// 精确重复 → 409（曾允许重复行致路由静默取首条）
		c2 := call("sk-b3-adm", http.MethodPost, admPath("/model-mappings"),
			map[string]any{"alias": alias, "upstream_model": upstream, "priority": 5})
		c2.Body.Close()
		if c2.StatusCode != http.StatusConflict {
			t.Fatalf("重复映射应 409, got %d", c2.StatusCode)
		}
		// 渠道级映射指向不存在渠道 → 404（FK 违例映射）
		ck := call("sk-b3-adm", http.MethodPost, admPath("/model-mappings"),
			map[string]any{"alias": alias + "-c", "upstream_model": upstream, "channel_id": 99999999})
		ck.Body.Close()
		if ck.StatusCode != http.StatusNotFound {
			t.Fatalf("不存在渠道应 404, got %d", ck.StatusCode)
		}
		// 列表按 alias 过滤
		lq := call("sk-b3-adm", http.MethodGet, admPath(fmt.Sprintf("/model-mappings?alias=%s", alias)), nil)
		lraw, _ := io.ReadAll(lq.Body)
		lq.Body.Close()
		if !strings.Contains(string(lraw), upstream) || strings.Contains(string(lraw), "-c") {
			t.Fatalf("列表应只含匹配 alias 的映射: %s", lraw)
		}
		// 删除 → 204 → 再删 404
		dd := call("sk-b3-adm", http.MethodDelete, admPath(fmt.Sprintf("/model-mappings/%d", mid.ID)), nil)
		dd.Body.Close()
		if dd.StatusCode != http.StatusNoContent {
			t.Fatalf("delete mapping = %d", dd.StatusCode)
		}
		d2 := call("sk-b3-adm", http.MethodDelete, admPath(fmt.Sprintf("/model-mappings/%d", mid.ID)), nil)
		d2.Body.Close()
		if d2.StatusCode != http.StatusNotFound {
			t.Fatalf("重复删除应 404, got %d", d2.StatusCode)
		}
	})
}
