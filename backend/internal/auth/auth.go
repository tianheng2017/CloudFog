// Package auth API Key 鉴权判定（08 §3.2 校验与吊销）。
// 职责边界：纯判定 + 上下文携带，不感知 HTTP/Gin/Redis。
//   - 凭证格式 sk-cf-<22位随机串>；key_hash = SHA-256(salt || 明文)，明文不入库（08 §3.1）；
//   - 校验序列（08 §3.2）：命中 key → status / expires_at / IP 白名单 → user 有效 → group 有效；
//   - Redis keyctx 缓存是 B3 限流层的性能优化（08 §3.2 主路径直查 PG 为合法降级语义），本包不关心。
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"cloudfog/internal/model"
)

// Lookup 数据访问（*repository.Repository 实现）。consumer-side 接口便于中间件单测。
type Lookup interface {
	APIKeyByHash(ctx context.Context, hash string) (*model.APIKey, error)
	UserByID(ctx context.Context, id int64) (*model.User, error)
	GroupByID(ctx context.Context, id int64) (*model.Group, error)
}

// Principal 鉴权成功后携带的请求上下文（key + 归属 user + 计费/路由组）。
type Principal struct {
	Key   *model.APIKey
	User  *model.User
	Group *model.Group
}

// Code 鉴权失败码（对外 HTTP 映射在中间件层：401 invalid_api_key / 403 ip_not_allowed）。
type Code string

const (
	CodeInvalidAPIKey  Code = "invalid_api_key" // 未知/禁用/过期/user 或 group 不可用，统一 401
	CodeIPNotAllowed   Code = "ip_not_allowed"  // 命中 key 但来源 IP 不在白名单，403
	CodeSessionExpired Code = "session_expired" // 会话缺失/过期/无效（portal 自助 07 §3.0）
)

// Error 结构化鉴权错误（可被 errors.As 提取做中间件响应判定）。
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// HashKey 计算 key_hash = SHA-256(salt || 明文) 的 64 位 hex（与 02 §3.3 key_hash 列一致）。
func HashKey(salt, plain string) string {
	sum := sha256.Sum256([]byte(salt + plain))
	return hex.EncodeToString(sum[:])
}

// BearerToken 从 Authorization 头解析明文 key。仅接受 "Bearer <token>"（大小写不敏感）。
// 无头/非 Bearer/空 token → (empty, false)；具体语义由调用方决定（对需鉴权路由即 401）。
func BearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	h := strings.TrimSpace(header)
	if len(h) <= len(prefix) {
		return "", false
	}
	if !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// Authenticate 执行完整鉴权判定，成功返回 Principal。
// clientIP 为空串且 key 配了 IP 白名单时视为不在白名单（保守拒绝）。
// DB 错误原样返回（基础设施错误，不属于"凭证无效"）。
func Authenticate(ctx context.Context, s Lookup, salt, bearer, clientIP string) (*Principal, error) {
	if salt == "" {
		return nil, errors.New("auth: security.api_key_salt 未配置，无法校验密钥")
	}
	key, err := s.APIKeyByHash(ctx, HashKey(salt, bearer))
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "API key 不存在"}
	}
	now := time.Now().UTC()
	switch {
	case key.Status == "disabled":
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "API key 已被禁用"}
	case key.Status == "expired":
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "API key 已过期"}
	case key.Status != "active":
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "API key 状态异常"}
	case key.ExpiresAt != nil && now.After(*key.ExpiresAt):
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "API key 已过期"}
	case len(key.IPWhitelist) > 0 && !ipAllowed(clientIP, key.IPWhitelist):
		return nil, &Error{Code: CodeIPNotAllowed, Message: "来源 IP 不在密钥白名单"}
	}

	user, err := s.UserByID(ctx, key.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != "active" {
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "用户不可用"}
	}
	group, err := s.GroupByID(ctx, key.GroupID)
	if err != nil {
		return nil, err
	}
	if group == nil || group.Status != "active" {
		return nil, &Error{Code: CodeInvalidAPIKey, Message: "所属分组不可用"}
	}
	return &Principal{Key: key, User: user, Group: group}, nil
}

// ipAllowed 判断来源 IP 是否命中任一白名单项（CIDR 或裸 IP；裸 IP 视为单地址前缀）。
// 解析失败的白名单项跳过（配置错误不应拖垮整条鉴权，宁可保守拒绝靠告警暴露）。
func ipAllowed(clientIP string, cidrs []string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return false // 空/畸形来源 IP：保守拒绝
	}
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") { // 裸 IP
			a, err := netip.ParseAddr(c)
			if err == nil && a == addr {
				return true
			}
			continue
		}
		p, err := netip.ParsePrefix(c)
		if err == nil && p.Contains(addr) {
			return true
		}
	}
	return false
}

type principalCtxKey struct{}

// WithPrincipal 把鉴权结果挂到 ctx（handler 层经 PrincipalFrom 取用）。
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// PrincipalFrom 从 ctx 取鉴权结果；未鉴权返回 (nil, false)。
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(*Principal)
	return p, ok
}
