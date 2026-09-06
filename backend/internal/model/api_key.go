package model

import "time"

// APIKey 平台下发密钥（02 §3.3）。明文只在创建时展示一次；入库仅存 SHA-256(salt+明文) 哈希。
// 索引：key_hash 唯一；复合 (user_id,status)、(status,expires_at) —— 迁移 SQL 建。
type APIKey struct {
	ID      int64  `gorm:"primaryKey;column:id"`
	UserID  int64  `gorm:"column:user_id;not null;index:idx_api_keys_user_status,priority:1"`
	GroupID int64  `gorm:"column:group_id;not null"` // 决定可见渠道与倍率
	Name    string `gorm:"column:name;type:varchar(100);not null"`

	// KeyPrefix 明文前缀（sk-cf-ab12，共 10 位），用于列表展示（08 §3.2）
	KeyPrefix string `gorm:"column:key_prefix;type:varchar(16);not null"`
	// KeyHash 唯一，SHA-256(salt+明文)；明文不入库
	KeyHash string `gorm:"column:key_hash;type:varchar(64);not null;uniqueIndex:uk_api_keys_key_hash"`

	// status: active | disabled | expired（expired 由 key:expire 周期任务扫描置位，01 §7.2）
	Status    string     `gorm:"column:status;type:varchar(20);not null;default:'active';index:idx_api_keys_user_status,priority:2;index:idx_api_keys_status_expires,priority:1"`
	ExpiresAt *time.Time `gorm:"column:expires_at;type:timestamptz;index:idx_api_keys_status_expires,priority:2"`

	IPWhitelist    []string `gorm:"column:ip_whitelist;type:jsonb;serializer:json"`    // CIDR 数组，可空
	ModelWhitelist []string `gorm:"column:model_whitelist;type:jsonb;serializer:json"` // 覆盖分组限制，可空
	// QuotaDailyUSD 单 key 日额度（USD），可空
	QuotaDailyUSD *Decimal `gorm:"column:quota_daily_usd;type:numeric(20,10)"`
	// RPM/TPM 可空，覆盖用户级限制
	RPMLimit *int `gorm:"column:rpm_limit"`
	TPMLimit *int `gorm:"column:tpm_limit"`

	LastUsedAt *time.Time `gorm:"column:last_used_at;type:timestamptz"`

	Timestamps
	SoftDelete
	Source
}

func (APIKey) TableName() string { return "api_keys" }
