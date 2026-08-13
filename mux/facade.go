package mux

import (
	"net/http"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/msg"
	"github.com/gin-gonic/gin"
)

// WSSession 是内部 WebSocket Session 封装，用于直接访问底层连接。
type WSSession = msg.WSSession

// HealthResponse 是健康检查响应结构。
type HealthResponse struct {
	Status  string `json:"status"`
	WSConns int    `json:"ws_conns"`
}

// HealthHandler 返回一个健康检查 HTTP Handler。
// 注册方式：engine.HTTP().GET("/health", mux.HealthHandler)
func HealthHandler(engine *Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var wsConns int
		if engine.wsHub != nil {
			wsConns = engine.wsHub.Count()
		}
		c.JSON(http.StatusOK, HealthResponse{
			Status:  "ok",
			WSConns: wsConns,
		})
	}
}

// ── 内置中间件（便捷函数）─────────────────────────────────

// Log 返回请求日志中间件。
func Log(logger *logx.Logger) Middleware {
	return LogMiddleware(logger)
}

// Auth 返回 JWT 认证中间件。
// 必须通过 WithJWTSecret 配置密钥，否则 panic（防止忘记配置导致安全漏洞）。
func Auth(opts ...AuthOption) Middleware {
	return AuthMiddleware(opts...)
}

// RateLimit 返回限流中间件。
func RateLimit(rate float64, burst int) Middleware {
	return RateLimitMiddleware(rate, burst)
}

// Recover 返回 Panic 恢复中间件。
var Recover = RecoverMiddleware

// Sanitize 返回响应字段过滤中间件。
func Sanitize(fields ...string) Middleware {
	return SanitizeMiddleware(fields...)
}

// LogRoutes 输出所有已注册路由信息（便捷函数，委托给 Engine.LogRoutes）。
func LogRoutes(engine *Engine, logger *logx.Logger) {
	if engine != nil {
		engine.LogRoutes(logger)
	}
}
