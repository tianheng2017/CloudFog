package model

import (
	"time"

	"gorm.io/gorm"
)

// Timestamps 自动管理创建与更新时间；存储层一律 timestamptz（02 §13.1：应用内部一律 UTC，
// 由 db.go 的 NowFunc 强制）。日志/流水类表不嵌入本结构（各自显式定义，见 UsageLog）。
type Timestamps struct {
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;autoUpdateTime"`
}

// SoftDelete 提供软删除（gorm.DeletedAt 自动为查询追加 WHERE deleted_at IS NULL）。
// 仅用于业务实体表；日志/流水/关联表禁止使用（02 §6.1 只追加语义）。
// 部分唯一索引（WHERE deleted_at IS NULL，02 §3.1 email 等）无法用 tag 表达，由迁移 SQL 负责。
type SoftDelete struct {
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;type:timestamptz;index"`
}

// Source 业务实体溯源字段（02 §16 设计要点 3，权威出处）：
//
//	source='builtin' 引导种子 / 'migration' 迁移导入 / 'user' 运营与用户创建（默认）
//	source_id 仅 source='migration' 时有值，记录源系统主键
//
// 两列可空；默认值由应用层填充；引导幂等只覆盖 builtin 记录（01 §10.4）。
type Source struct {
	Source   string  `gorm:"column:source;type:varchar(20);not null;default:'user'"`
	SourceID *string `gorm:"column:source_id;type:varchar(128)"`
}
