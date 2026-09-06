package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudfog/internal/model"
)

// ── 纯函数单测 ─────────────────────────────────────────────

func TestHashKeyDeterministic(t *testing.T) {
	salt, plain := "s3cret-salt-0123456789abcdef", "sk-cf-testkey"
	h1, h2 := HashKey(salt, plain), HashKey(salt, plain)
	if h1 != h2 {
		t.Fatalf("同输入应同哈希: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("hash 应为 64 位 hex, got %d", len(h1))
	}
	if HashKey("other-salt-xxxxxxxxxxxxxxxx", plain) == h1 {
		t.Fatal("不同 salt 应不同哈希")
	}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		name, header string
		wantTok      string
		wantOK       bool
	}{
		{"标准", "Bearer sk-cf-abc", "sk-cf-abc", true},
		{"大小写", "bearer sk-cf-abc", "sk-cf-abc", true},
		{"多余空白", "  Bearer   sk-cf-abc  ", "sk-cf-abc", true},
		{"缺失", "", "", false},
		{"Basic", "Basic dXNlcg==", "", false},
		{"空 token", "Bearer   ", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok, ok := BearerToken(c.header)
			if tok != c.wantTok || ok != c.wantOK {
				t.Fatalf("BearerToken(%q) = (%q,%v), want (%q,%v)", c.header, tok, ok, c.wantTok, c.wantOK)
			}
		})
	}
}

func TestIPAllowed(t *testing.T) {
	cases := []struct {
		name, ip string
		list     []string
		want     bool
	}{
		{"裸 IP 命中", "1.2.3.4", []string{"1.2.3.4"}, true},
		{"CIDR 命中", "10.0.0.7", []string{"10.0.0.0/24"}, true},
		{"CIDR 未命中", "10.0.1.7", []string{"10.0.0.0/24"}, false},
		{"列表任一命中", "8.8.8.8", []string{"10.0.0.0/8", "8.8.8.0/24"}, true},
		{"空来源保守拒绝", "", []string{"10.0.0.0/8"}, false},
		{"坏 CIDR 项跳过", "9.9.9.9", []string{"not-a-cidr", "9.9.9.0/24"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ipAllowed(c.ip, c.list); got != c.want {
				t.Fatalf("ipAllowed(%q,%v) = %v, want %v", c.ip, c.list, got, c.want)
			}
		})
	}
}

// ── Authenticate 表驱动（fake Lookup）─────────────────────────

type fakeLookup struct {
	keys              map[string]*model.APIKey // by key_hash
	users             map[int64]*model.User
	groups            map[int64]*model.Group
	userErr, groupErr error
}

func (f *fakeLookup) APIKeyByHash(_ context.Context, hash string) (*model.APIKey, error) {
	return f.keys[hash], nil
}
func (f *fakeLookup) UserByID(_ context.Context, id int64) (*model.User, error) {
	if f.userErr != nil {
		return nil, f.userErr
	}
	return f.users[id], nil
}
func (f *fakeLookup) GroupByID(_ context.Context, id int64) (*model.Group, error) {
	if f.groupErr != nil {
		return nil, f.groupErr
	}
	return f.groups[id], nil
}

const testSalt = "test-salt-0123456789abcdef"

func nowUTC() time.Time { return time.Now().UTC() }
func inFuture() *time.Time {
	tt := nowUTC().Add(24 * time.Hour)
	return &tt
}

