// Package anthropic Anthropic Messages API provider（04 §3.1/§3.6）。
package anthropic

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
	adapter.Register(NewAnthropicProvider())
}

// NewAnthropicProvider 构造 Anthropic provider（code=anthropic，protocol=anthropic）。
func NewAnthropicProvider() adapter.Provider { return &anthropicProvider{} }

type anthropicProvider struct{}

func (p *anthropicProvider) Code() string               { return "anthropic" }
func (p *anthropicProvider) Protocol() adapter.Protocol { return adapter.ProtocolAnthropic }

// ── 请求编码（04 §3.1：system 独立、user/assistant 严格交替）────────

type anthContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthMsg struct {
	Role    string        `json:"role"`
	Content []anthContent `json:"content"`
}

type anthRequest struct {
	Model         string        `json:"model"`
	MaxTokens     int           `json:"max_tokens"` // Anthropic 必填
	System        []anthContent `json:"system,omitempty"`
	Messages      []anthMsg     `json:"messages"`
	Stream        bool          `json:"stream,omitempty"`
	Temperature   *float64      `json:"temperature,omitempty"`
	TopP          *float64      `json:"top_p,omitempty"`
	StopSequences []string      `json:"stop_sequences,omitempty"`
}

// defaultMaxTokens Anthropic 要求 max_tokens 必填；IR 未指定时编排层应给出，此处兜底保守值。
const defaultMaxTokens = 4096

func (p *anthropicProvider) EncodeRequest(ctx context.Context, in adapter.EncodeInput) (*http.Request, error) {
	req := in.Request
	if req == nil {
		return nil, fmt.Errorf("adapter: EncodeInput.Request 为空")
	}
	if req.System == "" && len(req.Messages) == 0 {
		return nil, fmt.Errorf("adapter: anthropic 请求无任何消息")
	}
	maxTok := defaultMaxTokens
	if v := req.Params.MaxTokens; v != nil && *v > 0 {
		maxTok = *v
	}

	body := anthRequest{
		Model:     in.UpstreamModel,
		MaxTokens: maxTok,
		Stream:    req.Stream,
	}
	if req.System != "" {
		body.System = []anthContent{{Type: "text", Text: req.System}}
	}
	if req.Params.Temperature != nil {
		body.Temperature = req.Params.Temperature
	}
	if req.Params.TopP != nil {
		body.TopP = req.Params.TopP
	}
	body.StopSequences = req.Params.Stop

	// 角色交替合并：连续同角色文本用 \n\n 连接；首条必须 user（04 §3.1 坑点）。
	msgs, err := mergeAlternating(req.Messages)
	if err != nil {
		return nil, err
	}
	body.Messages = msgs

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("adapter: anthropic 编码失败: %w", err)
	}
	u := strings.TrimRight(in.Runtime.BaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range in.Runtime.Headers {
		httpReq.Header.Set(k, v)
	}
	if httpReq.Header.Get("anthropic-version") == "" {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}
	if in.Runtime.Bearer != "" {
		httpReq.Header.Set("x-api-key", in.Runtime.Bearer)
	}
	for k, v := range in.Runtime.HeaderOverride {
		httpReq.Header.Set(k, v)
	}
	return httpReq, nil
}

// mergeAlternating 把 IR 消息转为 anthropic 消息，自动合并连续同角色（文本 \n\n 连接）。
// 首条非 user 时返回错误（平台归一化入口应保证，04 §3.1）。
func mergeAlternating(msgs []ir.Message) ([]anthMsg, error) {
	var out []anthMsg
	for _, m := range msgs {
		role := string(m.Role)
		if role != "user" && role != "assistant" {
			continue // system 已在顶层；tool 等 MVP 不映射
		}
		if len(out) == 0 && role != "user" {
			return nil, fmt.Errorf("adapter: anthropic 首条消息必须为 user，收到 %s", role)
		}
		text, err := anthTextFromParts(m.Content)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Content = append(out[len(out)-1].Content, anthContent{Type: "text", Text: text})
			continue
		}
		out = append(out, anthMsg{Role: role, Content: []anthContent{{Type: "text", Text: text}}})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("adapter: anthropic 无可发送消息")
	}
	return out, nil
}

// anthTextFromParts 提取纯文本内容；多模态 part 显式报错而非静默丢弃
// （否则用户以为图片已发送而请求照发，属静默失真——04 §3.2 演进前 fail-fast）。
func anthTextFromParts(parts []ir.ContentPart) (string, error) {
	var b strings.Builder
	for _, c := range parts {
		switch c.Type {
		case ir.PartText:
			b.WriteString(c.Text)
		default:
			return "", fmt.Errorf("adapter: anthropic 暂不支持 %s 类型内容（多模态演进中）", c.Type)
		}
	}
	return b.String(), nil
}

// ── 非流式解码 ───────────────────────────────────────────

