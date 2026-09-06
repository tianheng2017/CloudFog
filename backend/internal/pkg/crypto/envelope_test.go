package crypto

import (
	"encoding/json"
	"strings"
	"testing"
)

// 64 hex = 32B 主密钥（配置 security.master_key 同格式）
const (
	mkCur  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mkPrev = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func TestSealOpenRoundtrip(t *testing.T) {
	fields := map[string]any{"api_key": "sk-ant-secret", "extra": map[string]any{"region": "us"}}
	blob, err := Seal(fields, mkCur, "k_2026_08", "channel:42")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !IsSealed(blob) {
		t.Fatal("应标记为信封形态")
	}
	if blob["api_key"] != nil {
		t.Fatal("明文不得出现在信封外层")
	}
	// 正确的 AAD + 当前主密钥
	got, err := Open(blob, mkCur, mkPrev, "channel:42")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got["api_key"] != "sk-ant-secret" {
		t.Fatalf("解密字段不符: %+v", got)
	}
	// DB 泄露场景：无主密钥无法还原明文
	if !IsSealed(blob) || strings.Contains(mustJSON(blob), "sk-ant-secret") {
		t.Fatal("信封 JSON 不得含明文")
	}
}

func TestOpenAADMismatchFails(t *testing.T) {
	blob, err := Seal(map[string]any{"api_key": "x"}, mkCur, "k_2026_08", "channel:42")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(blob, mkCur, "", "channel:43"); err == nil {
		t.Fatal("AAD 不一致必须解密失败（防跨渠道移植密文）")
	}
}

func TestOpenPreviousKeyRotation(t *testing.T) {
	// 旧主密钥时代写入的记录
	blob, err := Seal(map[string]any{"api_key": "old-secret"}, mkPrev, "k_2026_07", "channel:7")
	if err != nil {
		t.Fatal(err)
	}
	// 轮换期：新主密钥为 current、旧为 previous → 仍可读
	got, err := Open(blob, mkCur, mkPrev, "channel:7")
	if err != nil {
		t.Fatalf("轮换期旧记录应可解密: %v", err)
	}
	if got["api_key"] != "old-secret" {
		t.Fatal("旧记录字段不符")
	}
	// 上一代密钥已移除后 → 明确失败
	if _, err := Open(blob, mkCur, "", "channel:7"); err == nil {
		t.Fatal("主密钥已轮换移除后旧记录应报错")
	}
}

func TestOpenLegacyPlaintextPassthrough(t *testing.T) {
	// B2 dev 明文形态（无信封结构）原样返回
	legacy := map[string]any{"api_key": "sk-mock"}
	got, err := Open(legacy, mkCur, "", "channel:1")
	if err != nil {
		t.Fatalf("legacy 明文应原样返回: %v", err)
	}
	if got["api_key"] != "sk-mock" {
		t.Fatal("legacy 字段不符")
	}
}

func TestSealBadMasterKey(t *testing.T) {
	if _, err := Seal(map[string]any{"a": "b"}, "short", "k", "c:1"); err == nil {
		t.Fatal("非法主密钥应报错")
	}
}

func mustJSON(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
