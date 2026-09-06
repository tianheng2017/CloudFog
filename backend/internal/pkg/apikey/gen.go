// Package apikey 平台 API Key 生成（08 §3.1）。
// 明文格式 sk-cf-<22 位 Base62 随机串>（总熵≈131bit）；key_prefix = 明文前 10 位用于列表展示。
package apikey

import (
	"crypto/rand"
	"math/big"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Generate 生成新明文 Key 与其展示前缀。哈希（SHA-256(salt||plain)）由调用方 auth.HashKey 完成。
func Generate() (plain, prefix string, err error) {
	buf := make([]byte, 22)
	max := big.NewInt(int64(len(alphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", "", err
		}
		buf[i] = alphabet[n.Int64()]
	}
	plain = "sk-cf-" + string(buf)
	return plain, plain[:10], nil
}
