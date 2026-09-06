// Package crypto 上游凭证信封加密（08 §2）。
// 信封方案：随机数据密钥 DK 用 AES-256-GCM 加密凭证（AAD=渠道标识防密文移植），
// DK 再用主密钥 MK 加密得到 EDK 随记录存储（DB 泄露≠凭证泄露）。主密钥不入库，
// 配置经 security.master_key / previous_master_key（轮换期双密钥并存，10 §3.2）。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// sealedJSON 落库 JSON 形状（credentials jsonb 内，02 §2.2 字段清单）。
// 非加密的额外字段（如 extra）直接并列在外层同 map（Open 后合并返回）。
type sealedJSON struct {
	KeyID  string   `json:"key_id"`
	EDK    string   `json:"edk"`    // base64(12B nonce ‖ AES-GCM(MK, DK))
	Nonce  string   `json:"nonce"`  // 凭证层 GCM nonce base64（12B，每次加密随机）
	CT     string   `json:"ct"`     // base64(AES-GCM(DK, 凭证JSON, AAD))，密文含 16B tag
	Fields []string `json:"fields"` // 被加密的字段名（供展示/审计，不明文值）
}

// envelopeKeys sealed 标记依赖的键（fields 外）。
var sealedKeys = map[string]bool{"key_id": true, "edk": true, "nonce": true, "ct": true, "fields": true}

// IsSealed 判断 credentials jsonb 是否为信封形态（含 edk 即视为密封；legacy 明文无此键）。
func IsSealed(blob map[string]any) bool {
	if blob == nil {
		return false
	}
	if _, ok := blob["edk"].(string); !ok {
		return false
	}
	return true
}

// Seal 用信封加密 fields 全量明文键（AAD 建议 "channel:<id>"，防密文跨渠道移植）。
// 返回可直接写入 credentials jsonb 的 map（含信封结构；不含未加密 extra——调用方自行并列）。
func Seal(fields map[string]any, masterKeyHex, keyID, aad string) (map[string]any, error) {
	mk, err := decodeMasterKey(masterKeyHex)
	if err != nil {
		return nil, err
	}
	if keyID == "" {
		return nil, errors.New("crypto: key_id 不能为空（信封 cred_key_id）")
	}
	// 凭证载荷：字段名排序保证 JSON 稳定
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	payload := make(map[string]any, len(fields))
	for _, k := range keys {
		payload[k] = fields[k]
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("crypto: 凭证序列化失败: %w", err)
	}
	// 随机数据密钥 DK
	dk := make([]byte, 32)
	if _, err := rand.Read(dk); err != nil {
		return nil, fmt.Errorf("crypto: 随机数失败: %w", err)
	}
	// EDK = base64(nonce12 ‖ AES-GCM(MK, DK, AAD=nil))，nonce 前置便于轮换期探测。
	edkRaw, edkNonce, err := gcmSeal(mk, dk, nil)
	if err != nil {
		return nil, err
	}
	edk := base64.StdEncoding.EncodeToString(append(edkNonce, edkRaw...))
	// 凭证密文 = AES-GCM(DK, plain, AAD=渠道标识)
	ctRaw, nonce, err := gcmSeal(dk, plain, []byte(aad))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"key_id": keyID,
		"edk":    edk,
		"nonce":  base64.StdEncoding.EncodeToString(nonce),
		"ct":     base64.StdEncoding.EncodeToString(ctRaw),
		"fields": keys,
	}, nil
}

// Open 解密信封（按 key_id 自动选当前/上一代主密钥，轮换期双密钥并存）。
// 非密封明文直接原样返回；解密成功会把外层非信封字段（extra 等明文并列项）合并进结果。
// AAD 不一致/密钥错误返回明确错误（密文不可移植到其它 AAD）。
func Open(blob map[string]any, masterKeyHex, previousMasterKeyHex, aad string) (map[string]any, error) {
	if !IsSealed(blob) {
		return blob, nil // legacy 明文
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		return nil, err
	}
	var env sealedJSON
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("crypto: 信封结构非法: %w", err)
	}
	mks := []string{}
	if masterKeyHex != "" {
		mks = append(mks, masterKeyHex)
	}
	if previousMasterKeyHex != "" && previousMasterKeyHex != masterKeyHex {
		mks = append(mks, previousMasterKeyHex)
	}
	// EDK 解数据密钥：当前优先，失败尝试上一代（轮换期旧记录仍可读）
	edkRaw, err := base64.StdEncoding.DecodeString(env.EDK)
	if err != nil || len(edkRaw) < 12+16 {
		return nil, errors.New("crypto: EDK 格式非法")
	}
	edkNonce, edkCT := edkRaw[:12], edkRaw[12:]
	var dk []byte
	var lastErr error
	for _, h := range mks {
		mk, err := decodeMasterKey(h)
		if err != nil {
			lastErr = err
			continue
		}
		dk, err = gcmOpen(mk, edkNonce, edkCT, nil)
		if err == nil {
			break
		}
		lastErr = err
	}
	if dk == nil {
		return nil, fmt.Errorf("crypto: 数据密钥解密失败（主密钥不匹配或已轮换）: %w", lastErr)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, errors.New("crypto: nonce 格式非法")
	}
	ctRaw, err := base64.StdEncoding.DecodeString(env.CT)
	if err != nil {
		return nil, errors.New("crypto: 密文格式非法")
	}
	plain, err := gcmOpen(dk, nonce, ctRaw, []byte(aad))
	if err != nil {
		return nil, fmt.Errorf("crypto: 凭证解密失败（AAD/密钥不符，禁止跨渠道移植）: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(plain, &fields); err != nil {
		return nil, fmt.Errorf("crypto: 凭证载荷解析失败: %w", err)
	}
	// 合并外层非信封明文键（extra 等）
	for k, v := range blob {
		if !sealedKeys[k] {
			fields[k] = v
		}
	}
	return fields, nil
}

// decodeMasterKey hex 64（32 字节）→ bytes。
func decodeMasterKey(hexKey string) ([]byte, error) {
	if len(hexKey) != 64 {
		return nil, errors.New("crypto: 主密钥必须是 64 位 hex（32 字节）")
	}
	b, err := hex.DecodeString(hexKey)
	if err != nil || len(b) != 32 {
		return nil, errors.New("crypto: 主密钥 hex 解码失败")
	}
	return b, nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// gcmSeal 随机 nonce 加密，返回原始密文字节与 nonce（编码由调用方按用途决定）。
func gcmSeal(key, plain, aad []byte) (raw []byte, nonce []byte, err error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return aead.Seal(nil, nonce, plain, aad), nonce, nil
}

func gcmOpen(key, nonce, raw, aad []byte) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, raw, aad)
}
