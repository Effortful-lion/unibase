package mux

import (
	"encoding/json"
	"net/http"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/msg"
	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// WSSession 是内部 WebSocket Session 封装，用于直接访问底层连接。
type WSSession = msg.WSSession

// CmdMessage 是 WebSocket 传输的统一消息格式。
// 用于 Hub.Broadcast / BroadcastToRoom / BroadcastToUser 等广播 API。
type CmdMessage = websocketx.CmdMessage

// NewCmdMessage 构造一条 CmdMessage，body 会被 JSON 序列化为 Body 字段。
// body 为 nil 时 Body 留空。
func NewCmdMessage(cmd string, body any) (*CmdMessage, error) {
	msg := &CmdMessage{
		Cmd:  cmd,
		Meta: map[string]interface{}{},
	}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		msg.Body = data
	}
	return msg, nil
}

// MessageStore 消息持久化接口，用于 IM 离线消息存储。
type MessageStore = websocketx.MessageStore

// StoredMessage 持久化消息结构。
type StoredMessage = websocketx.StoredMessage

// NewNoOpMessageStore 创建空实现的消息存储。
func NewNoOpMessageStore() MessageStore {
	return websocketx.NewNoOpMessageStore()
}

// NewRedisMessageStore 创建 Redis 实现的消息存储。
func NewRedisMessageStore(rdb *redis.Client) MessageStore {
	return websocketx.NewRedisMessageStore(rdb)
}

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

// PerKeyRateLimit 返回按 key 隔离的限流中间件（per-user/per-IP）。
// 默认 key 策略：已认证用户按 userID 限流，未认证按 client IP 限流。
func PerKeyRateLimit(rate float64, burst int, opts ...PerKeyRateLimitOption) Middleware {
	return PerKeyRateLimitMiddleware(rate, burst, opts...)
}

// Recover 返回 Panic 恢复中间件。
var Recover = RecoverMiddleware

// Sanitize 返回响应字段过滤中间件。
func Sanitize(fields ...string) Middleware {
	return SanitizeMiddleware(fields...)
}

// SecureHeaders 返回安全响应头中间件（X-Content-Type-Options、X-Frame-Options 等）。
var SecureHeaders = SecureHeadersMiddleware

// LogRoutes 输出所有已注册路由信息（便捷函数，委托给 Engine.LogRoutes）。
func LogRoutes(engine *Engine, logger *logx.Logger) {
	if engine != nil {
		engine.LogRoutes(logger)
	}
}
