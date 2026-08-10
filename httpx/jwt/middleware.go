package jwt

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// JWTOption JWT 中间件配置。
type JWTOption func(*jwtConfig)

type jwtConfig struct {
	parser      *Parser
	tokenLookup []string // "header:Authorization", "query:token"
}

func defaultJWTConfig() *jwtConfig {
	return &jwtConfig{
		tokenLookup: []string{"header:Authorization"},
	}
}

// WithParser 注入自定义 JWT 解析器。
func WithParser(parser *Parser) JWTOption {
	return func(c *jwtConfig) {
		c.parser = parser
	}
}

// WithTokenLookup 指定 token 提取位置。
func WithTokenLookup(lookup ...string) JWTOption {
	return func(c *jwtConfig) {
		c.tokenLookup = lookup
	}
}

// Middleware 返回 JWT 认证中间件（通用版，需通过 WithParser 注入解析器）。
func Middleware(opts ...JWTOption) gin.HandlerFunc {
	cfg := defaultJWTConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.parser == nil {
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "jwt middleware not configured: parser is required, use WithParser()",
			})
		}
	}

	return authMiddleware(cfg)
}

// JWTMiddleware 快捷创建 HMAC JWT 中间件，一行搞定。
// 等价于 Middleware(WithParser(NewHMACParser(secret)))，用于最常见的对称密钥场景。
func JWTMiddleware(secret []byte, opts ...JWTOption) gin.HandlerFunc {
	allOpts := append([]JWTOption{WithParser(NewHMACParser(secret))}, opts...)
	return Middleware(allOpts...)
}

// ClaimsFromContext 从 Gin context 中提取 JWT Claims。
// 在中间件之后的 handler 中调用。
func ClaimsFromContext(c *gin.Context) (*Claims, error) {
	v, exists := c.Get("jwt_claims")
	if !exists {
		return nil, errors.New("jwt claims not found in context")
	}
	claims, ok := v.(*Claims)
	if !ok {
		return nil, errors.New("invalid jwt claims type in context")
	}
	return claims, nil
}

func authMiddleware(cfg *jwtConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := extractToken(c.Request, cfg.tokenLookup)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		claims, err := cfg.parser.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token: " + err.Error()})
			return
		}

		c.Set("jwt_claims", claims)
		c.Next()
	}
}

func extractToken(req *http.Request, lookups []string) (string, error) {
	for _, lookup := range lookups {
		parts := strings.SplitN(lookup, ":", 2)
		if len(parts) != 2 {
			continue
		}
		source := parts[0]
		key := parts[1]

		switch source {
		case "header":
			token := req.Header.Get(key)
			if token == "" {
				continue
			}
			token = strings.TrimPrefix(token, "Bearer ")
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			return token, nil

		case "query":
			token := req.URL.Query().Get(key)
			if token != "" {
				return token, nil
			}

		case "param":
			token := req.PathValue(key)
			if token != "" {
				return token, nil
			}
		}
	}
	return "", errors.New("token not found")
}
