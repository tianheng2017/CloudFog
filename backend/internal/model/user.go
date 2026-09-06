package model

import "time"

// User 用户（02 §3.1）。索引：
//
//	email / username：唯一部分索引 WHERE deleted_at IS NULL —— 迁移 SQL 手工建（模型无法表达部分唯一）
//	非部分普通索引（status、role）由迁移 SQL 创建；模型 tag 仅作声明性标注。
type User struct {
	ID    int64  `gorm:"primaryKey;column:id"`
	Email string `gorm:"column:email;type:varchar(255);not null"`
	// Phone 可空（02 §3.1：可空，唯一）
	Phone    *string `gorm:"column:phone;type:varchar(32)"`
	Username string  `gorm:"column:username;type:varchar(64);not null"`
	// password_hash 可空：纯 OAuth 用户（08 §4.1）
	PasswordHash *string `gorm:"column:password_hash;type:varchar(255)"`
	// password_algo 哈希算法标识：bcrypt / argon2id / md5_legacy（13-operations §5.3）
	PasswordAlgo string `gorm:"column:password_algo;type:varchar(20);not null;default:'argon2id'"`
	Role         string `gorm:"column:role;type:varchar(20);not null;default:'user';index:idx_users_role"` // user | admin | super_admin
	// status: active | disabled | pending（pending=待邮箱验证，user:cleanup 清理超时未验证）
	Status         string `gorm:"column:status;type:varchar(20);not null;default:'pending';index:idx_users_status"`
	DefaultGroupID *int64 `gorm:"column:default_group_id"`

	// timezone IANA 名（如 Asia/Shanghai）；决定看板与账单的账期切分口径（02 §13.2）
	Timezone string `gorm:"column:timezone;type:varchar(64);not null;default:'UTC'"`

	// version 全局版本号：禁用/角色变更/风控处置时递增，使该用户所有 Key 的鉴权缓存一次性失效（08 §3.2）
	Version int64 `gorm:"column:version;not null;default:0"`

	// risk_level: normal | watch | restricted | blocked；watch 以上触发更严格限流（13-operations §7）
	RiskLevel string `gorm:"column:risk_level;type:varchar(20);not null;default:'normal'"`
	// concurrency_limit 用户级并发上限，0 表示跟随分组
	ConcurrencyLimit int        `gorm:"column:concurrency_limit;not null;default:0"`
	LastLoginAt      *time.Time `gorm:"column:last_login_at;type:timestamptz"`

	Timestamps
	SoftDelete
	Source
}

func (User) TableName() string { return "users" }
