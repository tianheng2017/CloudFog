package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"cloudfog/internal/billing"
	"cloudfog/internal/ir"
	"cloudfog/internal/model"
	_ "cloudfog/internal/pkg/adapter/openai" // 注册 openai 适配器
	"cloudfog/internal/repository"
	"cloudfog/internal/router"
	"cloudfog/internal/task"
)

type fakeCatalog struct {
	models     map[string]*model.Model
	prices     map[int64][]*model.ModelPrice
	mappings   map[string][]model.ModelMapping
	candidates []*repository.RouteCandidate
}

func (f *fakeCatalog) ModelByName(_ context.Context, name string) (*model.Model, error) {
	return f.models[name], nil
}
func (f *fakeCatalog) MappingRows(_ context.Context, alias string) ([]model.ModelMapping, error) {
	return f.mappings[alias], nil
}
func (f *fakeCatalog) CurrentModelPrice(_ context.Context, id int64, _ time.Time) (*model.ModelPrice, error) {
	ps := f.prices[id]
	if len(ps) == 0 {
		return nil, nil
	}
	return ps[len(ps)-1], nil
}
func (f *fakeCatalog) RouteCandidatesByGroup(context.Context, int64) ([]*repository.RouteCandidate, error) {
	return f.candidates, nil
}

type fakeBal struct{ bal *model.UserBalance }

func (f *fakeBal) BalanceByUserID(context.Context, int64) (*model.UserBalance, error) {
	if f.bal == nil {
		return nil, nil
	}
	return f.bal, nil
}

// openaiResp raw 上游 chat.completion。
func openaiCompletion(text, model string) string {
	return fmt.Sprintf(`{"id":"cmpl-1","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`, model, text)
}

func newChan(id int64, base string) *model.Channel {
	b := base
	return &model.Channel{ID: id, ProviderID: 1, ProviderCode: "openai", BaseURL: &b, Status: "active", Schedulable: true, CircuitState: "closed", Credentials: map[string]any{"api_key": "sk-up"}, Priority: int(id)}
}

func openAICandidate(id int64, base string) *repository.RouteCandidate {
	return &repository.RouteCandidate{
		Channel:  newChan(id, base),
		Provider: &model.Provider{ID: 1, Code: "openai", Protocol: "openai_compat", BaseURL: base, AuthType: "bearer", Status: "active"},
	}
}

func activeModel() *model.Model {
	return &model.Model{ID: 1, Name: "gpt-4o", Status: "active", BillingMode: "token"}
}

func callerOK() *Caller {
	return &Caller{UserID: 1, APIKeyID: 1, Group: &model.Group{ID: 10, Status: "active"}}
}

func mustGateway(cat router.Catalog, bal BalanceStore) *Gateway {
	return &Gateway{Cat: cat, Bal: bal, HTTP: &http.Client{Timeout: 5 * time.Second}, Now: func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) }}
}

func TestChatFailoverToSecondChannel(t *testing.T) {
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}))
	defer srvBad.Close()
	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-up" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, openaiCompletion("hi from ok", "gpt-4o"))
	}))
	defer srvOK.Close()

	gw := mustGateway(&fakeCatalog{
		models:     map[string]*model.Model{"gpt-4o": activeModel()},
		prices:     map[int64][]*model.ModelPrice{1: {{ModelID: 1, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}},
		candidates: []*repository.RouteCandidate{openAICandidate(1, srvBad.URL), openAICandidate(2, srvOK.URL)},
	}, &fakeBal{bal: &model.UserBalance{UserID: 1, Balance: model.Decimal{Decimal: newDecimal(100)}}})

	res, err := gw.Chat(context.Background(), callerOK(), &ir.CanonicalRequest{Model: "gpt-4o", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}}}}}, "")
	if err != nil {
		t.Fatalf("Chat 应经故障转移成功: %v", err)
	}
	if res.Resp == nil || len(res.Resp.Choices) == 0 || res.Resp.Choices[0].Message.Content[0].Text != "hi from ok" {
		t.Fatalf("未命中正常渠道响应: %+v", res.Resp)
	}
	if res.Channel.Candidate.Channel.ID != 2 {
		t.Fatalf("应切换到渠道 2, got %d", res.Channel.Candidate.Channel.ID)
	}
}

