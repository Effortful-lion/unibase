package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// testClaims 模拟 JWT claims，实现 UserID() 方法用于中间件提取主体 ID。
type testClaims struct {
	userID   string
	Username string
	Role     string
}

func (c *testClaims) UserID() string { return c.userID }

func setupGin() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c
}

func TestMiddleware_AllowsAuthorized(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddPermission(nil, "editor", "post", "read")
	_ = enforcer.AddRoleForSubject(nil, "user-1", "editor")

	c := setupGin()
	c.Set("jwt_claims", &testClaims{userID: "user-1"})
	c.Request, _ = http.NewRequest("GET", "/post/1", nil)

	mw := Middleware(enforcer)
	mw(c)
	c.Next()

	if c.IsAborted() {
		t.Error("expected request not to be aborted for authorized user")
	}
}

func TestMiddleware_DeniesUnauthorized(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	c := setupGin()
	c.Set("jwt_claims", &testClaims{userID: "user-1"})
	c.Request, _ = http.NewRequest("GET", "/post/1", nil)

	mw := Middleware(enforcer)
	mw(c)

	if !c.IsAborted() {
		t.Error("expected request to be aborted for unauthorized user")
	}
	if c.Writer.Status() != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", c.Writer.Status())
	}
}

func TestMiddleware_SkipPath(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	c := setupGin()
	c.Set("jwt_claims", &testClaims{userID: "user-1"})
	c.Request, _ = http.NewRequest("GET", "/health", nil)

	mw := Middleware(enforcer, WithSkipPath("/health"))
	mw(c)
	c.Next()

	if c.IsAborted() {
		t.Error("expected request not to be aborted for skipped path")
	}
}

func TestMiddleware_MissingClaims(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	c := setupGin()
	// No claims set
	c.Request, _ = http.NewRequest("GET", "/post/1", nil)

	mw := Middleware(enforcer)
	mw(c)

	if !c.IsAborted() {
		t.Error("expected request to be aborted when claims missing")
	}
	if c.Writer.Status() != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", c.Writer.Status())
	}
}

func TestMiddleware_MethodMapping(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	// Only allow GET (read) on post
	_ = enforcer.AddPermission(nil, "viewer", "post", "read")
	_ = enforcer.AddRoleForSubject(nil, "user-1", "viewer")

	tests := []struct {
		method  string
		allowed bool
	}{
		{http.MethodGet, true},
		{http.MethodPost, false},
		{http.MethodPut, false},
		{http.MethodDelete, false},
	}

	for _, tc := range tests {
		c := setupGin()
		c.Set("jwt_claims", &testClaims{userID: "user-1"})
		c.Request, _ = http.NewRequest(tc.method, "/post/1", nil)

		mw := Middleware(enforcer)
		mw(c)

		if tc.allowed && c.IsAborted() {
			t.Errorf("method %s: expected allowed but was aborted", tc.method)
		}
		if !tc.allowed && !c.IsAborted() {
			t.Errorf("method %s: expected denied but was allowed", tc.method)
		}
	}
}

func TestMiddleware_DomainIsolation(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddPermissionInDomain(nil, "editor", "post", "read", "org-a")
	_ = enforcer.AddRoleForSubjectInDomain(nil, "user-1", "editor", "org-a")

	// user-1 in org-a should be allowed
	c := setupGin()
	c.Set("jwt_claims", &testClaims{userID: "user-1"})
	c.Request, _ = http.NewRequest("GET", "/post/1", nil)

	mw := Middleware(enforcer, WithDomainExtractor(func(ctx *gin.Context) string {
		return "org-a"
	}))
	mw(c)

	if c.IsAborted() {
		t.Error("expected user-1 to be allowed in org-a domain")
	}
}

func TestExtractResource(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/users", "api"},
		{"/api/users/123", "api"},
		{"/health", "health"},
		{"/", "*"},
	}

	for _, tc := range tests {
		c := setupGin()
		c.Request, _ = http.NewRequest("GET", tc.path, nil)
		result := extractResource(c)
		if result != tc.expected {
			t.Errorf("extractResource(%q): expected %q, got %q", tc.path, tc.expected, result)
		}
	}
}

// jwtStyleClaims 模拟 httpx/jwt.Claims（带 UserID 字段）。
type jwtStyleClaims struct {
	UserID   string
	Username string
	Role     string
}

func TestMiddleware_JWTStyleClaims(t *testing.T) {
	storage := NewMemoryStorage()
	enforcer, _ := NewEnforcer(storage)

	_ = enforcer.AddPermission(nil, "editor", "post", "read")
	_ = enforcer.AddRoleForSubject(nil, "user-1", "editor")

	c := setupGin()
	c.Set("jwt_claims", &jwtStyleClaims{UserID: "user-1"})
	c.Request, _ = http.NewRequest("GET", "/post/1", nil)

	mw := Middleware(enforcer)
	mw(c)

	if c.IsAborted() {
		t.Error("expected request not to be aborted for JWT-style claims")
	}
}

func TestExtractSubjectID_WithStructField(t *testing.T) {
	// 测试带 UserID 字段的结构体
	claims := &jwtStyleClaims{UserID: "user-123"}
	id, err := extractSubjectID(claims)
	if err != nil {
		t.Fatalf("extractSubjectID: %v", err)
	}
	if id != "user-123" {
		t.Errorf("expected user-123, got %s", id)
	}
}

func TestExtractSubjectID_WithIDField(t *testing.T) {
	// 测试带 ID 字段的结构体
	type idClaims struct {
		ID string
	}
	claims := &idClaims{ID: "svc-456"}
	id, err := extractSubjectID(claims)
	if err != nil {
		t.Fatalf("extractSubjectID: %v", err)
	}
	if id != "svc-456" {
		t.Errorf("expected svc-456, got %s", id)
	}
}

func TestExtractSubjectID_NilPointer(t *testing.T) {
	// nil 指针不应 panic
	var nilClaims *jwtStyleClaims
	_, err := extractSubjectID(nilClaims)
	if err == nil {
		t.Error("expected error for nil pointer, got nil")
	}
}
