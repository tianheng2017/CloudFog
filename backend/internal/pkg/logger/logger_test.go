package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// newTestLogger 返回写内存的 logger，供断言输出内容。
func newTestLogger(t *testing.T, opts Options) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	level := parseLevel(opts.Level)
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: timeToUTC,
	})
	return slog.New(&redactHandler{inner: inner}), &buf
}

func Test_SensitiveKey_Redacted(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "info", Format: "json"})
	log.Info("login attempt", "api_key", "sk-cf-supersecret", "user_id", 42)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, buf.String())
	}
	if got := rec["api_key"]; got != "[REDACTED]" {
		t.Fatalf("api_key 应被脱敏，got %v", got)
	}
	if got := rec["user_id"]; got != float64(42) {
		t.Fatalf("普通字段不应被脱敏，got %v", got)
	}
	if strings.Contains(buf.String(), "supersecret") {
		t.Fatal("输出中不得出现明文凭证")
	}
}

// 回归测试：锁定 Handle 路径脱敏修复（Record 自身 Attrs 必须被处理）。
func Test_HandlePath_Attrs_Redacted(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "info", Format: "json"})
	log.Info("settle", slog.String("credentials", "plain-secret"), slog.Int("amount", 100))

	out := buf.String()
	if strings.Contains(out, "plain-secret") {
		t.Fatalf("Handle 路径的敏感属性未脱敏: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("应包含脱敏标记: %s", out)
	}
}

type fakeSecret struct{}

func (fakeSecret) Redact() string { return "sk-cf-****" }

func Test_Redactable_Interface(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "info", Format: "json"})
	log.Info("display", "secret", fakeSecret{})
	if !strings.Contains(buf.String(), "sk-cf-****") {
		t.Fatalf("Redactable 接口未生效: %s", buf.String())
	}
}

func Test_TimeUTC_RFC3339Nano(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "info", Format: "json"})
	log.Info("t")
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	ts, ok := rec["time"].(string)
	if !ok {
		t.Fatalf("time 字段缺失: %s", buf.String())
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("时间格式不是 RFC3339Nano: %v", err)
	}
	if parsed.Location() != time.UTC && time.Since(parsed) > time.Hour {
		t.Fatalf("时间应为 UTC 附近: %s", ts)
	}
}

func Test_LevelFilter(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "warn", Format: "json"})
	log.Info("hidden")
	log.Warn("shown")
	if strings.Contains(buf.String(), "hidden") {
		t.Fatal("info 级别应被过滤")
	}
	if !strings.Contains(buf.String(), "shown") {
		t.Fatal("warn 级别应输出")
	}
}

// 回归测试：slog.Group 嵌套子键必须被递归脱敏（否则组内敏感键绕过检测直接落盘）。
func Test_GroupChild_Redacted(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "info", Format: "json"})
	log.Info("billing-settle",
		slog.Group("billing",
			slog.String("order_id", "o-1"),
			slog.String("credentials", "group-secret"),
		),
	)
	out := buf.String()
	if strings.Contains(out, "group-secret") {
		t.Fatalf("slog.Group 子键明文泄露: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("组内敏感键应被脱敏: %s", out)
	}
	// 组结构应保留（order_id 不受影响），不能整组抹掉
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	g, ok := rec["billing"].(map[string]any)
	if !ok {
		t.Fatalf("组结构应保留: %s", out)
	}
	if g["order_id"] != "o-1" || g["credentials"] != "[REDACTED]" {
		t.Fatalf("组内字段处理异常: %v", g)
	}
}

// 回归测试：后缀匹配——组合键（smtp_password/stripe_secret_key 等）必须被覆盖。
func Test_SuffixMatch_Redacted(t *testing.T) {
	log, buf := newTestLogger(t, Options{Level: "info", Format: "json"})
	log.Info("notify",
		"smtp_password", "mail-pass",
		"stripe_secret_key", "sk_live_xxx",
		"refresh_token", "rt-xyz",
	)
	out := buf.String()
	for _, want := range []string{"mail-pass", "sk_live_xxx", "rt-xyz"} {
		if strings.Contains(out, want) {
			t.Fatalf("组合敏感键未脱敏，输出含明文 %s: %s", want, out)
		}
	}
}
