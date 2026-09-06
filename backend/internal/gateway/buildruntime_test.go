package gateway

import (
	"strings"
	"testing"

	"cloudfog/internal/model"
	"cloudfog/internal/pkg/crypto"
	"cloudfog/internal/repository"
	"cloudfog/internal/router"
)

const (
	brMK  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	brKey = "k_br"
)

func brAttempt(cred map[string]any) *router.Attempt {
	return &router.Attempt{
		Candidate: &repository.RouteCandidate{
			Channel: &model.Channel{ID: 7, Credentials: cred},
			Provider: &model.Provider{Code: "openai", BaseURL: "https://upstream.invalid",
				Protocol: "openai_compat", AuthType: "bearer"},
		},
		UpstreamModel: "gpt-4o",
	}
}

func TestGatewayBuildRuntimeDecryptsSealed(t *testing.T) {
	blob, err := crypto.Seal(map[string]any{"api_key": "sk-env-secret"}, brMK, brKey, "channel:7")
	if err != nil {
		t.Fatal(err)
	}
	g := &Gateway{CredMK: brMK}
	rt, err := g.buildRuntime(brAttempt(blob))
	if err != nil {
		t.Fatalf("解密装配失败: %v", err)
	}
	if rt.Bearer != "sk-env-secret" {
		t.Fatalf("信封解密后应取得明文: %q", rt.Bearer)
	}
	if rt.BaseURL != "https://upstream.invalid" {
		t.Fatalf("base url 装配异常: %q", rt.BaseURL)
	}
}

func TestGatewayBuildRuntimeSealedNoMK(t *testing.T) {
	blob, _ := crypto.Seal(map[string]any{"api_key": "sk-x"}, brMK, brKey, "channel:7")
	// 未配置主密钥：必须明确报错（不静默以空凭据发起上游）
	rt, err := (&Gateway{}).buildRuntime(brAttempt(blob))
	if err == nil || !strings.Contains(err.Error(), "主密钥") {
		t.Fatalf("应报主密钥缺失: err=%v rt=%+v", err, rt)
	}
}

func TestGatewayBuildRuntimeWrongAAD(t *testing.T) {
	// AAD 用错渠道（密文被移植场景）→ 解密失败明确报错
	blob, _ := crypto.Seal(map[string]any{"api_key": "sk-x"}, brMK, brKey, "channel:1")
	_, err := (&Gateway{CredMK: brMK}).buildRuntime(brAttempt(blob)) // attempt channel id=7
	if err == nil {
		t.Fatal("AAD 不符必须解密失败（防密文跨渠道移植）")
	}
}

func TestGatewayBuildRuntimeLegacyPlaintext(t *testing.T) {
	// B2 legacy 明文渠道：无主密钥也正常透传（零改造兼容）
	at := brAttempt(map[string]any{"api_key": "sk-legacy"})
	rt, err := (&Gateway{}).buildRuntime(at)
	if err != nil {
		t.Fatalf("legacy 透传失败: %v", err)
	}
	if rt.Bearer != "sk-legacy" {
		t.Fatalf("legacy bearer: %q", rt.Bearer)
	}
}
