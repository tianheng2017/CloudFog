package migrate

import (
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestMigrationFiles(t *testing.T) {
	mfs := fstest.MapFS{
		// 合法 up 迁移（乱序以验证排序）
		"20260906000002_usage_logs.up.sql": &fstest.MapFile{Data: []byte("-- up")},
		"20260906000001_init.up.sql":       &fstest.MapFile{Data: []byte("-- up")},
		// down 对不应被当作 up 解析
		"20260906000001_init.down.sql": &fstest.MapFile{Data: []byte("-- down")},
		// 必须忽略：embed.go 自身、无 .up.sql 后缀的裸 SQL、目录
		"embed.go":                        &fstest.MapFile{Data: []byte("//go:embed")},
		"20260906000003_wrong_naming.sql": &fstest.MapFile{Data: []byte("-- ignored")},
		"some_subdir":                     &fstest.MapFile{Mode: fs.ModeDir},
	}
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	got, err := migrationFiles(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []migrationFile{
		{version: 20260906000001, name: "20260906000001_init.up.sql"},
		{version: 20260906000002, name: "20260906000002_usage_logs.up.sql"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMigrationFilesIgnoreNonMigration(t *testing.T) {
	mfs := fstest.MapFS{
		"foo.up.sql":     &fstest.MapFile{Data: []byte("-- 无下划线，不得 panic，应忽略")},
		"_.up.sql":       &fstest.MapFile{Data: []byte("-- 版本为空，应忽略")},
		"abc_def.up.sql": &fstest.MapFile{Data: []byte("-- 版本非数字，应忽略")},
	}
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	got, err := migrationFiles(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %+v", got)
	}
}

func TestMigrationFilesVersionConflict(t *testing.T) {
	mfs := fstest.MapFS{
		"20260906000001_init.up.sql":      &fstest.MapFile{Data: []byte("-- up")},
		"20260906000001_duplicate.up.sql": &fstest.MapFile{Data: []byte("-- up dup")},
	}
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if _, err := migrationFiles(entries); err == nil {
		t.Fatal("expected version conflict error, got nil")
	}
}
