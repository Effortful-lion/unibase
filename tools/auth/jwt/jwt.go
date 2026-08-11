// Package jwt 提供基于 golang-jwt/jwt/v5 的 JWT 生成、解析与刷新能力。
//
// Claims 结构体中预留 Role 字段，后续可无缝对接基于角色的权限认证框架。
package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 扩展了标准 RegisteredClaims，携带业务身份信息。
//
// Role 字段为后续 RBAC 权限控制预留，生成 token 时传入，解析后可用于
// Casbin 等权限框架的 Enforce(sub=role, obj=path, act=method) 调用。
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

// Manager 定义了 JWT 的完整生命周期操作。
type Manager interface {
	// Generate 生成 JWT token，包含 userID、username 与 role。
	// role 可为空字符串，为后续权限控制预留字段。
	Generate(userID, username, role string) (string, error)

	// Parse 解析并校验 JWT token，返回 claims。
	// 若 token 过期或签名无效则返回错误。
	Parse(token string) (*Claims, error)

	// Refresh 使用已有 token 的 claims 生成新 token，续期有效期。
	// 适用于 token 续期场景，避免用户重新登录。
	Refresh(token string) (string, error)
}

// manager 是 Manager 的默认实现。
type manager struct {
	secret []byte
	expiry time.Duration // token 有效期
}

// ManagerConfig 用于配置 Manager。
type ManagerConfig struct {
	Secret []byte        // JWT 签名密钥，建议 32 字节以上
	Expiry time.Duration // token 有效期，0 表示使用默认值 24h
}

// DefaultExpiry 默认 token 有效期。
const DefaultExpiry = 24 * time.Hour

// NewManager 创建使用默认过期时间（24h）的 Manager。
func NewManager(secret string) Manager {
	return NewManagerWithConfig(ManagerConfig{
		Secret: []byte(secret),
	})
}

// NewManagerWithConfig 使用自定义配置创建 Manager。
func NewManagerWithConfig(cfg ManagerConfig) Manager {
	expiry := cfg.Expiry
	if expiry == 0 {
		expiry = DefaultExpiry
	}
	return &manager{secret: cfg.Secret, expiry: expiry}
}

func (m *manager) Generate(userID, username, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiry)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *manager) Parse(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrUnexpectedSigningMethod
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (m *manager) Refresh(tokenString string) (string, error) {
	claims, err := m.Parse(tokenString)
	if err != nil {
		return "", err
	}
	// 以当前时间重新签发，保留原有业务 claims
	return m.Generate(claims.UserID, claims.Username, claims.Role)
}

// ErrUnexpectedSigningMethod 表示 token 使用的签名算法与预期不符。
var ErrUnexpectedSigningMethod = errors.New("jwt: unexpected signing method")

// ErrInvalidToken 表示 token 无效或已过期。
var ErrInvalidToken = errors.New("jwt: invalid token")
