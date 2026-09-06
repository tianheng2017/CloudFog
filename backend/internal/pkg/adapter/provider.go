// Package adapter 上游供应商适配层（04 §1/§2）。
// 职责：IR ↔ 上游协议的编解码 + 上游错误归一化；**无状态、并发安全**（多渠道/多 goroutine 共享单实例）。
// 渠道相关信息（凭证已解密、BaseURL、模型映射、Header 覆盖）经 EncodeInput 传入（04 §1：凭证获取解密归 channel）。
// 本包仅声明接口/注册器与共享工具；openai/、anthropic/ 子包各自实现并以 init 自注册（04 §2.1）。
package adapter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cloudfog/internal/ir"
)

// Protocol 上游协议类型（providers.protocol 枚举）。
type Protocol string

const (
	ProtocolOpenAICompat Protocol = "openai_compat"
	ProtocolAnthropic    Protocol = "anthropic"
	ProtocolGeminiNative Protocol = "gemini_native"
)

// Runtime 一次上游调用的渠道运行参数（凭证已解密，仅内存）。
type Runtime struct {
	ID             int64
	ProviderCode   string
	BaseURL        string            // 已解析的最终 BaseURL（渠道覆盖后）
	Bearer         string            // bearer 明文（仅内存；auth_type=bearer 用）
	Headers        map[string]string // 额外/自定义头（auth_type=header 的 x-api-key 等在此）
	ProxyURL       string
	HeaderOverride map[string]string // 渠道级 Header 覆盖（最后应用）
}

// EncodeInput 编码入参（04 §2 EncodeInput 的 MVP 子集）。
type EncodeInput struct {
	Request       *ir.CanonicalRequest
	UpstreamModel string
	Runtime       Runtime
	RequestID     string
}

// Provider 上游供应商统一抽象（MVP 方法集；Capabilities/健康检查/ExtractUsage 随探测层落地）。
type Provider interface {
	// Code 返回供应商唯一标识（providers.code）。
	Code() string
	// Protocol 返回上游协议。
	Protocol() Protocol
	// EncodeRequest 把 IR 编码为上游 HTTP 请求（含鉴权头、stream 标记）。
	EncodeRequest(ctx context.Context, in EncodeInput) (*http.Request, error)
	// DecodeResponse 解码非流式响应体为 IR。
	DecodeResponse(body []byte) (*ir.CanonicalResponse, error)
	// DecodeStream 解码上游 SSE 流为统一事件流。ctx 取消时关闭返回 channel。
	// 防 goroutine 泄漏契约：调用方（HTTP 编排层）在 ctx 取消时必须关闭底层 reader
	//（http resp.Body.Close），内部 scanner 随即 EOF 退出——否则解析 goroutine 会阻塞。
	DecodeStream(ctx context.Context, r io.Reader) (<-chan ir.StreamEvent, error)
	// NormalizeError 把上游 HTTP 错误归一化为平台错误（含重试语义，04 §2 UpstreamError）。
	NormalizeError(statusCode int, respHeader http.Header, body []byte) *UpstreamError
}

// UpstreamError 归一化上游错误（04 §2）。
type UpstreamError struct {
	Code            string // 平台错误码（03 §7.1 内部表 / 07 §5.2 对外表）
	HTTPStatus      int
	Message         string // 已脱敏的用户可见信息
	Retryable       bool   // 是否可切换其他渠道
	RetryableOnSame bool   // 是否可同渠道原地重试
	RetryAfter      time.Duration
	TriggersCircuit bool
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream[%d] %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// IsUpstreamError 判断 error 是否（含包装链）为 *UpstreamError。
func IsUpstreamError(err error) (*UpstreamError, bool) {
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return ue, true
	}
	return nil, false
}

// ── 注册器（04 §2.1：init 自注册，重复注册启动期 panic）──────────

var registry = map[string]Provider{}

// Register 注册适配器（重复 code panic，暴露启动期而非运行时）。
func Register(p Provider) {
	if p == nil || p.Code() == "" {
		panic("adapter: 非法注册（code 为空）")
	}
	if _, exists := registry[p.Code()]; exists {
		panic("adapter: 重复注册供应商 " + p.Code())
	}
	registry[p.Code()] = p
}

// Get 按 code 取适配器。
func Get(code string) (Provider, bool) {
	p, ok := registry[code]
	return p, ok
}

// All 返回全部已注册适配器（遍历顺序不稳定，仅用于枚举）。
func All() []Provider {
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}

// ── SSE 解析工具（03/04 §3.6：OpenAI 兼容与 Anthropic 共用）──────

// SSEEvent 一条解析后的 SSE 事件。Event 可为空（默认 message，OpenAI 兼容只发 data:）。
// Error 非 nil 表示流解析层错误（超长行/读错误），各 Provider 应转 EvError 而非静默丢弃。
type SSEEvent struct {
	Event string
	Data  string
	Error error
}

// ParseSSE 从流中解析 SSE 事件（供各协议 Provider 复用）。
// 处理：忽略空行与注释行；跨行 data（以 data: 续行）拼接为单事件（换行分隔）；EOF 关闭 channel。
func ParseSSE(r io.Reader) (<-chan SSEEvent, error) {
	ch := make(chan SSEEvent)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		var ev SSEEvent
		started := false
		flush := func() {
			if started {
				ch <- ev
			}
			ev, started = SSEEvent{}, false
		}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue // 注释
			}
			field, val, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			val = strings.TrimPrefix(val, " ") // "data: x" → "x"
			switch strings.TrimSpace(field) {
			case "event":
				ev.Event = val
				started = true
			case "data":
				if started && ev.Data != "" {
					ev.Data += "\n"
				}
				ev.Data += val
				started = true
			}
		}
		if err := sc.Err(); err != nil {
			// 超长行/读错误：上报错误事件，绝不能静默截断输出（04 §7 混沌要求）
			ch <- SSEEvent{Error: err}
			return
		}
		flush()
	}()
	return ch, nil
}
