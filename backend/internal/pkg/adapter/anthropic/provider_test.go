package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cloudfog/internal/ir"
	"cloudfog/internal/pkg/adapter"
)

func anthEncodeInput(system string, msgs []ir.Message, stream bool) adapter.EncodeInput {
	return adapter.EncodeInput{
		UpstreamModel: "claude-3-5-sonnet",
		Runtime: adapter.Runtime{
			BaseURL: "https://api.anthropic.com",
			Bearer:  "sk-ant-test",
			Headers: map[string]string{"x-api-key": "sk-ant-test", "anthropic-version": "2023-06-01"},
		},
		Request: &ir.CanonicalRequest{Model: "claude-3-5-sonnet", System: system, Messages: msgs, Stream: stream},
	}
}

func textMsg(role ir.Role, text string) ir.Message {
	return ir.Message{Role: role, Content: []ir.ContentPart{{Type: ir.PartText, Text: text}}}
}

func TestAnthropicEncodeMergesAlternating(t *testing.T) {
	p := NewAnthropicProvider()
	in := anthEncodeInput("你是助手", []ir.Message{
		textMsg(ir.RoleUser, "a"),
		textMsg(ir.RoleUser, "b"), // 连续 user 须合并为同一条
		textMsg(ir.RoleAssistant, "c"),
	}, false)
	req, err := p.EncodeRequest(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("URL: %s", req.URL)
	}
	if req.Header.Get("x-api-key") != "sk-ant-test" || req.Header.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("鉴权头缺失: %v", req.Header)
	}
	var body struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "user" || len(body.Messages[0].Content) != 2 || body.Messages[1].Role != "assistant" {
		t.Fatalf("连续同角色未合并/交替失败: %+v", body.Messages)
	}
	if body.MaxTokens <= 0 {
		t.Fatal("anthropic max_tokens 必填缺失")
	}
}

func TestAnthropicEncodeFirstMustUser(t *testing.T) {
	p := NewAnthropicProvider()
	if _, err := p.EncodeRequest(context.Background(), anthEncodeInput("", []ir.Message{textMsg(ir.RoleAssistant, "c")}, false)); err == nil {
		t.Fatal("首条 assistant 应报错（04 §3.1）")
	}
}

func TestAnthropicDecodeResponse(t *testing.T) {
	p := NewAnthropicProvider()
	raw := `{"id":"msg_x","model":"claude-3-5-sonnet","type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":8,"output_tokens":2,"cache_read_input_tokens":1,"cache_creation_input_tokens":0}}`
	resp, err := p.DecodeResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != ir.FinishStop || resp.Usage.InputTokens != 8 || resp.Usage.CacheReadTokens != 1 {
		t.Fatalf("解码错误: %+v", resp)
	}
	if resp.Model != "claude-3-5-sonnet" {
		t.Fatalf("上游 model 应透传（OpenAI 兼容响应 r.Model 直接展示，缺失会返回空 model）: %q", resp.Model)
	}
}

func TestAnthropicDecodeStream(t *testing.T) {
	p := NewAnthropicProvider()
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":6,\"cache_read_input_tokens\":0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"好\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	evs, err := p.DecodeStream(context.Background(), strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	gotUsage, gotDone := false, false
	for e := range evs {
		switch e.Type {
		case ir.EvDelta:
			text.WriteString(e.Delta)
		case ir.EvUsage:
			gotUsage = true
			if e.Usage.InputTokens != 6 || e.Usage.OutputTokens != 3 {
				t.Fatalf("usage 映射错误: %+v", e.Usage)
			}
		case ir.EvDone:
			gotDone = true
			if e.FinishReason != ir.FinishStop {
				t.Fatalf("finish 应 stop: %s", e.FinishReason)
			}
		}
	}
	if text.String() != "你好" || !gotUsage || !gotDone {
		t.Fatalf("流错误: text=%q usage=%v done=%v", text.String(), gotUsage, gotDone)
	}
}

func TestAnthropicStreamReasoningChannel(t *testing.T) {
	p := NewAnthropicProvider()
	sse := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"text\":\"思考中\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"回答\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	evs, err := p.DecodeStream(context.Background(), strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	var reasoning, answer string
	for e := range evs {
		if e.Type == ir.EvReasoningDelta {
			reasoning += e.Delta
		}
		if e.Type == ir.EvDelta {
			answer += e.Delta
		}
	}
	if reasoning != "思考中" || answer != "回答" {
		t.Fatalf("thinking/text 通道分流错误: reasoning=%q answer=%q", reasoning, answer)
	}
}

// 混沌：未收到 message_stop 的 EOF（断流）必须报 EvError。
func TestAnthropicDecodeStreamTruncated(t *testing.T) {
	p := NewAnthropicProvider()
	sse := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n"
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

func TestAnthropicImageFailsFast(t *testing.T) {
	p := NewAnthropicProvider()
	img := ir.Message{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.PartImageURL, ImageURL: "data:..."}}}
	if _, err := p.EncodeRequest(context.Background(), anthEncodeInput("", []ir.Message{img}, false)); err == nil {
		t.Fatal("多模态 part 应显式报错而非静默丢弃（04 §3.2 演进前 fail-fast）")
	}
}

func TestAnthropicNormalizeError(t *testing.T) {
	p := NewAnthropicProvider()
	ue := p.NormalizeError(http.StatusUnauthorized, http.Header{}, []byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`))
	if ue == nil || ue.Code != "upstream_auth_failed" || ue.HTTPStatus != 502 || !ue.TriggersCircuit {
		t.Fatalf("401 归一错误: %+v", ue)
	}
}
