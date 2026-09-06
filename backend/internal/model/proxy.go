package model

// Proxy 回源代理（02 §4.4）：用于海外供应商的出口 IP 管理。
// password 为加密存储的代理密码，明文不入库。
type Proxy struct {
	ID       int64  `gorm:"primaryKey;column:id"`
	Name     string `gorm:"column:name;type:varchar(100);not null"`
	Type     string `gorm:"column:type;type:varchar(10);not null"` // http | socks5
	Host     string `gorm:"column:host;type:varchar(255);not null"`
	Port     int    `gorm:"column:port;not null"`
	Username string `gorm:"column:username;type:varchar(128);not null;default:''"`
	Password string `gorm:"column:password;type:text;not null;default:''"` // 密文（信封加密，明文不入库）
	// status: active | disabled
	Status string `gorm:"column:status;type:varchar(20);not null;default:'active'"`

	Timestamps
	SoftDelete
}

func (Proxy) TableName() string { return "proxies" }
