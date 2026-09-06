// Package password 密码哈希（02 §3.1 password_algo：bcrypt / argon2id / md5_legacy）。
// 当前标准算法 = argon2id（PHC 字符串自描述）；md5_legacy 仅校验（登录成功后渐进升级）。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// argon2id 参数（OWASP 建议基线；成本随硬件在部署时校准）
	aTime    = 1
	aMemory  = 64 * 1024 // 64 MiB
	aThreads = 4
	aKeyLen  = 32
)

// ErrMismatch 密码不匹配。
var ErrMismatch = errors.New("password: 密码不匹配")

// Hash 用 argon2id 生成 PHC 编码哈希（格式：$argon2id$v=19$m=...,t=...,p=...$salt$hash）。
func Hash(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("password: 明文不能为空")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: 生成盐失败: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, aTime, aMemory, aThreads, aKeyLen)
	enc := func(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, aMemory, aTime, aThreads, enc(salt), enc(key)), nil
}

// Verify 校验密码。返回 algo（当前实际算法名），供上层判断是否需要渐进升级。
func Verify(plain, encoded string) (algo string, err error) {
	if plain == "" || encoded == "" {
		return "", ErrMismatch
	}
	if strings.HasPrefix(encoded, "$argon2id$") {
		ok, err := verifyArgon2(plain, encoded)
		if err != nil {
			return "", err
		}
		if !ok {
			return "argon2id", ErrMismatch
		}
		return "argon2id", nil
	}
	if strings.HasPrefix(encoded, "$2a$") || strings.HasPrefix(encoded, "$2b$") {
		return "bcrypt", errors.New("password: bcrypt 校验未启用（迁移用户按 13-operations §5.3 引入）")
	}
	if len(encoded) == 32 { // 兼容 md5_legacy 存量（仅拒绝明文误存判断用）
		return "md5_legacy", errors.New("password: md5_legacy 需走迁移工具校验，禁止直连比对")
	}
	return "", ErrMismatch
}

func verifyArgon2(plain, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false, errors.New("password: 非法 argon2id PHC")
	}
	var version int
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}
	// DoS 防护：参数上限（防被注入巨额 m/t 的恶意哈希触发内存/CPU 耗尽，见 argon2 建议）
	if memory > 512<<10 || time > 16 || threads > 16 || threads == 0 {
		return false, errors.New("password: argon2id 参数超出安全上限")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
