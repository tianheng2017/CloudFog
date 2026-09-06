// Package migrations 内嵌全部迁移 SQL，使单二进制自包含（无需运行时挂载目录）。
//
// 命名规则（golang-migrate v4）：
//
//	<version>_<描述>.up.sql     例如 20260906000001_init.up.sql
//	<version>_<描述>.down.sql   例如 20260906000001_init.down.sql
//
// 表结构唯一事实源（02 §11）：模型只是访问层映射；AutoMigrate 全面禁用。
package migrations

import "embed"

//go:embed *
var FS embed.FS