func TestChat402NoUpstream(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	gw := mustGateway(&fakeCatalog{
		models:     map[string]*model.Model{"gpt-4o": activeModel()},
		candidates: []*repository.RouteCandidate{openAICandidate(1, srv.URL)},
	}, &fakeBal{}) // 余额行不存在 → 402
	_, err := gw.Chat(context.Background(), callerOK(), &ir.CanonicalRequest{Model: "gpt-4o", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartText, Text: "x"}}}}}, "")
	var api *APIError
	if !errors.As(err, &api) || api.Status != 402 || api.Code != CodeInsufficientBalance {
		t.Fatalf("应为 402 insufficient_balance, got %v", err)
	}
	if hit {
		t.Fatal("余额不足不得发起上游请求（M5）")
	}
}

func TestChatWhitelistReject(t *testing.T) {
	gw := mustGateway(&fakeCatalog{models: map[string]*model.Model{"gpt-4o": activeModel()}}, &fakeBal{bal: &model.UserBalance{UserID: 1, Balance: model.Decimal{Decimal: newDecimal(1)}}})
	caller := callerOK()
	caller.Group.AllowedModels = []string{"gpt-4o-mini"}
	_, err := gw.Chat(context.Background(), caller, &ir.CanonicalRequest{Model: "gpt-4o", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartText, Text: "x"}}}}}, "")
	var api *APIError
	if !errors.As(err, &api) || api.Status != 403 || api.Code != CodeModelNotAllowed {
		t.Fatalf("白名单拒绝应为 403, got %v", err)
	}
}

func TestChatStreamSSE(t *testing.T) {
	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()
	gw := mustGateway(&fakeCatalog{
		models:     map[string]*model.Model{"gpt-4o": activeModel()},
		candidates: []*repository.RouteCandidate{openAICandidate(1, srv.URL)},
	}, &fakeBal{bal: &model.UserBalance{UserID: 1, Balance: model.Decimal{Decimal: newDecimal(1)}}})
	res, err := gw.Chat(context.Background(), callerOK(), &ir.CanonicalRequest{Model: "gpt-4o", Stream: true, Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartText, Text: "x"}}}}}, "")
	if err != nil {
		t.Fatalf("流式应成功: %v", err)
	}
	defer res.Close()
	var b strings.Builder
	for e := range res.Events {
		if e.Type == ir.EvDelta {
			b.WriteString(e.Delta)
		}
	}
	if b.String() != "Hello" {
		t.Fatalf("流式文本错误: %q", b.String())
	}
}

func newDecimal(v int64) (d decimal.Decimal) {
	return decimal.NewFromInt(v)
}

// ── 流式计量（2026-09-08：此前流式请求不计量不结算=白嫖）────────────────

type recEnqueuer struct{ tasks []task.Task }

func (r *recEnqueuer) Enqueue(_ context.Context, t task.Task) error {
	r.tasks = append(r.tasks, t)
	return nil
}
func (r *recEnqueuer) EnqueueIn(ctx context.Context, t task.Task, _ time.Duration) error {
	return r.Enqueue(ctx, t)
}

func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func streamGateway(cat router.Catalog, enq task.TaskEnqueuer) *Gateway {
	gw := mustGateway(cat, &fakeBal{bal: &model.UserBalance{UserID: 1, Balance: model.Decimal{Decimal: newDecimal(1)}}})
	gw.Prod = &billing.Producer{Enq: enq}
	return gw
}

func streamReq() *ir.CanonicalRequest {
	return &ir.CanonicalRequest{Model: "gpt-4o", Stream: true,
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartText, Text: "hello world"}}}}}
}

