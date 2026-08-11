package auth

import (
	"crypto/rand"
	"testing"

	"github.com/Effortful-lion/unibase/tools/auth/basic"
)

func TestAuth_JWTAndBasic(t *testing.T) {
	// 生成随机密钥
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}

	// 创建 bcrypt 哈希用于测试
	hash, err := basic.HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash password failed: %v", err)
	}

	hashes := map[string]string{"admin": hash}
	a := NewDefaultAuth(string(secret), basic.NewBcryptAuthenticator(hashes))

	// --- JWT 路径 ---
	token, err := a.GenerateToken("u-001", "admin", "admin")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := a.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != "u-001" || claims.Username != "admin" || claims.Role != "admin" {
		t.Fatalf("claims mismatch: %+v", claims)
	}

	// Refresh
	refreshed, err := a.ValidateToken(token)
	_ = refreshed

	// --- Basic 路径 ---
	if !a.Authenticate("admin", "secret123") {
		t.Error("expected authenticate success")
	}
	if a.Authenticate("admin", "wrong") {
		t.Error("expected authenticate failure")
	}
	if a.Authenticate("nonexistent", "anything") {
		t.Error("expected authenticate failure for unknown user")
	}
}
