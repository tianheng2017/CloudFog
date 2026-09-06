// Package openai OpenAI 兼容上游 provider（04 §3/§4：OpenAI 基线、DeepSeek 薄封装复用）。
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"cloudfog/internal/ir"
	"cloudfog/internal/pkg/adapter"
)

func init() {
	adapter.Register(NewOpenAIProvider())
	adapter.Register(NewDeepSeekProvider())
}

// NewOpenAIProvider OpenAI 基线 provider。
func NewOpenAIProvider() adapter.Provider {
	return &openAICompat{code: "openai"}
}

// NewDeepSeekProvider DeepSeek 薄封装（OpenAI 兼容 codec 复用，04 §2.2）。
func NewDeepSeekProvider() adapter.Provider {
	return &openAICompat{code: "deepseek"}
}

type openAICompat struct{ code string }

func (p *openAICompat) Code() string               { return p.code }
func (p *openAICompat) Protocol() adapter.Protocol { return adapter.ProtocolOpenAICompat }

// ── 请求编码 ─────────────────────────────────────────────

type openAIMsg struct {
	Role       string `json:"role"`
	Content    any    `json:"content"` // string 或 []part
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  []any  `json:"tool_calls,omitempty"`
	Name       string `json:"name,omitempty"`
}

type openAIChatRequest struct {
	Model            string        `json:"model"`
	Messages         []openAIMsg   `json:"messages"`
	Stream           bool          `json:"stream,omitempty"`
	StreamOptions    *streamOption `json:"stream_options,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	Stop             []string      `json:"stop,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	Seed             *int64        `json:"seed,omitempty"`
	ReasoningEffort  string        `json:"reasoning_effort,omitempty"`
	ResponseFormat   any           `json:"response_format,omitempty"`
	User             string        `json:"user,omitempty"`
}

type streamOption struct {
	IncludeUsage bool `json:"include_usage"`
}

// contentPart 编码多模态片段为 openai part 对象。
func openAIContent(parts []ir.ContentPart) any {
	if len(parts) == 0 {
		return ""
	}
	single := true
	for _, c := range parts {
		if c.Type != ir.PartText {
			single = false
			break
		}
	}
	if single {
		b := strings.Builder{}
		for _, c := range parts {
			b.WriteString(c.Text)
		}
		return b.String()
	}
	out := make([]map[string]any, 0, len(parts))
	for _, c := range parts {
		switch c.Type {
		case ir.PartText:
			out = append(out, map[string]any{"type": "text", "text": c.Text})
		case ir.PartImageURL, ir.PartImageBase64:
			out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": c.ImageURL}})
		}
	}
	return out
}

