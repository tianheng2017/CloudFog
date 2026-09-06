package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/auth"
	"cloudfog/internal/gateway"
	"cloudfog/internal/ir"
)

// API v1 业务端点（07 §2.1 OpenAI 兼容主入口）。
// 计量/结算投递（usage:write / billing:settle）为 b2-6 钩子位，本版端点完成转发闭环。
type API struct {
	Store auth.Lookup
	Salt  string
	Gw    *gateway.Gateway
	Log   *slog.Logger
}

func (a *API) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

// Register 挂载 v1 组（请求链：RequestID → APIKeyAuth → handler）。
func (a *API) Register(eng *gin.Engine) {
	v1 := eng.Group("/v1")
	v1.Use(RequestIDMiddleware(), APIKeyAuth(a.Store, a.Salt))
	v1.GET("/models", a.handleModels)
	v1.POST("/chat/completions", a.handleChat)
}

// ── error 响应（03 §7.2 OpenAI 风格 + request_id 头）────────────────

func writeAPIError(c *gin.Context, status int, code string, msg string) {
	id := requestID(c) // 与中间件生成/透传值一致（header 与 body 同源）
	c.Header("X-Request-ID", id)
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message":    msg,
			"type":       "cloudfog_error",
			"code":       code,
			"param":      nil,
			"request_id": id,
		},
	})
}

// ── GET /v1/models ─────────────────────────────────────────

func (a *API) handleModels(c *gin.Context) {
	if _, ok := PrincipalOf(c); !ok {
		writeAPIError(c, http.StatusUnauthorized, "invalid_api_key", "缺少有效密钥")
		return
	}
	list, err := a.Models(c.Request.Context())
	if err != nil {
		writeAPIError(c, http.StatusInternalServerError, "server_error", "模型列表加载失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": list})
}

// Models 供 /v1/models（model.go 层的对象字段映射）。
func (a *API) Models(ctx context.Context) ([]map[string]any, error) {
	models, err := a.Gw.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		out = append(out, map[string]any{
			"id":       m.Name,
			"object":   "model",
			"created":  m.CreatedAt.Unix(),
			"owned_by": m.ProviderCode,
		})
	}
	return out, nil
}

// ── POST /v1/chat/completions ──────────────────────────────

func (a *API) handleChat(c *gin.Context) {
	p, ok := PrincipalOf(c)
	if !ok {
		writeAPIError(c, http.StatusUnauthorized, "invalid_api_key", "缺少有效密钥")
		return
	}
	req, err := gateway.DecodeChatOpenAI(c.Request.Body)
	if err != nil {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	caller := &gateway.Caller{
		UserID:         p.User.ID,
		APIKeyID:       p.Key.ID,
		Group:          p.Group,
		ModelWhitelist: p.Key.ModelWhitelist,
	}
	res, err := a.Gw.Chat(c.Request.Context(), caller, req, requestID(c))
	if err != nil {
		var api *gateway.APIError
		if errors.As(err, &api) {
			writeAPIError(c, api.Status, string(api.Code), api.Msg)
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			writeAPIError(c, http.StatusGatewayTimeout, "request_timeout", "请求超时")
			return
		}
		writeAPIError(c, http.StatusBadGateway, "upstream_error", "上游转发失败")
		return
	}
	if res.Stream {
		a.writeChatStream(c, res)
		return
	}
	a.writeChatJSON(c, res)
}

func (a *API) writeChatJSON(c *gin.Context, res *gateway.Result) {
	c.Header("X-CloudFog-Model", res.Channel.UpstreamModel)
	c.Header("X-CloudFog-Provider", res.Channel.Candidate.Provider.Code)
	c.JSON(http.StatusOK, openAICompletionFromIR(res.Resp))
}

func (a *API) writeChatStream(c *gin.Context, res *gateway.Result) {
	defer func() {
		if res.Close != nil {
			res.Close()
		}
	}()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-CloudFog-Model", res.Channel.UpstreamModel)
	c.Header("X-CloudFog-Provider", res.Channel.Candidate.Provider.Code)
	fl, ok := c.Writer.(http.Flusher)
	if !ok {
		writeAPIError(c, http.StatusInternalServerError, "server_error", "响应不支持流式")
		return
	}
	chunkID := "chatcmpl-" + requestID(c)
	// 标准 OpenAI 兼容 SSE：每个事件须为 "data: <json>\n\n"（07 §2.1）。
	// 此前直接用 JSON Encoder 输出裸 JSON（无 data: 前缀、无空行分隔），
	// 官方 SDK 的 SSE 解析器无法识别——B2-7 官方 SDK 验收发现并修复。
	writeSSE := func(v gin.H) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = c.Writer.Write(append(append([]byte("data: "), b...), '\n', '\n'))
		return err
	}
	for ev := range res.Events {
		// 写入失败（客户端断连/写超时）即终止：defer 关闭上游 body，解析 goroutine 随之退出，
		// 避免断线后仍持续拉取并消费上游 SSE。
		switch ev.Type {
		case ir.EvDelta:
			if err := writeSSE(streamChunk(chunkID, res.Channel.UpstreamModel, gin.H{"content": ev.Delta}, nil)); err != nil {
				return
			}
		case ir.EvReasoningDelta:
			if err := writeSSE(streamChunk(chunkID, res.Channel.UpstreamModel, gin.H{"reasoning_content": ev.Delta}, nil)); err != nil {
				return
			}
		case ir.EvError:
			a.log().Error("chat 流错误", "err", ev.Err)
			return
		case ir.EvDone:
			if err := writeSSE(streamChunk(chunkID, res.Channel.UpstreamModel, nil, &ev.FinishReason)); err != nil {
				return
			}
			_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
			fl.Flush()
			return
		}
		fl.Flush()
	}
	// 上游异常结束（无 Done）：补一个空完成避免客户端永久等待
	_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	fl.Flush()
}

func streamChunk(id, model string, delta gin.H, finish *ir.FinishReason) gin.H {
	ch := gin.H{"index": 0}
	if delta != nil {
		ch["delta"] = delta
	}
	if finish != nil {
		ch["finish_reason"] = string(*finish)
	}
	return gin.H{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []gin.H{ch}}
}

func openAICompletionFromIR(r *ir.CanonicalResponse) gin.H {
	choices := make([]gin.H, 0, len(r.Choices))
	for _, ch := range r.Choices {
		text := ""
		for _, part := range ch.Message.Content {
			if part.Type == ir.PartText {
				text += part.Text
			}
		}
		choices = append(choices, gin.H{
			"index":         ch.Index,
			"message":       gin.H{"role": "assistant", "content": text},
			"finish_reason": string(ch.FinishReason),
		})
	}
	usage := gin.H{}
	if r.Usage.InputTokens != 0 || r.Usage.OutputTokens != 0 {
		usage = gin.H{
			"prompt_tokens":     r.Usage.InputTokens,
			"completion_tokens": r.Usage.OutputTokens,
			"total_tokens":      r.Usage.InputTokens + r.Usage.OutputTokens,
		}
	}
	return gin.H{
		"id":      r.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   r.Model,
		"choices": choices,
		"usage":   usage,
	}
}
