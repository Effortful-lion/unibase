package mux

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/httpx/jwt"
	"github.com/Effortful-lion/unibase/mux/internal/msg"
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
					logx.Default().Module("mux").Error("reply panic failed", logx.Fields{"error": err})
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
// 从请求的 Authorization header（HTTP）或 Session Meta（WS）提取 token 并验证。
// 验证通过后通过 Session.WithUserID / Session.SetUserID 设置 userID。
// Handler 中统一通过 ctx.Session().UserID() 获取。
//
// 注册方式：
//
//	p.Use(AuthMiddleware(WithJWTSecret(secret)))
//
// 必须通过 WithJWTSecret 配置密钥，否则 panic（防止忘记配置导致安全漏洞）。
func AuthMiddleware(opts ...AuthOption) Middleware {
	cfg := &authOptions{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.secret == nil {
		panic("mux.AuthMiddleware: secret is required, use WithJWTSecret() to configure")
	}

	parser := jwt.NewHMACParser(cfg.secret)

	return func(next Handler) Handler {
		return func(ctx *Context) error {
			token, err := extractToken(ctx)
			if err != nil {
				return ctx.ReplyError(http.StatusUnauthorized, "unauthorized")
			}

			claims, err := parser.Parse(token)
			if err != nil {
				return ctx.ReplyError(http.StatusUnauthorized, "invalid token")
			}

			if claims.UserID != "" {
				setUserID(ctx.Session(), claims.UserID)
			}

			return next(ctx)
		}
	}
}

// setUserID 将 userID 设置到 Session，支持 HTTP 和 WS 两种实现。
func setUserID(s Session, userID string) {
	switch v := s.(type) {
	case *msg.HTTPRequestSession:
		v.WithUserID(userID)
	case *msg.WSSession:
		v.Raw().SetUserID(userID)
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

// ── PerKeyRateLimitMiddleware ─────────────────────────────

// rateLimitPool 是按 key 隔离的令牌桶限流池，自动清理长时间未访问的 limiter。
type rateLimitPool struct {
	mu       sync.Mutex
	limiters map[string]*rateLimitEntry
	rate     float64
	burst    int
	stopCh   chan struct{}
	doneCh   chan struct{}
}

type rateLimitEntry struct {
	limiter    *limiter.Limiter
	lastAccess time.Time
}

func newRateLimitPool(rate float64, burst int) *rateLimitPool {
	pool := &rateLimitPool{
		limiters: make(map[string]*rateLimitEntry),
		rate:     rate,
		burst:    burst,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go pool.cleanupLoop()
	return pool
}

func (pool *rateLimitPool) cleanupLoop() {
	defer close(pool.doneCh)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-pool.stopCh:
			return
		case <-ticker.C:
			pool.mu.Lock()
			threshold := time.Now().Add(-30 * time.Minute)
			for key, entry := range pool.limiters {
				if entry.lastAccess.Before(threshold) {
					delete(pool.limiters, key)
				}
			}
			pool.mu.Unlock()
		}
	}
}

// Stop 停止自动清理 goroutine。
func (pool *rateLimitPool) Stop() {
	select {
	case <-pool.stopCh:
	default:
		close(pool.stopCh)
	}
	<-pool.doneCh
}

// allow 按 key 检查是否允许通过，不存在则创建。
func (pool *rateLimitPool) allow(key string) bool {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if entry, exists := pool.limiters[key]; exists {
		entry.lastAccess = time.Now()
		return entry.limiter.Allow()
	}

	lim := limiter.NewLimiter(pool.rate, pool.burst)
	pool.limiters[key] = &rateLimitEntry{limiter: lim, lastAccess: time.Now()}
	return lim.Allow()
}

// PerKeyRateLimitOption 配置 per-key 限流中间件。
type PerKeyRateLimitOption func(*perKeyRateLimitConfig)

type perKeyRateLimitConfig struct {
	keyFunc func(*Context) string
}

// WithKeyFunc 自定义 key 提取函数。
// 默认策略：优先使用 Session.UserID()，为空时回退到 client IP（HTTP）或 session ID（WS）。
func WithKeyFunc(fn func(*Context) string) PerKeyRateLimitOption {
	return func(c *perKeyRateLimitConfig) {
		c.keyFunc = fn
	}
}

// defaultRateLimitKey 默认 key 提取：已认证用 userID，否则用 client IP 或 session ID。
func defaultRateLimitKey(ctx *Context) string {
	if userID := ctx.Session().UserID(); userID != "" {
		return "user:" + userID
	}
	if gc := ctx.Gin(); gc != nil && gc.Request != nil {
		return "ip:" + gc.ClientIP()
	}
	return "session:" + ctx.Session().ID()
}

// PerKeyRateLimitMiddleware 返回按 key 隔离的限流中间件。
// 默认 key 策略：优先使用 Session.UserID()，为空时回退到 client IP（HTTP）或 session ID（WS）。
// 适用于 IM 场景：限制单用户消息发送频率，防止单用户耗尽资源。
//
// rate: 每个 key 每秒允许的请求数
// burst: 每个 key 的桶容量（允许的突发请求数）
// 超过限流阈值时返回 429。
func PerKeyRateLimitMiddleware(rate float64, burst int, opts ...PerKeyRateLimitOption) Middleware {
	cfg := &perKeyRateLimitConfig{
		keyFunc: defaultRateLimitKey,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	pool := newRateLimitPool(rate, burst)

	return func(next Handler) Handler {
		return func(ctx *Context) error {
			key := cfg.keyFunc(ctx)
			if !pool.allow(key) {
				return ctx.ReplyError(http.StatusTooManyRequests, "rate limit exceeded")
			}
			return next(ctx)
		}
	}
}

// PerKeyRateLimitCleanup 停止 PerKeyRateLimitMiddleware 创建的清理 goroutine。
// 返回的 cleanup 函数应在服务关闭时执行（通过 Pipeline.AddCleanup 或手动调用）。
func PerKeyRateLimitCleanup(rate float64, burst int) func() {
	pool := newRateLimitPool(rate, burst)
	return pool.Stop
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

// ── SecureHeadersMiddleware ───────────────────────────────

// SecureHeadersMiddleware 添加常见安全响应头，防止 XSS、MIME 嗅探、点击劫持等攻击。
// 仅对 HTTP 模式生效（WebSocket 无 HTTP 响应头）。
var SecureHeadersMiddleware Middleware = func(next Handler) Handler {
	return func(ctx *Context) error {
		if gc := ctx.Gin(); gc != nil {
			header := gc.Writer.Header()
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "DENY")
			header.Set("X-XSS-Protection", "1; mode=block")
			header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		}
		return next(ctx)
	}
}

// ── 内部辅助 ──────────────────────────────────────────────

// errTokenNotFound 表示未在请求中找到 JWT token。
var errTokenNotFound = errors.New("mux: token not found")

// headerAccessor 抽象 Header 访问，便于类型断言。
type headerAccessor interface {
	GetHeader(string) string
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
// 提取优先级：HTTP Authorization header → WebSocket Session Meta。
func extractToken(ctx *Context) (string, error) {
	// HTTP 模式：从 Authorization header 提取
	if raw := ctx.Raw(); raw != nil {
		if gc, ok := raw.(headerAccessor); ok {
			token := gc.GetHeader("Authorization")
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

	return "", errTokenNotFound
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
