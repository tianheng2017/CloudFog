package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudfog/internal/ir"
	"cloudfog/internal/pkg/adapter"
)

// HTTP 往返 + ctx 取消契约（04 §2 DecodeStream）：编排层取消时关闭 resp.Body 后，
// 事件流必须在限时内退出（无 goroutine 泄漏、无死锁）。
func TestOpenAIHTTPStreamCancelCloses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A\"}}]}\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done() // 保持连接打开，等待客户端取消
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewOpenAIProvider()
	in := mkOpenAIInput()
	in.Runtime.BaseURL = srv.URL
	req, err := p.EncodeRequest(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	evs, err := p.DecodeStream(ctx, resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan struct{})
	go func() {
		sawDelta := false
		for e := range evs {
			if e.Type == ir.EvDelta && e.Delta == "A" && !sawDelta {
				sawDelta = true
				cancel()
				_ = resp.Body.Close() // 编排层取消契约：关闭底层 reader 使解析 goroutine 退出
			}
		}
		close(seen)
	}()
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后事件流未在限时内退出（goroutine 泄漏/死锁）")
	}
}

func mkOpenAIInput() adapter.EncodeInput {
	temp := 0.7
	return adapter.EncodeInput{
		UpstreamModel: "gpt-4o",
		Runtime: adapter.Runtime{
			BaseURL: "https://api.openai.com/v1",
			Bearer:  "sk-up",
		},
		Request: &ir.CanonicalRequest{
			Model:  "gpt-4o",
			System: "你是一个助手",
			Messages: []ir.Message{
				{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartText, Text: "你好"}}},
			},
			Params: ir.GenerateParams{Temperature: &temp},
			Stream: true,
		},
	}
}

func TestOpenAIEncodeRequest(t *testing.T) {
	p := NewOpenAIProvider()
	req, err := p.EncodeRequest(context.Background(), mkOpenAIInput())
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer sk-up" {
		t.Fatalf("鉴权头错误: %s", req.Header.Get("Authorization"))
	}
	if req.URL.String() != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("URL 错误: %s", req.URL)
	}
	var body struct {
		Model         string `json:"model"`
		Stream        bool   `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "gpt-4o" || !body.Stream || !body.StreamOptions.IncludeUsage {
		t.Fatalf("stream_options.include_usage 必须携带（03 §3.6）: %+v", body)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
		t.Fatalf("system 应前置: %+v", body.Messages)
	}
}

func TestOpenAIDecodeResponse(t *testing.T) {
	p := NewOpenAIProvider()
	raw := `{"id":"cmpl-x","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"Hi"},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":3}}}`
	resp, err := p.DecodeResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "cmpl-x" || len(resp.Choices) != 1 || resp.Choices[0].Message.Content[0].Text != "Hi" {
		t.Fatalf("解码错误: %+v", resp)
	}
	if resp.FinishReason != ir.FinishLength || resp.Usage.InputTokens != 10 || resp.Usage.CacheReadTokens != 3 {
		t.Fatalf("usage/finish 映射错误: %+v", resp)
	}
}

func TestOpenAIDecodeStream(t *testing.T) {
	p := NewOpenAIProvider()
	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	evs, err := p.DecodeStream(context.Background(), strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	gotDone := false
	for e := range evs {
		switch e.Type {
		case ir.EvDelta:
			text.WriteString(e.Delta)
		case ir.EvDone:
			gotDone = true
			if e.FinishReason != ir.FinishStop {
				t.Fatalf("finish 应为 stop, got %s", e.FinishReason)
			}
		}
	}
	if text.String() != "Hello" || !gotDone {
		t.Fatalf("流事件错误: text=%q done=%v", text.String(), gotDone)
	}
}

func TestOpenAINormalizeError(t *testing.T) {
	p := NewOpenAIProvider()
	h := http.Header{}
	h.Set("Retry-After", "2")
	ue := p.NormalizeError(http.StatusTooManyRequests, h, []byte(`{"error":{"message":"slow down","type":"rate_limit_error"}}`))
	if ue == nil || !ue.Retryable || !ue.RetryableOnSame || ue.Code != "upstream_rate_limited" || ue.RetryAfter.Seconds() != 2 {
		t.Fatalf("429 归一错误: %+v", ue)
	}
	// 401 → 不可同渠道重试且触发熔断
	ue2 := p.NormalizeError(http.StatusUnauthorized, http.Header{}, []byte(`{}`))
	if ue2 == nil || ue2.RetryableOnSame || !ue2.TriggersCircuit || ue2.Code != "upstream_auth_failed" {
		t.Fatalf("401 归一错误: %+v", ue2)
	}
}

// 混沌：未收到 [DONE] 的 EOF（断流）必须报 EvError，不得当作成功完成。
func TestOpenAIDecodeStreamTruncated(t *testing.T) {
	p := NewOpenAIProvider()
	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n"
	evs, err := p.DecodeStream(context.Background(), strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	sawErr, sawDone := false, false
	for e := range evs {
		if e.Type == ir.EvError {
			sawErr = true
		}
		if e.Type == ir.EvDone {
			sawDone = true
		}
	}
	if !sawErr || sawDone {
		t.Fatalf("断流应报 EvError 且无 Done: err=%v done=%v", sawErr, sawDone)
	}
}

// 混沌：非法 JSON chunk 不得 panic，报 EvError。
func TestOpenAIDecodeStreamBadChunk(t *testing.T) {
	p := NewOpenAIProvider()
	sse := "data: {not-json\n\n"
	evs, err := p.DecodeStream(context.Background(), strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	sawErr := false
	for e := range evs {
		if e.Type == ir.EvError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("坏 chunk 应报 EvError")
	}
}

func TestDeepSeekIsOpenAICompat(t *testing.T) {
	if p := NewDeepSeekProvider(); p.Code() != "deepseek" || p.Protocol() != adapter.ProtocolOpenAICompat {
		t.Fatalf("deepseek 应为 openai_compat: %+v", p)
	}
}
