package adapter_test

// 注册器端到端（04 §2.1）：通过空导入触发各子包 init 自注册，验证 Get/All/重复 panic。

import (
	"testing"

	"cloudfog/internal/pkg/adapter"
	"cloudfog/internal/pkg/adapter/anthropic"
	"cloudfog/internal/pkg/adapter/openai"
)

var (
	_ = openai.NewOpenAIProvider // 防止 import 被 gofmt 移除（NewXxx 仅供包外构造/测试重复注册用）
	_ = anthropic.NewAnthropicProvider
)

func TestRegistrySelfRegister(t *testing.T) {
	for _, code := range []string{"openai", "deepseek", "anthropic"} {
		p, ok := adapter.Get(code)
		if !ok || p == nil {
			t.Fatalf("自注册后应能 Get(%q)，got ok=%v", code, ok)
		}
	}
	if _, ok := adapter.Get("no-such-provider"); ok {
		t.Fatal("未知 code 应 Get=false")
	}
	if len(adapter.All()) < 3 {
		t.Fatalf("All() 至少应有 3 个适配器, got %d", len(adapter.All()))
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	// openai 已由 init 自注册，再注册同 code 必须在启动期 panic（04 §2.1）
	defer func() {
		if recover() == nil {
			t.Fatal("重复注册应 panic")
		}
	}()
	adapter.Register(openai.NewOpenAIProvider())
}

func TestProviderProtocolContract(t *testing.T) {
	// 协议判定与 04 §4 供应商清单一致：deepseek 走 openai_compat
	if p, _ := adapter.Get("deepseek"); p.Protocol() != adapter.ProtocolOpenAICompat {
		t.Fatalf("deepseek 应为 openai_compat, got %s", p.Protocol())
	}
	if p, _ := adapter.Get("anthropic"); p.Protocol() != adapter.ProtocolAnthropic {
		t.Fatalf("anthropic 应为 anthropic, got %s", p.Protocol())
	}
}
