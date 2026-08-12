package jwt

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func randomSecret(t *testing.T) string {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return string(secret)
}

func TestManager_GenerateAndParse(t *testing.T) {
	mgr := NewManager(randomSecret(t))
	claims := Claims{
		UserID:   "u-100",
		Username: "alice",
		Role:     "user",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(DefaultExpiry)),
		},
	}

	token, err := mgr.GenerateWithClaims(claims)
	if err != nil {
		t.Fatalf("GenerateWithClaims failed: %v", err)
	}

	parsed, err := mgr.Parse(token)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if parsed.UserID != "u-100" {
		t.Errorf("UserID = %q, want u-100", parsed.UserID)
	}
	if parsed.Username != "alice" {
		t.Errorf("Username = %q, want alice", parsed.Username)
	}
	if parsed.Role != "user" {
		t.Errorf("Role = %q, want user", parsed.Role)
	}
	if parsed.IssuedAt == nil {
		t.Error("IssuedAt is nil")
	}
	if parsed.ExpiresAt == nil {
		t.Error("ExpiresAt is nil")
	}
}

func TestManager_Refresh(t *testing.T) {
	mgr := NewManager(randomSecret(t))

	claims := Claims{
		UserID:   "u-200",
		Username: "bob",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(DefaultExpiry)),
		},
	}

	token, err := mgr.GenerateWithClaims(claims)
	if err != nil {
		t.Fatalf("GenerateWithClaims failed: %v", err)
	}

	// 等待 1s 后 refresh，确保 iat 更新
	time.Sleep(1 * time.Second)

	newToken, err := mgr.Refresh(token)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	oldClaims, _ := mgr.Parse(token)
	newClaims, err := mgr.Parse(newToken)
	if err != nil {
		t.Fatalf("Parse refreshed token failed: %v", err)
	}

	if newClaims.UserID != oldClaims.UserID {
		t.Errorf("UserID changed after refresh")
	}
	if !newClaims.IssuedAt.Time.After(oldClaims.IssuedAt.Time) {
		t.Errorf("new iat should be after old iat")
	}
}

func TestManager_ParseInvalidToken(t *testing.T) {
	mgr := NewManager(randomSecret(t))

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"malformed", "not.a.valid.token"},
		{"wrong_secret", generateWithSecret("u", "u", "", randomSecret(t))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mgr.Parse(tc.token)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestManager_MapClaims(t *testing.T) {
	mgr := NewManager(randomSecret(t))

	data := map[string]any{
		"user_id": "u-400",
		"dept":    "engineering",
		"perms":   []string{"read", "write"},
	}
	mc := jwt.MapClaims(data)
	mc["exp"] = time.Now().Add(DefaultExpiry).Unix()
	mc["iat"] = time.Now().Unix()

	token, err := mgr.GenerateWithClaims(mc)
	if err != nil {
		t.Fatalf("Generate with MapClaims failed: %v", err)
	}

	parsed, err := mgr.Parse(token)
	if err != nil {
		t.Fatalf("Parse MapClaims token failed: %v", err)
	}
	if parsed.UserID != "u-400" {
		t.Errorf("UserID from MapClaims = %q, want u-400", parsed.UserID)
	}
}

func generateWithSecret(userID, username, role, secret string) string {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(DefaultExpiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(secret))
	return signed
}

func TestManager_ExpiredToken(t *testing.T) {
	mgr := NewManagerWithConfig(ManagerConfig{
		Secret: []byte(randomSecret(t)),
		Expiry: -time.Hour,
	})

	claims := Claims{
		UserID:   "u-300",
		Username: "charlie",
		Role:     "",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}

	token, err := mgr.GenerateWithClaims(claims)
	if err != nil {
		t.Fatalf("GenerateWithClaims failed: %v", err)
	}

	_, err = mgr.Parse(token)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestClaims_ExtraField(t *testing.T) {
	mgr := NewManager(randomSecret(t))

	claims := NewClaimsWithRole("u-500", "dave", "editor")
	claims.SetExtra("dept", "engineering")
	claims.SetExtra("perms", []string{"read", "write"})

	token, err := mgr.GenerateWithClaims(*claims)
	if err != nil {
		t.Fatalf("GenerateWithClaims failed: %v", err)
	}

	parsed, err := mgr.Parse(token)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	dept, ok := parsed.GetExtra("dept")
	if !ok || dept != "engineering" {
		t.Errorf(`GetExtra("dept") = %v, ok=%v, want "engineering", true`, dept, ok)
	}

	_, ok = parsed.GetExtra("perms")
	if !ok {
		t.Error(`GetExtra("perms") not found`)
	}

	_, ok = parsed.GetExtra("nonexistent")
	if ok {
		t.Error("expected false for nonexistent key")
	}
}

func TestClaims_RefreshPreservesExtra(t *testing.T) {
	mgr := NewManager(randomSecret(t))

	claims := NewClaimsWithRole("u-600", "eve", "admin")
	claims.SetExtra("org", "acme")

	token, err := mgr.GenerateWithClaims(*claims)
	if err != nil {
		t.Fatalf("GenerateWithClaims failed: %v", err)
	}

	newToken, err := mgr.Refresh(token)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	parsed, err := mgr.Parse(newToken)
	if err != nil {
		t.Fatalf("Parse refreshed token failed: %v", err)
	}

	org, ok := parsed.GetExtra("org")
	if !ok || org != "acme" {
		t.Errorf("Extra lost after refresh: org=%v, ok=%v", org, ok)
	}
}

func TestNewClaims(t *testing.T) {
	c := NewClaims("uid", "name")
	if c.UserID != "uid" || c.Username != "name" {
		t.Errorf("NewClaims = %+v", c)
	}
	if c.Extra != nil {
		t.Error("Extra should be nil for NewClaims")
	}
}

func TestNewClaimsWithRole(t *testing.T) {
	c := NewClaimsWithRole("uid", "name", "admin")
	if c.UserID != "uid" || c.Username != "name" || c.Role != "admin" {
		t.Errorf("NewClaimsWithRole = %+v", c)
	}
}

func TestClaims_RoleField(t *testing.T) {
	// 验证 Role 字段可被正确序列化和反序列化
	mgr := NewManager(randomSecret(t))

	for _, role := range []string{"", "user", "admin", "superadmin"} {
		claims := Claims{
			UserID:   "u",
			Username: "u",
			Role:     role,
			RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(DefaultExpiry)),
			},
		}
		token, err := mgr.GenerateWithClaims(claims)
		if err != nil {
			t.Fatalf("Generate with role %q failed: %v", role, err)
		}
		parsed, err := mgr.Parse(token)
		if err != nil {
			t.Fatalf("Parse with role %q failed: %v", role, err)
		}
		if parsed.Role != role {
			t.Errorf("Role = %q, want %q", parsed.Role, role)
		}
	}
}
