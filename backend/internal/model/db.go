// Package model 数据访问层入口（01 §6 repository 依赖方向）：
// gorm.Open（pgx 驱动）+ 连接池参数。表结构唯一事实源是 migrations/ SQL（02 §11），
// 本包仅做连接与池管理，AutoMigrate 全面禁用。
package model

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Options 连接选项（来自 config.DatabaseConfig）。
type Options struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	// LogLevel: silent | error | warn | info（默认 warn，GORM SQL 日志走 slog 前先保守使用内置 logger）
	LogLevel string
}

// Open 建立 GORM 连接并配置连接池。
func Open(opts Options) (*gorm.DB, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("model: DSN 不能为空")
	}
	db, err := gorm.Open(postgres.Open(opts.DSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(parseLogLevel(opts.LogLevel)),
		// 02 §13.1：应用内部一律 UTC——autoCreateTime/autoUpdateTime 强制 UTC
		NowFunc: func() time.Time { return time.Now().UTC() },
		// 02 §11 铁律：表结构唯一事实源是 migrations/，业务代码一律不得调用 AutoMigrate。
		// gorm.Config 无字段可"禁用"它——纪律靠约定 + 代码评审 + 模型↔表结构一致性测试（12 §5.1）。
	})
	if err != nil {
		return nil, fmt.Errorf("model: 连接 PostgreSQL 失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("model: 获取底层 sql.DB 失败: %w", err)
	}
	if opts.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	}
	if opts.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	}
	if opts.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)
	}
	return db, nil
}

// Ping 健康检查（/readyz 用，09 §7）。
func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func parseLogLevel(s string) gormlogger.LogLevel {
	switch s {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "warn":
		return gormlogger.Warn
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}
