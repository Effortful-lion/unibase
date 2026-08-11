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
	token, err := mgr.Generate("u-100", "alice", "user")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	claims, err := mgr.Parse(token)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if claims.UserID != "u-100" {
		t.Errorf("UserID = %q, want u-100", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Errorf("Username = %q, want alice", claims.Username)
	}
	if claims.Role != "user" {
		t.Errorf("Role = %q, want user", claims.Role)
	}
	if claims.IssuedAt == nil {
		t.Error("IssuedAt is nil")
	}
	if claims.ExpiresAt == nil {
		t.Error("ExpiresAt is nil")
	}
}

func TestManager_Refresh(t *testing.T) {
	mgr := NewManager(randomSecret(t))

	token, err := mgr.Generate("u-200", "bob", "admin")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
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

func generateWithSecret(userID, username, role, secret string) string {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(DefaultExpiry)),
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

	token, err := mgr.Generate("u-300", "charlie", "")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	_, err = mgr.Parse(token)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestClaims_RoleField(t *testing.T) {
	// 验证 Role 字段可被正确序列化和反序列化
	mgr := NewManager(randomSecret(t))

	for _, role := range []string{"", "user", "admin", "superadmin"} {
		token, err := mgr.Generate("u", "u", role)
		if err != nil {
			t.Fatalf("Generate with role %q failed: %v", role, err)
		}
		claims, err := mgr.Parse(token)
		if err != nil {
			t.Fatalf("Parse with role %q failed: %v", role, err)
		}
		if claims.Role != role {
			t.Errorf("Role = %q, want %q", claims.Role, role)
		}
	}
}