type anthResp struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	Type       string `json:"type"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (p *anthropicProvider) DecodeResponse(body []byte) (*ir.CanonicalResponse, error) {
	var raw anthResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("adapter: anthropic 响应解析失败: %w", err)
	}
	var text string
	for _, c := range raw.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	// 上游响应体含 model 字段（如 claude-3-5-sonnet）：透传给 OpenAI 兼容响应的 model（r.Model），
	// 否则客户端会收到空 model（SDK 侧可能校验失败）。
	resp := &ir.CanonicalResponse{ID: raw.ID, Model: raw.Model}
	if text != "" {
		resp.Choices = []ir.Choice{{
			Message:      ir.Message{Role: ir.RoleAssistant, Content: []ir.ContentPart{{Type: ir.PartText, Text: text}}},
			FinishReason: mapAnthStop(raw.StopReason),
		}}
		resp.FinishReason = mapAnthStop(raw.StopReason)
	}
	resp.Usage = ir.Usage{
		InputTokens:      raw.Usage.InputTokens,
		OutputTokens:     raw.Usage.OutputTokens,
		CacheReadTokens:  raw.Usage.CacheReadInputTokens,
		CacheWriteTokens: raw.Usage.CacheCreationInputTokens,
		Source:           ir.UsageUpstream,
	}
	return resp, nil
}

func mapAnthStop(s string) ir.FinishReason {
	switch s {
	case "end_turn":
		return ir.FinishStop
	case "max_tokens":
		return ir.FinishLength
	case "tool_use":
		return ir.FinishToolCalls
	case "error":
		return ir.FinishError
	case "stop_sequence":
		return ir.FinishStop
	default:
		return ir.FinishStop
	}
}

// ── 流式解码（SSE：message_start / content_block_delta / message_delta / message_stop）──

// 流事件各自的结构（事件体根字段不同，分别解析避免 json tag 冲突）。
type anthMsgStart struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type anthContentDelta struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type anthMsgDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthSSEError struct {
	Type  string `json:"type"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *anthropicProvider) DecodeStream(ctx context.Context, r io.Reader) (<-chan ir.StreamEvent, error) {
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
					out <- ir.StreamEvent{Type: ir.EvError, Err: fmt.Errorf("adapter: anthropic SSE 解析层错误: %w", ev.Error)}
					return
				}
				if !ok {
					// 未收到 message_stop 的 EOF = 上游断流：显式报错而非静默无 Done（04 §7 混沌）
					out <- ir.StreamEvent{Type: ir.EvError, Err: errors.New("adapter: anthropic 流异常结束（未收到 message_stop）")}
					return
				}
				switch ev.Event {
				case "message_start":
					var e anthMsgStart
					if json.Unmarshal([]byte(ev.Data), &e) == nil {
						usage.InputTokens = e.Message.Usage.InputTokens
						usage.CacheReadTokens = e.Message.Usage.CacheReadInputTokens
						usage.CacheWriteTokens = e.Message.Usage.CacheCreationInputTokens
					}
				case "content_block_delta":
					var e anthContentDelta
					if json.Unmarshal([]byte(ev.Data), &e) == nil && e.Delta.Text != "" {
						// 内容通道分流：thinking_delta（思考链）≠ text_delta（可见回复）（03 §3.2/§3.5）
						typ := ir.EvDelta
						if e.Delta.Type == "thinking_delta" {
							typ = ir.EvReasoningDelta
						}
						out <- ir.StreamEvent{Type: typ, Delta: e.Delta.Text}
					}
				case "message_delta":
					var e anthMsgDelta
					if json.Unmarshal([]byte(ev.Data), &e) == nil {
						if e.Delta.StopReason != "" {
							finish = mapAnthStop(e.Delta.StopReason)
						}
						if e.Usage.OutputTokens > 0 {
							usage.OutputTokens = e.Usage.OutputTokens
						}
					}
				case "message_stop":
					if finish == "" {
						finish = ir.FinishStop
					}
					out <- ir.StreamEvent{Type: ir.EvUsage, Usage: &usage}
					out <- ir.StreamEvent{Type: ir.EvDone, FinishReason: finish}
					return
				case "error":
					var e anthSSEError
					if json.Unmarshal([]byte(ev.Data), &e) == nil {
						out <- ir.StreamEvent{Type: ir.EvError, Err: fmt.Errorf("anthropic: %s", e.Error.Message)}
					}
					return
				}
			}
		}
	}()
	return out, nil
}

// ── 错误归一 ─────────────────────────────────────────────

type anthErr struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *anthropicProvider) NormalizeError(statusCode int, respHeader http.Header, body []byte) *adapter.UpstreamError {
	ue := adapter.NormalizeHTTPStatus(statusCode, respHeader.Get("Retry-After"))
	if ue == nil {
		return nil
	}
	var e anthErr
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		ue.Message = sanitize(e.Error.Message)
	}
	return ue
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return strings.ReplaceAll(s, "\n", " ")
}
