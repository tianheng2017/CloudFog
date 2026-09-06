package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"cloudfog/internal/model"
)

// stubLookup auth.Lookup 的最小测试替身。
type stubLookup struct {
	key  *model.APIKey
	user *model.User
	grp  *model.Group
}

func (s *stubLookup) APIKeyByHash(_ context.Context, _ string) (*model.APIKey, error) {
	if s == nil || s.key == nil {
		return nil, nil
	}
	return s.key, nil
}
func (s *stubLookup) UserByID(_ context.Context, _ int64) (*model.User, error) {
	if s == nil {
		return nil, nil
	}
	return s.user, nil
}
func (s *stubLookup) GroupByID(_ context.Context, _ int64) (*model.Group, error) {
	if s == nil {
		return nil, nil
	}
	return s.grp, nil
}

func activeUser() *model.User   { return &model.User{ID: 1, Status: "active"} }
func activeGroup() *model.Group { return &model.Group{ID: 10, Status: "active"} }

func TestAPIAuthMiddleware(t *testing.T) {
	const salt = "test-salt-0123456789abcdef"
	good := &stubLookup{
		key:  &model.APIKey{ID: 9, UserID: 1, GroupID: 10, Status: "active"},
		user: activeUser(),
		grp:  activeGroup(),
	}
	unknown := &stubLookup{user: activeUser(), grp: activeGroup()}

	cases := []struct {
		name       string
		store      *stubLookup
		header     string
		wantStatus int
	}{
		{"缺 Authorization → 401", &stubLookup{}, "", http.StatusUnauthorized},
		{"未知 key → 401", unknown, "Bearer sk-cf-nope", http.StatusUnauthorized},
		{"成功 → 200", good, "Bearer sk-cf-good", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newAuthRouter(c.store, salt)
			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/test", nil)
			req.Header.Set("Authorization", c.header)
			r.ServeHTTP(w, req)
			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, c.wantStatus, w.Body.String())
			}
		})
	}
}

func TestAPIAuthMiddlewareSetsPrincipal(t *testing.T) {
	good := &stubLookup{
		key:  &model.APIKey{ID: 42, UserID: 1, GroupID: 10, Status: "active"},
		user: activeUser(),
		grp:  activeGroup(),
	}
	r := newAuthRouter(good, "test-salt-0123456789abcdef")
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer sk-cf-real")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	// handler 回显 principal 字段，证明已注入上下文
	if body := w.Body.String(); !strings.Contains(body, `"key_id":42`) || !strings.Contains(body, `"group_id":10`) {
		t.Fatalf("principal 未正确注入: %s", body)
	}
}

func TestAPIAuthMiddlewareIPDenied(t *testing.T) {
	// httptest RemoteAddr 默认 192.0.2.1；白名单 10.0.0.0/8 必拒绝 → 403
	restricted := &stubLookup{
		key:  &model.APIKey{ID: 9, UserID: 1, GroupID: 10, Status: "active", IPWhitelist: []string{"10.0.0.0/8"}},
		user: activeUser(),
		grp:  activeGroup(),
	}
	r := newAuthRouter(restricted, "test-salt-0123456789abcdef")
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer sk-cf-restricted")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ip_not_allowed"`) {
		t.Fatalf("错误体应含 ip_not_allowed: %s", w.Body.String())
	}
}

func newAuthRouter(store *stubLookup, salt string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/test", APIKeyAuth(store, salt), func(c *gin.Context) {
		p, ok := PrincipalOf(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"err": "no principal"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"key_id": p.Key.ID, "user_id": p.User.ID, "group_id": p.Group.ID})
	})
	return r
}
