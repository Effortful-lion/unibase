package middleware

import (
	"github.com/Effortful-lion/httpx/jwt"
	"github.com/gin-gonic/gin"
)

// JWT 返回 JWT 认证中间件，包装 jwt.JWTMiddleware。
func JWT(secret []byte) gin.HandlerFunc {
	return jwt.JWTMiddleware(secret)
}
