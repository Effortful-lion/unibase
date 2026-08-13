package mux

import (
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/httpx/jwt"
	"github.com/Effortful-lion/unibase/tools/limiter"
)

// ── 全局中间件（REST + Cmd 共用）───────────────────────────

// LogMiddleware 记录请求日志。
func LogMiddleware(logger *logx.Logger) Middleware {
	if logger == nil {
		logger = logx.Default()
	}
	return func(next Handler) Handler {
		return func(ctx *Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)

			fields := logx.Fields{
				"cmd":       ctx.Cmd(),
				"protocol":  ctx.Protocol(),
				"duration":  duration.Seconds(),
				"sessionId": ctx.Session().ID(),
				"requestId": ctx.RequestID(),
				"status":    httpStatus(ctx),
				"method":    httpMethod(ctx),
				"clientIp":  clientIP(ctx),
			}
			if err != nil {
				fields["error"] = err.Error()
				logger.Error("request error", fields)
			} else {
				logger.Info("request", fields)
			}
			return err
		}
	}
}

// httpStatus 返回 HTTP 状态码，非 HTTP 模式返回 0。
func httpStatus(ctx *Context) int {
	if ctx.Gin() != nil && ctx.Gin().Writer != nil {
		return ctx.Gin().Writer.Status()
	}
	return 0
}

// httpMethod 返回 HTTP 方法，非 HTTP 模式返回空字符串。
func httpMethod(ctx *Context) string {
	if ctx.Gin() != nil && ctx.Gin().Request != nil {
		return ctx.Gin().Request.Method
	}
	return ""
}

// clientIP 返回客户端 IP，非 HTTP 模式返回空字符串。
func clientIP(ctx *Context) string {
	if ctx.Gin() != nil && ctx.Gin().Request != nil {
		return ctx.Gin().Request.RemoteAddr
	}
	return ""
}

// RecoverMiddleware 是全局 Panic 兜底中间件。
// 放在中间件链最外层，防止 Handler panic 导致 goroutine 崩溃。
var RecoverMiddleware Middleware = func(next Handler) Handler {
	return func(ctx *Context) error {
		defer func() {
			if r := recover(); r != nil {
				if err := ctx.ReplyError(http.StatusInternalServerError, "panic: "+toError(r).Error()); err != nil {
					logx.Default().Module("middleware").Error("reply panic failed", logx.Fields{"error": err})
				}
			}
		}()
		return next(ctx)
	}
}

// OnlyWS 限制中间件仅在 WebSocket 模式下执行。
func OnlyWS(next Handler) Handler {
	return func(ctx *Context) error {
		if ctx.Protocol() != ProtocolWS {
			return ctx.ReplyError(http.StatusForbidden, "only websocket allowed")
		}
		return next(ctx)
	}
}

// OnlyHTTP 限制中间件仅在 HTTP 模式下执行。
func OnlyHTTP(next Handler) Handler {
	return func(ctx *Context) error {
		if ctx.Protocol() != ProtocolHTTP {
			return ctx.ReplyError(http.StatusForbidden, "only http allowed")
		}
		return next(ctx)
	}
}

// ── AuthMiddleware ─────────────────────────────────────────

// AuthOption 配置 AuthMiddleware。
type AuthOption func(*authOptions)

type authOptions struct {
	secret []byte
}

// WithJWTSecret 设置 JWT 签名密钥。
func WithJWTSecret(secret []byte) AuthOption {
	return func(o *authOptions) {
		o.secret = secret
	}
}

