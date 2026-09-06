// Package migrate 以 library 形式集成 golang-migrate v4（10 §3.5 基线）。
// 提供 cloudfog migrate up | down N | status 三个子命令的底层逻辑。
// 迁移文件经 //go:embed 内嵌（见 cloudfog/migrations），生产单二进制自包含。
package migrate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // 注册 postgres 驱动（DSN scheme 匹配用）
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"cloudfog/migrations"
)

// New 构造迁移执行器。dsn 形如 postgres://user:pass@host:5432/db?sslmode=disable。
func New(dsn string) (*migrate.Migrate, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return nil, fmt.Errorf("migrate: DSN 必须以 postgres:// 开头")
	}
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: 加载内嵌迁移文件: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return nil, fmt.Errorf("migrate: 初始化失败: %w", err)
	}
	return m, nil
}

// Up 应用全部待执行迁移。无待执行迁移时视为成功（Idempotent，02 §11 / 12 §5.1）。
func Up(dsn string) error {
	m, err := New(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }() // 释放连接：错误无传播语义（返回路径已显式处理）
	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up 失败: %w", err)
	}
	return nil
}

// Down 回滚 steps 步（CLI 只允许显式回滚，禁止全部回滚）。
func Down(dsn string, steps int) error {
	if steps <= 0 {
		return fmt.Errorf("migrate down 需要正整数步数（如 down 1）")
	}
	m, err := New(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }() // 释放连接：错误无传播语义（返回路径已显式处理）
	err = m.Steps(-steps)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down 失败: %w", err)
	}
	return nil
}

// Status 打印每个迁移的应用状态（version: dirty | applied | pending）。
func Status(dsn string, out io.Writer) error {
	m, err := New(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }() // 释放连接：错误无传播语义（返回路径已显式处理）

	current, dirty, verr := m.Version()
	if verr != nil && !errors.Is(verr, migrate.ErrNilVersion) {
		return fmt.Errorf("migrate status 失败: %w", verr)
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	list, err := migrationFiles(entries)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, "%-24s %-40s %s\n", "VERSION", "FILE", "STATE"); err != nil {
		return err
	}
	for _, mi := range list {
		state := "pending"
		if mi.version < current || (mi.version == current && verr == nil) {
			state = "applied"
		}
		if dirty && mi.version == current {
			state = "applied (dirty!)"
		}
		if _, err := fmt.Fprintf(out, "%-24d %-40s %s\n", mi.version, mi.name, state); err != nil {
			return err
		}
	}
	if len(list) == 0 {
		if _, err := fmt.Fprintln(out, "（无迁移文件）"); err != nil {
			return err
		}
	}
	return nil
}

// migrationFile 是内嵌目录中一个 up 迁移的元信息。
type migrationFile struct {
	version uint
	name    string
}

// migrationFiles 从目录条目中解析全部 up 迁移（去重后按版本升序）。
// 只认 <版本>_<描述>.up.sql；其余文件（含 embed.go 自身、误放的裸 .sql）一律忽略——
// 命名不符的迁移文件 golang-migrate 也会静默跳过，绝不可作为唯一入口文件。
func migrationFiles(entries []fs.DirEntry) ([]migrationFile, error) {
	seen := make(map[uint]string)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		idx := strings.Index(name, "_")
		if idx <= 0 { // 无 "_" 或版本为空：非法命名，忽略（与 migrate 行为一致）
			continue
		}
		v64, err := strconv.ParseUint(name[:idx], 10, 64)
		if err != nil {
			continue
		}
		v := uint(v64)
		// 版本冲突（同名 .up/.down 对之外的重复）视为错误，避免静默选其一
		if prev, dup := seen[v]; dup && prev != name {
			return nil, fmt.Errorf("迁移版本冲突: %s 与 %s 使用同一版本号 %d", prev, name, v)
		}
		seen[v] = name
	}
	list := make([]migrationFile, 0, len(seen))
	for v, n := range seen {
		list = append(list, migrationFile{version: v, name: n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	return list, nil
}

// RequireSchemaVersion 启动引导用：校验数据库 schema 已迁移到不低于 want 的版本，
// 未达标则返回错误并提示执行 cloudfog migrate up（01 §10 启动迁移检查）。
func RequireSchemaVersion(dsn string, want uint64) error {
	m, err := New(dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }() // 释放连接：错误无传播语义（返回路径已显式处理）
	v, _, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return fmt.Errorf("数据库尚未执行任何迁移，请先运行 cloudfog migrate up（目标版本 ≥ %d）", want)
		}
		return fmt.Errorf("读取迁移版本失败: %w", err)
	}
	if uint64(v) < want {
		return fmt.Errorf("数据库 schema 版本 %d 低于所需 %d，请先运行 cloudfog migrate up", v, want)
	}
	return nil
}