func (p *openAICompat) EncodeRequest(ctx context.Context, in adapter.EncodeInput) (*http.Request, error) {
	req := in.Request
	if req == nil {
		return nil, fmt.Errorf("adapter: EncodeInput.Request 为空")
	}
	body := openAIChatRequest{Model: in.UpstreamModel, Stream: req.Stream}
	if req.Stream {
		body.StreamOptions = &streamOption{IncludeUsage: true} // 03 §3.6：请求即带 usage
	}
	if req.System != "" {
		body.Messages = append(body.Messages, openAIMsg{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		om := openAIMsg{Role: string(m.Role), Content: openAIContent(m.Content), ToolCallID: m.ToolCallID, Name: m.Name}
		body.Messages = append(body.Messages, om)
	}
	if v := req.Params.MaxTokens; v != nil {
		body.MaxTokens = v
	}
	if v := req.Params.Temperature; v != nil {
		body.Temperature = v
	}
	if v := req.Params.TopP; v != nil {
		body.TopP = v
	}
	if v := req.Params.PresencePenalty; v != nil {
		body.PresencePenalty = v
	}
	if v := req.Params.FrequencyPenalty; v != nil {
		body.FrequencyPenalty = v
	}
	if v := req.Params.Seed; v != nil {
		body.Seed = v
	}
	body.Stop = req.Params.Stop
	body.User = req.Params.User
	if req.Params.ReasoningEffort != "" {
		body.ReasoningEffort = req.Params.ReasoningEffort
	}
	if rf := req.Params.ResponseFormat; rf != nil {
		switch rf.Type {
		case "json_object":
			body.ResponseFormat = map[string]any{"type": "json_object"}
		case "json_schema":
			body.ResponseFormat = map[string]any{"type": "json_schema", "json_schema": rf.JSONSchema}
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("adapter: openai 编码失败: %w", err)
	}
	u := joinURL(in.Runtime.BaseURL, "/chat/completions")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if in.Runtime.Bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+in.Runtime.Bearer)
	}
	for k, v := range in.Runtime.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range in.Runtime.HeaderOverride {
		httpReq.Header.Set(k, v)
	}
	return httpReq, nil
}

// joinURL 把 baseURL 与固定路径拼接（base 可能带 /v1 或尾斜杠）。
func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

// ── 非流式解码 ───────────────────────────────────────────

type openAIResp struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   any    `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	PromptDetails    struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (p *openAICompat) DecodeResponse(body []byte) (*ir.CanonicalResponse, error) {
	var raw openAIResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("adapter: openai 响应解析失败: %w", err)
	}
	resp := &ir.CanonicalResponse{ID: raw.ID, Model: raw.Model}
	for _, c := range raw.Choices {
		msg := ir.Message{Role: ir.Role(c.Message.Role)}
		msg.Content = textToParts(c.Message.Content)
		resp.Choices = append(resp.Choices, ir.Choice{Index: c.Index, Message: msg, FinishReason: mapOpenAIStop(c.FinishReason)})
	}
	if raw.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:     raw.Usage.PromptTokens,
			OutputTokens:    raw.Usage.CompletionTokens,
			CacheReadTokens: raw.Usage.PromptDetails.CachedTokens,
			Source:          ir.UsageUpstream,
		}
	}
	if len(resp.Choices) > 0 {
		resp.FinishReason = resp.Choices[0].FinishReason
	}
	return resp, nil
}

// textToParts openai content（string 或 part 数组）归一为 IR 片段。
func textToParts(content any) []ir.ContentPart {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []ir.ContentPart{{Type: ir.PartText, Text: v}}
	case []any:
		var out []ir.ContentPart
		for _, item := range v {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			switch m["type"] {
			case "text":
				out = append(out, ir.ContentPart{Type: ir.PartText, Text: asStr(m["text"])})
			case "image_url":
				out = append(out, ir.ContentPart{Type: ir.PartImageURL, ImageURL: asStr(m["image_url"])})
			}
		}
		return out
	default:
		return nil
	}
}

func mapOpenAIStop(s string) ir.FinishReason {
	switch s {
	case "stop":
		return ir.FinishStop
	case "length":
		return ir.FinishLength
	case "tool_calls":
		return ir.FinishToolCalls
	case "content_filter":
		return ir.FinishContentFilter
	default:
		return ir.FinishStop
	}
}

// ── 流式解码（SSE）────────────────────────────────────────

type openAISSEChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *openAICompat) DecodeStream(ctx context.Context, r io.Reader) (<-chan ir.StreamEvent, error) {
	events, err := p.parseStream(ctx, r)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (p *openAICompat) parseStream(ctx context.Context, r io.Reader) (<-chan ir.StreamEvent, error) {
	rawEvents, err := adapter.ParseSSE(r)
	if err != nil {
		return nil, err
	}
	out := make(chan ir.StreamEvent, 8)
	go func() {
		defer close(out)
		var usage ir.Usage
		usage.Source = ir.UsageUpstream
		var finish ir.FinishReason
		for {
			select {
			case <-ctx.Done():
				out <- ir.StreamEvent{Type: ir.EvError, Err: ctx.Err()}
				return
			case ev, ok := <-rawEvents:
				if ev.Error != nil {
					out <- ir.StreamEvent{Type: ir.EvError, Err: fmt.Errorf("adapter: openai SSE 解析层错误: %w", ev.Error)}
					return
				}
				if !ok {
					// 未收到 [DONE] 的 EOF = 上游断流/截断：绝不能当作成功完成（否则截断响应被计为完整）
					out <- ir.StreamEvent{Type: ir.EvError, Err: errors.New("adapter: openai 流异常结束（未收到 [DONE]）")}
					return
				}
				data := strings.TrimSpace(ev.Data)
				if data == "" {
					continue
				}
				if data == "[DONE]" {
					if finish == "" {
						finish = ir.FinishStop
					}
					out <- ir.StreamEvent{Type: ir.EvUsage, Usage: &usage}
					out <- ir.StreamEvent{Type: ir.EvDone, FinishReason: finish}
					return
				}
				var chunk openAISSEChunk
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					out <- ir.StreamEvent{Type: ir.EvError, Err: fmt.Errorf("adapter: openai SSE chunk 解析失败: %w", err)}
					return
				}
				if chunk.Error != nil {
					out <- ir.StreamEvent{Type: ir.EvError, Err: &adapter.UpstreamError{Code: "upstream_error", HTTPStatus: 500, Message: chunk.Error.Message}}
					return
				}
				for _, c := range chunk.Choices {
					if c.Delta.Content != "" {
						out <- ir.StreamEvent{Type: ir.EvDelta, Delta: c.Delta.Content}
					}
					if c.Delta.ReasoningContent != "" {
						out <- ir.StreamEvent{Type: ir.EvReasoningDelta, Delta: c.Delta.ReasoningContent}
					}
					if c.FinishReason != "" {
						finish = mapOpenAIStop(c.FinishReason)
					}
				}
				if chunk.Usage != nil {
					usage.InputTokens = chunk.Usage.PromptTokens
					usage.OutputTokens = chunk.Usage.CompletionTokens
					usage.CacheReadTokens = chunk.Usage.PromptDetails.CachedTokens
				}
			}
		}
	}()
	return out, nil
}

// ── 错误归一 ─────────────────────────────────────────────

type openAIErr struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func (p *openAICompat) NormalizeError(statusCode int, respHeader http.Header, body []byte) *adapter.UpstreamError {
	ue := adapter.NormalizeHTTPStatus(statusCode, respHeader.Get("Retry-After"))
	if ue == nil {
		return nil
	}
	var e openAIErr
	if json.Unmarshal(body, &e) == nil && e.Error != nil && e.Error.Message != "" {
		ue.Message = sanitize(e.Error.Message)
	}
	return ue
}

// sanitize 截断并去除换行，防止上游正文原样进入日志/响应。
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return strings.ReplaceAll(s, "\n", " ")
}

func asStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