func TestAuthenticate(t *testing.T) {
	activeUser := &model.User{ID: 1, Status: "active"}
	disabledUser := &model.User{ID: 2, Status: "disabled"}
	activeGroup := &model.Group{ID: 10, Status: "active"}
	disabledGroup := &model.Group{ID: 11, Status: "disabled"}

	store := &fakeLookup{keys: map[string]*model.APIKey{}, users: map[int64]*model.User{1: activeUser, 2: disabledUser}, groups: map[int64]*model.Group{10: activeGroup, 11: disabledGroup}}
	seed := func(plain string, key *model.APIKey) {
		store.keys[HashKey(testSalt, plain)] = key
	}

	seed("sk-cf-good", &model.APIKey{ID: 1, UserID: 1, GroupID: 10, Status: "active", ExpiresAt: inFuture()})
	seed("sk-cf-disabled", &model.APIKey{ID: 2, UserID: 1, GroupID: 10, Status: "disabled"})
	exp := nowUTC().Add(-time.Hour)
	seed("sk-cf-expired", &model.APIKey{ID: 3, UserID: 1, GroupID: 10, Status: "active", ExpiresAt: &exp})
	seed("sk-cf-ip", &model.APIKey{ID: 4, UserID: 1, GroupID: 10, Status: "active", ExpiresAt: inFuture(), IPWhitelist: []string{"192.168.0.0/24"}})
	seed("sk-cf-userdis", &model.APIKey{ID: 5, UserID: 2, GroupID: 10, Status: "active", ExpiresAt: inFuture()})
	seed("sk-cf-groupdis", &model.APIKey{ID: 6, UserID: 1, GroupID: 11, Status: "active", ExpiresAt: inFuture()})

	ctx := context.Background()
	cases := []struct {
		name, bearer, ip string
		wantCode         Code
		wantPrincipal    bool
	}{
		{"未知 key", "sk-cf-nope", "1.1.1.1", CodeInvalidAPIKey, false},
		{"成功", "sk-cf-good", "1.1.1.1", "", true},
		{"disabled", "sk-cf-disabled", "1.1.1.1", CodeInvalidAPIKey, false},
		{"expires_at 过期", "sk-cf-expired", "1.1.1.1", CodeInvalidAPIKey, false},
		{"IP 白名单命中", "sk-cf-ip", "192.168.0.9", "", true},
		{"IP 白名单拒绝", "sk-cf-ip", "8.8.8.8", CodeIPNotAllowed, false},
		{"用户不可用", "sk-cf-userdis", "1.1.1.1", CodeInvalidAPIKey, false},
		{"分组不可用", "sk-cf-groupdis", "1.1.1.1", CodeInvalidAPIKey, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Authenticate(ctx, store, testSalt, c.bearer, c.ip)
			if c.wantPrincipal {
				if err != nil || p == nil || p.Key.ID == 0 || p.User == nil || p.Group == nil {
					t.Fatalf("应成功, got p=%+v err=%v", p, err)
				}
				return
			}
			if err == nil {
				t.Fatal("应失败")
			}
			var ae *Error
			if !errors.As(err, &ae) {
				t.Fatalf("应为 *Error, got %T: %v", err, err)
			}
			if ae.Code != c.wantCode {
				t.Fatalf("code = %s, want %s", ae.Code, c.wantCode)
			}
		})
	}
}

func TestAuthenticateStoreErrorPropagates(t *testing.T) {
	store := &fakeLookup{keys: map[string]*model.APIKey{}, users: map[int64]*model.User{}, groups: map[int64]*model.Group{}, userErr: errors.New("db down")}
	store.keys[HashKey(testSalt, "sk-cf-x")] = &model.APIKey{ID: 1, UserID: 1, GroupID: 10, Status: "active", ExpiresAt: inFuture()}
	_, err := Authenticate(context.Background(), store, testSalt, "sk-cf-x", "1.1.1.1")
	if err == nil {
		t.Fatal("DB 错误应向上传播")
	}
	var ae *Error
	if errors.As(err, &ae) {
		t.Fatalf("DB 错误不应包装为鉴权错误: %v", err)
	}
}

func TestPrincipalContextRoundTrip(t *testing.T) {
	p := &Principal{Key: &model.APIKey{ID: 7}}
	ctx := WithPrincipal(context.Background(), p)
	got, ok := PrincipalFrom(ctx)
	if !ok || got.Key.ID != 7 {
		t.Fatalf("ctx 往返失败: %+v %v", got, ok)
	}
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("空 ctx 不应命中")
	}
}
