package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// LoadDotEnv 加载 .env 文件（10 §1.3：项目根目录创建 .env）。
// 语义：已存在的真实环境变量优先（不覆盖）——保证容器/CI 注入的值压过本地 .env。
// 格式：KEY=VALUE；支持整行 # 注释、未加引号值的行内 " # " 注释（godotenv 语义，
// 含 # 的密码请用引号包裹）、export 前缀、引号值；空行跳过；UTF-8 BOM 自动剥离。
func LoadDotEnv(paths ...string) error {
	list := paths
	if len(list) == 0 {
		list = []string{".env"}
	}
	for _, path := range list {
		if err := loadOneDotEnv(path); err != nil {
			return err
		}
	}
	return nil
}

func loadOneDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // .env 不存在不是错误（CI/容器场景）
		}
		return err
	}
	defer func() { _ = f.Close() }() // 只读解析：错误经返回值传播

	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			// UTF-8 BOM 剥离：Windows 编辑器保存的 .env 首个键名会带 \uFEFF 前缀而静默失效
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if n := len(v); n >= 2 && (v[0] == '"' || v[0] == '\'') {
			// 引号值：取首尾同类引号之间的内容（`"pa#ss" # note` → pa#ss），
			// 引号内的 # 与行内注释都不受影响
			if end := strings.LastIndexByte(v, v[0]); end > 0 {
				v = v[1:end]
			}
		} else if i := strings.Index(v, " #"); i >= 0 {
			// 未加引号值的行内注释剥离（godotenv 语义）；含 # 的密码必须用引号包裹
			v = strings.TrimSpace(v[:i])
		}
		if k == "" {
			continue
		}
		// 真实环境变量优先：已存在则不覆盖
		if _, exists := os.LookupEnv(k); !exists {
			if err := os.Setenv(k, v); err != nil {
				return err // 不静默忽略：设置失败必须暴露
			}
		}
	}
	return sc.Err()
}
