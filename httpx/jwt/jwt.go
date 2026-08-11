package jwt

import (
	"net/http"
	"strings"
	"time"

	"github.com/Effortful-lion/unibase/tools/auth/jwt"
	"github.com/gin-gonic/gin"
	gjwt "github.com/golang-jwt/jwt/v5"
)

// Claims 是 auth.Claims 的快捷类型别名，携带 JSON tags，适用于 HTTP 响应序列化。
type Claims = jwt.Claims

// NewClaims 创建 Claims，自动填充 IssuedAt 和 ExpiresAt。
func NewClaims(userID, username string, ttl time.Duration) Claims {
	now := time.Now()
	return Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: gjwt.RegisteredClaims{
			IssuedAt:  gjwt.NewNumericDate(now),
			ExpiresAt: gjwt.NewNumericDate(now.Add(ttl)),
		},
	}
}

// Parser JWT 处理器，基于 tools/auth/jwt.Manager 实现。
type Parser struct {
	mgr jwt.Manager
}

// NewHMACParser 创建 HMAC 签名算法的解析器（最常用场景）。
func NewHMACParser(secret []byte) *Parser {
	return &Parser{mgr: jwt.NewManager(string(secret))}
}

// Parse 解析并验证 JWT token，返回 Claims。
func (p *Parser) Parse(tokenString string) (*Claims, error) {
	return p.mgr.Parse(tokenString)
}

// Sign 用指定密钥签名 Claims，返回 JWT token 字符串。
func (p *Parser) Sign(claims Claims, key any) (string, error) {
	secret, ok := key.([]byte)
	if !ok {
		return "", errInvalidKey
	}
	return jwt.NewManager(string(secret)).Generate(claims.UserID, claims.Username, claims.Role)
}

// Verify 验证 token 是否有效（签名正确、未过期）。
func (p *Parser) Verify(tokenString string) bool {
	_, err := p.mgr.Parse(tokenString)
	return err == nil
}

// JWTMiddleware 快捷创建 HMAC JWT 中间件，一行搞定。
// 在中间件之后的 handler 中可通过 ClaimsFromContext 提取 claims。
func JWTMiddleware(secret []byte) gin.HandlerFunc {
	parser := NewHMACParser(secret)
	return func(c *gin.Context) {
		token, err := extractToken(c.Request)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": err.Error()})
			return
		}

		claims, err := parser.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid token: " + err.Error()})
			return
		}

		c.Set("jwt_claims", claims)
		c.Next()
	}
}

// ClaimsFromContext 从 Gin context 中提取 JWT Claims。
// 在 JWTMiddleware 之后的 handler 中调用。
func ClaimsFromContext(c *gin.Context) (*Claims, error) {
	v, exists := c.Get("jwt_claims")
	if !exists {
		return nil, errClaimsNotFound
	}
	claims, ok := v.(*Claims)
	if !ok {
		return nil, errInvalidClaimsType
	}
	return claims, nil
}

// --- 内部函数 ---

func extractToken(req *http.Request) (string, error) {
	token := req.Header.Get("Authorization")
	if token != "" {
		token = trimBearer(token)
		if token != "" {
			return token, nil
		}
	}

	token = req.URL.Query().Get("token")
	if token != "" {
		return token, nil
	}

	return "", errTokenNotFound
}

func trimBearer(s string) string {
	s = strings.TrimPrefix(s, "Bearer ")
	s = strings.TrimPrefix(s, "bearer ")
	return strings.TrimSpace(s)
}

// --- 内部错误 ---

var (
	errInvalidKey        = err("jwt: signing key must be []byte")
	errTokenNotFound     = err("jwt: token not found")
	errClaimsNotFound    = err("jwt: claims not found in context")
	errInvalidClaimsType = err("jwt: invalid claims type in context")
)

type err string

func (e err) Error() string { return string(e) }