// TestChatStreamMetering 流式终态必须投递 usage:write + billing:settle（上游 usage 优先）。
func TestChatStreamMetering(t *testing.T) {
	srv := sseServer(t, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n"+
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n"+
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n"+
		"data: [DONE]\n\n")
	enq := &recEnqueuer{}
	gw := streamGateway(&fakeCatalog{
		models:     map[string]*model.Model{"gpt-4o": activeModel()},
		candidates: []*repository.RouteCandidate{openAICandidate(1, srv.URL)},
	}, enq)

	res, err := gw.Chat(context.Background(), callerOK(), streamReq(), "req-stream-1")
	if err != nil {
		t.Fatalf("流式应成功: %v", err)
	}
	defer res.Close()
	if res.Settle == nil {
		t.Fatal("流式结果必须携带计量钩子（Settle）")
	}
	var usage *ir.Usage
	chars := 0
	for e := range res.Events {
		if e.Type == ir.EvDelta {
			chars += len(e.Delta)
		}
		if e.Type == ir.EvUsage {
			usage = e.Usage
		}
	}
	res.Settle(StreamUsage{Usage: usage, DeltaChars: chars}, 200, "")

	if len(enq.tasks) != 2 {
		t.Fatalf("应投递 usage:write + billing:settle 两条任务, got %d", len(enq.tasks))
	}
	var u billing.UsageLogPayload
	if err := json.Unmarshal(enq.tasks[0].Payload, &u); err != nil {
		t.Fatal(err)
	}
	if enq.tasks[0].Type != task.TaskUsageWrite || enq.tasks[1].Type != task.TaskBillingSettle {
		t.Fatalf("任务类型/顺序错误: %s %s", enq.tasks[0].Type, enq.tasks[1].Type)
	}
	if !u.Stream || u.Tokens.Input != 4 || u.Tokens.Output != 2 || u.StatusCode != 200 {
		t.Fatalf("流式用量归集错误: %+v", u)
	}
	if u.RequestID != "req-stream-1" {
		t.Fatalf("request_id 未透传: %s", u.RequestID)
	}
}

// TestChatStreamMeteringEstimate 上游未回 usage 时按输出字符粗估，杜绝零用量白嫖。
func TestChatStreamMeteringEstimate(t *testing.T) {
	srv := sseServer(t, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"1234567890\"}}]}\n\n"+
		"data: [DONE]\n\n")
	enq := &recEnqueuer{}
	gw := streamGateway(&fakeCatalog{
		models:     map[string]*model.Model{"gpt-4o": activeModel()},
		candidates: []*repository.RouteCandidate{openAICandidate(1, srv.URL)},
	}, enq)

	res, err := gw.Chat(context.Background(), callerOK(), streamReq(), "req-stream-2")
	if err != nil {
		t.Fatalf("流式应成功: %v", err)
	}
	defer res.Close()
	chars := 0
	for e := range res.Events {
		if e.Type == ir.EvDelta {
			chars += len(e.Delta)
		}
	}
	res.Settle(StreamUsage{DeltaChars: chars}, 200, "")

	var u billing.UsageLogPayload
	if err := json.Unmarshal(enq.tasks[0].Payload, &u); err != nil {
		t.Fatal(err)
	}
	if u.Tokens.Input <= 0 {
		t.Fatalf("输入 token 应有粗估值: %+v", u)
	}
	if u.Tokens.Output != chars/4 {
		t.Fatalf("输出 token 应按字符粗估: got %d, chars %d", u.Tokens.Output, chars)
	}
}

// TestChatStreamBillingUnavailable 启用预扣但无投递器时流式也必须 fail-closed（防白嫖）。
func TestChatStreamBillingUnavailable(t *testing.T) {
	srv := sseServer(t, "data: [DONE]\n\n")
	gw := mustGateway(&fakeCatalog{
		models:     map[string]*model.Model{"gpt-4o": activeModel()},
		candidates: []*repository.RouteCandidate{openAICandidate(1, srv.URL)},
	}, &fakeBal{bal: &model.UserBalance{UserID: 1, Balance: model.Decimal{Decimal: newDecimal(1)}}})
	gw.Res = &fakeReserve{} // 启用预扣但 Prod 为空
	_, err := gw.Chat(context.Background(), callerOK(), streamReq(), "req-stream-3")
	var api *APIError
	if !errors.As(err, &api) || api.Status != 503 || api.Code != "billing_unavailable" {
		t.Fatalf("流式应 fail-closed 503 billing_unavailable, got %v", err)
	}
}

type fakeReserve struct{}

func (fakeReserve) Reserve(context.Context, int64, string, decimal.Decimal) error { return nil }
func (fakeReserve) Reset(context.Context, int64, string) error                    { return nil }
