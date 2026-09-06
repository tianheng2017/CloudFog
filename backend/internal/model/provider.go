package model

// Provider 供应商元数据（02 §4.1）。code 唯一。disabled 时其下所有渠道不参与调度。
// auth_type=signature 时由 Adapter 负责 token 换取与缓存（04 §4.1）。
type Provider struct {
	ID   int64  `gorm:"primaryKey;column:id"`
	Code string `gorm:"column:code;type:varchar(50);not null;uniqueIndex:uk_providers_code"`
	Name string `gorm:"column:name;type:varchar(100);not null"`

	// protocol: openai_compat | anthropic | gemini_native
	Protocol string `gorm:"column:protocol;type:varchar(30);not null"`
	BaseURL  string `gorm:"column:base_url;type:varchar(512);not null"`
	// auth_type: bearer | header | oauth | signature
	AuthType string `gorm:"column:auth_type;type:varchar(20);not null;default:'bearer'"`

	// capabilities 支持的能力集：stream / tools / vision / reasoning / embedding / image_gen
	Capabilities []string `gorm:"column:capabilities;type:jsonb;serializer:json;not null;default:'[]'"`
	DocsURL      string   `gorm:"column:docs_url;type:varchar(512);not null;default:''"`

	// BillOnFailure 对失败请求是否计费（默认 false，06 §11.4）
	BillOnFailure bool `gorm:"column:bill_on_failure;not null;default:false"`
	// BillPartialStream 流式中途失败按已输出部分计费（默认 true）
	BillPartialStream bool `gorm:"column:bill_partial_stream;not null;default:true"`
	// UsageReliable 上游 usage 是否可信（默认 true；false 时本地估算为主，06 §11.3）
	UsageReliable bool `gorm:"column:usage_reliable;not null;default:true"`

	// status: active | disabled
	Status string `gorm:"column:status;type:varchar(20);not null;default:'active'"`

	Timestamps
	SoftDelete
	Source
}

func (Provider) TableName() string { return "providers" }
