package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT 自定义声明。
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

// NewClaims 创建 Claims，自动填充 IssuedAt 和 ExpiresAt。
func NewClaims(userID, username string, ttl time.Duration) Claims {
	now := time.Now()
	return Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
}
