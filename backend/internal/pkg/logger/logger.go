// Package logger 提供结构化 slog 日志（01 §6 / 10 §3.5）：
// JSON 输出、UTC RFC3339 时间、敏感字段强制脱敏（08 §4.3 日志内容红线）。
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Redactable 值类型实现该接口后，日志输出自动替换为脱敏形式
// （如 API Key 只保留前缀，见 08 §3.2 key_prefix 固定 10 位）。
type Redactable interface {
	Redact() string
}

// sensitiveKeys 精确命中的敏感键（不区分大小写）。
// 业务代码不得把明文凭证放进这些键以外的键——评审红线。
var sensitiveKeys = map[string]bool{
	"api_key": true, "api_key_hash": true, "authorization": true,
	"credentials": true, "credential": true, "password": true,
	"secret": true, "secret_key": true, "master_key": true,
	"token": true, "access_token": true, "refresh_token": true,
	"proxy_password": true, "cookie": true, "session_id": true,
}

// isSensitive 判定：精确命中，或后缀命中（smtp_password / stripe_secret_key /
// refresh_token / platform_api_key 等组合键全部覆盖，防精确匹配漏网）。
func isSensitive(key string) bool {
	k := strings.ToLower(key)
	if sensitiveKeys[k] {
		return true
	}
	for _, suffix := range []string{"password", "secret", "secret_key", "token", "api_key"} {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// Options 日志选项（来自 config.ObservabilityConfig）。
type Options struct {
	Level  string // debug | info | warn | error
	Format string // json（生产唯一格式）；text 仅供本地调试
}

// New 构造生产 slog.Logger。时间一律 UTC RFC3339（02 §13.1：应用内部一律 UTC）。
// 职责切分：redactHandler 负责全部脱敏；inner 的 ReplaceAttr 只做时间格式转换，
// 避免双重脱敏（Redactable 产出会被 inner 的键名规则再次掩码）。
func New(opts Options) *slog.Logger {
	level := parseLevel(opts.Level)
	var inner slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: timeToUTC,
	})
	if strings.EqualFold(opts.Format, "text") {
		inner = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:       level,
			ReplaceAttr: timeToUTC,
		})
	}
	return slog.New(&redactHandler{inner: inner})
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func timeToUTC(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		if t, ok := a.Value.Any().(time.Time); ok {
			return slog.String(slog.TimeKey, t.UTC().Format(time.RFC3339Nano))
		}
	}
	return a
}

func redactAttr(a slog.Attr) slog.Attr {
	// 优先级 1：值类型自带脱敏形态（如 API Key 保留 10 位前缀比全掩码更有用）
	if r, ok := a.Value.Any().(Redactable); ok {
		return slog.String(a.Key, r.Redact())
	}
	// 优先级 1.5：递归处理 slog.Group 子键——否则组内敏感键
	//（slog.Group("x", slog.String("token", "明文"))）绕过键名检测直接落盘（08 §4.3）。
	if a.Value.Kind() == slog.KindGroup {
		children := a.Value.Group()
		redacted := make([]slog.Attr, 0, len(children))
		for _, c := range children {
			redacted = append(redacted, redactAttr(c))
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	}
	// 优先级 2：键名命中敏感集 → 整体掩码
	if isSensitive(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

// redactHandler 递归处理 WithAttrs，保证通过 logger.With 添加的嵌套字段也被脱敏。
type redactHandler struct {
	inner slog.Handler
}

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	// 关键：Record 自身的 Attrs（logger.Info("msg", "api_key", k) 这类逐调用属性）
	// 不经过 WithAttrs，必须在此处重建记录并逐项脱敏，否则明文凭证直接落盘（08 §4.3）。
	redacted := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		redacted = append(redacted, redactAttr(a))
		return true
	})
	r2 := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r2.AddAttrs(redacted...)
	return h.inner.Handle(ctx, r2)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		redacted = append(redacted, redactAttr(a))
	}
	return &redactHandler{inner: h.inner.WithAttrs(redacted)}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name)}
}