// AuthMiddleware 返回 JWT 认证中间件。
// 从请求的 Authorization header 提取 token 并验证。
// 必须通过 WithJWTSecret 配置密钥，否则 panic（防止忘记配置导致安全漏洞）。
func AuthMiddleware(opts ...AuthOption) Middleware {
	cfg := &authOptions{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.secret == nil {
		panic("mux.AuthMiddleware: secret is required, use WithJWTSecret() to configure")
	}

	return func(next Handler) Handler {
		return func(ctx *Context) error {
			token, err := extractToken(ctx)
			if err != nil {
				return ctx.ReplyError(http.StatusUnauthorized, "unauthorized")
			}

			parser := jwt.NewHMACParser(cfg.secret)
			claims, err := parser.Parse(token)
			if err != nil {
				return ctx.ReplyError(http.StatusUnauthorized, "invalid token")
			}

			if claims.UserID != "" {
				ctx.Session().SetState("user_id", claims.UserID)
			}

			return next(ctx)
		}
	}
}

// ── RateLimitMiddleware ───────────────────────────────────

// RateLimitMiddleware 返回基于令牌桶的限流中间件。
// rate: 每秒允许的请求数
// burst: 桶容量（允许的突发请求数）
// 超过限流阈值时返回 429。
//
// 注意：这是全局限流器，限制服务整体的 QPS。如需 per-user/per-IP 限流，
// 应在 Handler 内部或自定义中间件中实现。
func RateLimitMiddleware(rate float64, burst int) Middleware {
	l := limiter.NewLimiter(rate, burst)

	return func(next Handler) Handler {
		return func(ctx *Context) error {
			if !l.Allow() {
				return ctx.ReplyError(http.StatusTooManyRequests, "rate limit exceeded")
			}
			return next(ctx)
		}
	}
}

// ── SanitizeMiddleware ────────────────────────────────────

// SanitizeMiddleware 返回响应字段过滤中间件。
// fields: 需要从响应中移除的敏感字段名（如 "password", "token"）。
//
// 工作方式：在 Context 中设置过滤标记，由 Reply 方法在序列化时执行过滤。
// 仅对响应体为 map 的场景生效。
func SanitizeMiddleware(fields ...string) Middleware {
	if len(fields) == 0 {
		return func(next Handler) Handler { return next }
	}

	return func(next Handler) Handler {
		return func(ctx *Context) error {
			// 在 context 中记录需要过滤的字段
			for _, f := range fields {
				ctx.holder["__sanitize_"+f] = true
			}
			return next(ctx)
		}
	}
}

// SanitizeMap 过滤 map 中的敏感字段，返回新 map。
func SanitizeMap(data map[string]any, fields ...string) map[string]any {
	if len(fields) == 0 {
		return data
	}
	fieldSet := make(map[string]bool)
	for _, f := range fields {
		fieldSet[f] = true
	}

	result := make(map[string]any, len(data))
	for k, v := range data {
		if !fieldSet[k] {
			result[k] = v
		}
	}
	return result
}

// ── 内部辅助 ──────────────────────────────────────────────

// headerAccessor 抽象 Header 访问，便于类型断言。
type headerAccessor interface {
	Header(string) string
}

// toError 将 recover 值转换为 error。
func toError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return &wrapError{msg: http.StatusText(http.StatusInternalServerError), raw: v}
}

type wrapError struct {
	msg string
	raw any
}

func (e *wrapError) Error() string {
	return e.msg
}

// extractToken 从 Context 中提取 JWT token。
func extractToken(ctx *Context) (string, error) {
	// HTTP 模式：从 Authorization header 提取
	if raw := ctx.Raw(); raw != nil {
		if gc, ok := raw.(headerAccessor); ok {
			token := gc.Header("Authorization")
			if token != "" {
				return trimBearer(token), nil
			}
		}
	}

	// WebSocket 模式：从 Session meta 中提取（Upgrade 阶段由 Engine 注入）
	if ws := ctx.WebSocket(); ws != nil {
		if meta := ws.Meta(); meta != nil {
			if token, ok := meta["jwt_token"].(string); ok && token != "" {
				return token, nil
			}
		}
	}

	return "", http.ErrNoCookie
}

func trimBearer(s string) string {
	s = trimPrefix(s, "Bearer ")
	s = trimPrefix(s, "bearer ")
	return s
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}
