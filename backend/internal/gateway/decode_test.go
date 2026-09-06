package gateway

import (
	"strings"
	"testing"
)

func TestDecodeChatOpenAI(t *testing.T) {
	// 基础对话（含 system 上提 + 多 content part）
	req, err := DecodeChatOpenAI(strings.NewReader(`{
		"model":"gpt-test","stream":false,
		"messages":[
			{"role":"system","content":"你是助手"},
			{"role":"user","content":[{"type":"text","text":"hi"}]}
		],
		"temperature":0.5,"max_tokens":64
	}`))
	if err != nil {
		t.Fatalf("基础对话解码失败: %v", err)
	}
	if req.Model != "gpt-test" || req.System != "你是助手" || req.Stream {
		t.Fatalf("基础字段解析异常: %+v", req)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 1 || req.Messages[0].Content[0].Text != "hi" {
		t.Fatalf("user 消息解析异常: %+v", req.Messages)
	}
	if req.Params.MaxTokens == nil || *req.Params.MaxTokens != 64 {
		t.Fatal("max_tokens 未透传")
	}
}

func TestDecodeChatOpenAIToolsExplicitReject(t *testing.T) {
	// function calling 未接线：显式拒绝而非静默丢弃（否则照常计费却无 tool_calls，功能静默失灵）
	cases := []string{
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"f"}}]}`,
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"tool_choice":"auto"}`,
		`{"model":"m","messages":[{"role":"tool","tool_call_id":"c1","content":"r"},{"role":"assistant","content":"a"}]}`,
	}
	for i, body := range cases {
		if _, err := DecodeChatOpenAI(strings.NewReader(body)); err == nil {
			t.Fatalf("用例 %d 应被显式拒绝（tools/tool_choice/tool 消息）", i)
		}
	}
}
