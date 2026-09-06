package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"cloudfog/internal/ir"
)

// openaiChatReq OpenAI 兼容请求结构（07 §2.1 主入口子集：chat/completions）。
type openaiChatReq struct {
	Model            string            `json:"model"`
	Messages         []openaiChatMsg   `json:"messages"`
	Stream           bool              `json:"stream"`
	Tools            *json.RawMessage  `json:"tools"`       // 功能预留探测：当前不支持即显式拒绝（不静默丢包）
	ToolChoice       *json.RawMessage  `json:"tool_choice"` // 同上
	MaxTokens        *int              `json:"max_tokens"`
	Temperature      *float64          `json:"temperature"`
	TopP             *float64          `json:"top_p"`
	Stop             []string          `json:"stop"`
	PresencePenalty  *float64          `json:"presence_penalty"`
	FrequencyPenalty *float64          `json:"frequency_penalty"`
	Seed             *int64            `json:"seed"`
	ResponseFormat   *openaiRespFormat `json:"response_format"`
	User             string            `json:"user"`
}

type openaiChatMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string 或 [{type,text,image_url}]
}

type openaiRespFormat struct {
	Type string `json:"type"`
}

// DecodeChatOpenAI 把 OpenAI 兼容 chat/completions 请求体归一化为 IR（03 §4.1 入口解码）。
// 规则：system 消息上提为顶层 System（移除出 messages，适配 Anthropic 独立 system）；
// content 支持 string 或 part 数组（text/image_url）；未知厂商字段忽略（不报错，03 §3.3 逃生舱未提供时忽略）。
func DecodeChatOpenAI(r io.Reader) (*ir.CanonicalRequest, error) {
	var raw openaiChatReq
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("请求体不是合法 JSON: %w", err)
	}
	if strings.TrimSpace(raw.Model) == "" {
		return nil, fmt.Errorf("缺少 model")
	}
	// function calling（tools/tool_choice/tool 消息）为类型预留但入口未接线（IR 注释）。
	// 客户端带这些字段时**显式拒绝**而非静默丢弃——否则上游无 tools 会返回普通文本而照常计费，
	// 对期待工具调用的调用方是静默功能失灵。tool 消息同理（无 tool_call_id 支持语义会断裂）。
	if raw.Tools != nil || raw.ToolChoice != nil {
		return nil, fmt.Errorf("function calling（tools/tool_choice）尚不支持，请去除后重试")
	}
	req := &ir.CanonicalRequest{Model: raw.Model, Stream: raw.Stream}
	var sys []string
	for _, m := range raw.Messages {
		if m.Role == "tool" {
			return nil, fmt.Errorf("function calling（tool 消息）尚不支持，请去除后重试")
		}
		switch m.Role {
		case "system":
			parts, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("system 内容解析失败: %w", err)
			}
			for _, p := range parts {
				if p.Type == ir.PartText {
					sys = append(sys, p.Text)
				}
			}
		default:
			parts, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("%s 消息内容解析失败: %w", m.Role, err)
			}
			req.Messages = append(req.Messages, ir.Message{
				Role:    ir.Role(m.Role),
				Content: parts,
			})
		}
	}
	if len(sys) > 0 {
		req.System = strings.Join(sys, "\n")
	}
	if len(req.Messages) == 0 && req.System == "" {
		return nil, fmt.Errorf("请求不含任何消息")
	}

	req.Params = ir.GenerateParams{
		MaxTokens:        raw.MaxTokens,
		Temperature:      raw.Temperature,
		TopP:             raw.TopP,
		Stop:             raw.Stop,
		PresencePenalty:  raw.PresencePenalty,
		FrequencyPenalty: raw.FrequencyPenalty,
		Seed:             raw.Seed,
		User:             raw.User,
	}
	if raw.ResponseFormat != nil && raw.ResponseFormat.Type != "" {
		req.Params.ResponseFormat = &ir.ResponseFormat{Type: raw.ResponseFormat.Type}
	}
	return req, nil
}

func decodeOpenAIContent(raw json.RawMessage) ([]ir.ContentPart, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		if s == "" {
			return nil, nil
		}
		return []ir.ContentPart{{Type: ir.PartText, Text: s}}, nil
	}
	var arr []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("content 数组格式非法: %w", err)
	}
	out := make([]ir.ContentPart, 0, len(arr))
	for _, item := range arr {
		switch item.Type {
		case "text":
			out = append(out, ir.ContentPart{Type: ir.PartText, Text: item.Text})
		case "image_url":
			url := ""
			if item.ImageURL != nil {
				url = item.ImageURL.URL
			}
			out = append(out, ir.ContentPart{Type: ir.PartImageURL, ImageURL: url})
		}
	}
	return out, nil
}
