package jwt

import (
	"errors"
	"net/http"

	"github.com/Effortful-lion/unibase/mux/internal/httpx/code"
	"github.com/Effortful-lion/unibase/mux/internal/httpx/response"
	"github.com/gin-gonic/gin"
)

// --- 内部错误 ---

var (
	errClaimsNotFound = errors.New("jwt: claims not found in context")
)

// JWTMiddleware 快捷创建 HMAC JWT 中间件，一行搞定。
// 在中间件之后的 handler 中可通过 ClaimsFromContext 提取 claims。
func JWTMiddleware(secret []byte) gin.HandlerFunc {
	parser := NewHMACParser(secret)
	return func(c *gin.Context) {
		token, err := extractToken(c.Request)
		if err != nil {
			response.ResponseFail(c, http.StatusUnauthorized, code.Unauthorized, err.Error())
			return
		}

		claims, err := parser.Parse(token)
		if err != nil {
			response.ResponseFail(c, http.StatusUnauthorized, code.Unauthorized, "invalid token: "+err.Error())
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
