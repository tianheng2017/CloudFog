package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_Load_InvalidCronSpec_Fails(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	// robfig/cron 的 AddFunc 对非法表达式 panic，必须在 Load 阶段 fail-fast
	yaml := `
scheduler:
  channel_probe: "*/5 ** * * *"
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "scheduler.channel_probe") {
		t.Fatalf("非法 cron 应报错并定位到 scheduler.channel_probe，got: %v", err)
	}
}

func Test_Load_EmptyCronSpec_MeansDisabled(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	yaml := `
scheduler:
  channel_probe: ""
`
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("空 cron（不启用）不应报错: %v", err)
	}
}

func Test_Load_SameMasterKeyTwice_Fails(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CLOUDFOG_MASTER_KEY_OLD", strings.Repeat("a", 64)) // 与当前相同
	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), "previous_master_key") {
		t.Fatalf("新旧主密钥同值应报错，got: %v", err)
	}
}
