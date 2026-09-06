package model

// Model 模型规格（02 §5.1）。name 唯一（部分索引 WHERE deleted_at IS NULL，迁移 SQL 建）。
type Model struct {
	ID           int64  `gorm:"primaryKey;column:id"`
	Name         string `gorm:"column:name;type:varchar(100);not null"` // 对外模型名，如 gpt-4o
	ProviderCode string `gorm:"column:provider_code;type:varchar(50);not null;index:idx_models_provider"`
	DisplayName  string `gorm:"column:display_name;type:varchar(100);not null;default:''"`

	ContextWindow   int      `gorm:"column:context_window;not null;default:0"` // 上下文窗口（token）
	MaxOutputTokens int      `gorm:"column:max_output_tokens;not null;default:0"`
	Capabilities    []string `gorm:"column:capabilities;type:jsonb;serializer:json;not null;default:'[]'"` // vision / tools / reasoning / json_mode / audio / image_gen
	BillingMode     string   `gorm:"column:billing_mode;type:varchar(20);not null;default:'token'"`        // token | per_request | image
	Fallbacks       []string `gorm:"column:fallbacks;type:jsonb;serializer:json"`                          // 模型级降级链（模型名数组），05 §9
	AvgFirstTokenMS int      `gorm:"column:avg_first_token_ms;not null;default:0"`                         // stats:aggregate 每日回填（01 §7.2）
	Status          string   `gorm:"column:status;type:varchar(20);not null;default:'active'"`             // active | deprecated | hidden
	SortOrder       int      `gorm:"column:sort_order;not null;default:0"`

	Timestamps
	SoftDelete
	Source
}

func (Model) TableName() string { return "models" }
