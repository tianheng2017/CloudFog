// Package ir 统一中间表示（Canonical IR，03 §3.2）。
// 设计原则：覆盖主流对话能力的交集，厂商特性走 Extensions 逃生舱；
// 覆盖多模态/tools 的类型先行定义（字段齐全），B2-5 归一化入口按需填充。
// 本包零依赖（不含 model/repository），是平台内部请求/响应/流事件的唯一形状。
package ir

// Role 消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// PartType 内容片段类型。
type PartType string

const (
	PartText        PartType = "text"
	PartImageURL    PartType = "image_url"
	PartImageBase64 PartType = "image_base64"
	PartFile        PartType = "file"
	PartAudio       PartType = "audio"
)

// FinishReason 结束原因。
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishError         FinishReason = "error"
)

// UsageSource 计量来源（06 §11.1：三态对应 usage_logs.usage_source）。
type UsageSource string

const (
	UsageUpstream  UsageSource = "upstream"
	UsagePartial   UsageSource = "partial"
	UsageEstimated UsageSource = "estimated"
)

// ToolCall 工具调用（assistant 消息内）。
type ToolCall struct {
	ID       string
	Type     string // function
	Function FunctionCall
}

// FunctionCall 工具函数调用参数。
type FunctionCall struct {
	Name      string
	Arguments string // JSON 字符串（OpenAI 惯例）
}

// ContentPart 多模态片段（纯文本时为单元素数组）。
type ContentPart struct {
	Type     PartType
	Text     string
	ImageURL string // URL 或 data URL
	MimeType string // image_base64/file 时必填
	FileRef  string // 平台内对象引用
}

// Message 单条消息。
type Message struct {
	Role       Role
	Content    []ContentPart // 文本时 len=1（{Type: text}）
	System     string        // 便捷字段：入口统一剥离为独立 system 的辅助；Adapter 用顶层 System
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
	Reasoning  string
}

// Tool 工具定义。
type Tool struct {
	Type     string // function
	Function struct {
		Name        string
		Description string
		Parameters  map[string]any // JSON Schema
	}
}

// ResponseFormat 结构化输出约束。
type ResponseFormat struct {
	Type       string // text | json_object | json_schema
	JSONSchema map[string]any
}

// GenerateParams 生成参数（nil 指针 = 未指定，不发/用上游默认）。
type GenerateParams struct {
	MaxTokens        *int
	Temperature      *float64
	TopP             *float64
	TopK             *int
	Stop             []string
	PresencePenalty  *float64
	FrequencyPenalty *float64
	Seed             *int64
	ResponseFormat   *ResponseFormat
	ReasoningEffort  string
	User             string
}

// CanonicalRequest 平台内部统一请求表示（03 §3.2）。
type CanonicalRequest struct {
	Model      string
	Messages   []Message
	System     string
	Tools      []Tool
	ToolChoice any // "auto" | "none" | {type, function:{name}}
	Params     GenerateParams
	Stream     bool
	Extensions map[string]any // 厂商特有字段逃生舱
}

// Usage 用量统计。
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	Source           UsageSource
}

// Choice 非流式单个候选。
type Choice struct {
	Index        int
	Message      Message
	FinishReason FinishReason
}

// CanonicalResponse 非流式统一响应。
type CanonicalResponse struct {
	ID           string
	Model        string // 上游返回的模型名
	Choices      []Choice
	Usage        Usage
	FinishReason FinishReason
	Extensions   map[string]any
}

// StreamEventType 流事件类型。
type StreamEventType string

const (
	EvDelta          StreamEventType = "delta"
	EvReasoningDelta StreamEventType = "reasoning_delta"
	EvToolCallDelta  StreamEventType = "tool_call_delta"
	EvUsage          StreamEventType = "usage"
	EvError          StreamEventType = "error"
	EvDone           StreamEventType = "done"
)

// ToolCallDelta 流式工具调用增量。
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// StreamEvent 流式事件（Adapter 解码上游 → 统一事件；编码器再还原为出口格式）。
type StreamEvent struct {
	Type          StreamEventType
	Delta         string
	ToolCallDelta *ToolCallDelta
	Usage         *Usage
	FinishReason  FinishReason
	Err           error // EvError 时携带
}
