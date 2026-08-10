package httpx

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yourname/httpx/jwt"
	"github.com/yourname/httpx/response"
)

// Gin 从 gin.Context 创建框架无关的 ResponseWriter。
func Gin(c *gin.Context) response.ResponseWriter {
	return response.FromGin(c)
}

// JWT 快捷创建 HMAC JWT 中间件，一行搞定。
// 等价于 jwt.JWTMiddleware(secret)。
func JWT(secret []byte) gin.HandlerFunc {
	return jwt.JWTMiddleware(secret)
}

// NewHMACParser 快捷创建 HMAC JWT 解析器。
func NewHMACParser(secret []byte) *jwt.Parser {
	return jwt.NewHMACParser(secret)
}

// NewClaims 快捷创建 JWT Claims。
func NewClaims(userID, username string, ttl time.Duration) jwt.Claims {
	return jwt.NewClaims(userID, username, ttl)
}
