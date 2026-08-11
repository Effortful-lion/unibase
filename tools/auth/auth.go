// Package auth 提供统一的认证入口，聚合 JWT（对外）与 Basic Auth（对内）两种方式。
//
// JWT 路径用于对外 API 的身份校验，Claims 中预留 Role 字段，后续可无缝对接
// 基于角色的权限认证框架（如 Casbin）。
//
// Basic Auth 路径用于对内服务的快速身份校验，密码存储使用 bcrypt 哈希。
package auth

import (
	"net/http"

	"github.com/Effortful-lion/unibase/tools/auth/basic"
	"github.com/Effortful-lion/unibase/tools/auth/jwt"
)

// Auth 统一认证接口，上层无需关心内部采用哪种认证方式。
type Auth interface {
	// --- JWT 路径（对外身份校验） ---

	// GenerateToken 为用户生成 JWT token。
	// role 为预留字段，后续可用于 RBAC 权限控制，当前可为空字符串。
	GenerateToken(userID, username, role string) (string, error)

	// ValidateToken 解析并校验 JWT token，返回 claims 或错误。
	ValidateToken(token string) (*jwt.Claims, error)

	// --- Basic 路径（对内服务快速校验） ---

	// Authenticate 使用 Basic Auth 校验用户名和密码。
	// 适用于服务间内部调用的快速身份校验。
	Authenticate(username, password string) bool

	// AuthenticateRequest 从 HTTP 请求中提取 Basic Auth 头并校验。
	AuthenticateRequest(r *http.Request) (string, bool)
}

// DefaultAuth 是 Auth 的默认实现。
type DefaultAuth struct {
	jwtManager    jwt.Manager
	authenticator basic.Authenticator
}

// NewDefaultAuth 创建默认认证实例。
//
// jwtSecret 为 JWT 签名密钥，建议使用 32 字节以上的随机串。
// authenticator 为 Basic Auth 校验器，由调用方根据存储的 bcrypt 哈希构建。
func NewDefaultAuth(jwtSecret string, authenticator basic.Authenticator) *DefaultAuth {
	return &DefaultAuth{
		jwtManager:    jwt.NewManager(jwtSecret),
		authenticator: authenticator,
	}
}

func (a *DefaultAuth) GenerateToken(userID, username, role string) (string, error) {
	return a.jwtManager.Generate(userID, username, role)
}

func (a *DefaultAuth) ValidateToken(token string) (*jwt.Claims, error) {
	return a.jwtManager.Parse(token)
}

func (a *DefaultAuth) Authenticate(username, password string) bool {
	return a.authenticator.Authenticate(username, password)
}

func (a *DefaultAuth) AuthenticateRequest(r *http.Request) (string, bool) {
	username, password, ok := basic.Decode(r.Header.Get("Authorization"))
	if !ok {
		return "", false
	}
	if !a.authenticator.Authenticate(username, password) {
		return "", false
	}
	return username, true
}

// Claims 是 auth.Claims 的快捷方式，调用方可直接使用本类型避免导入 jwt 包。
type Claims = jwt.Claims

// Manager 是 auth.jwt.Manager 的快捷方式。
type Manager = jwt.Manager

// Authenticator 是 auth.basic.Authenticator 的快捷方式。
type Authenticator = basic.Authenticator
