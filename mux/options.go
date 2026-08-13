package mux

import (
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/mux/internal/httpx/middleware"
	"github.com/gin-gonic/gin"
)

// EngineOption 配置 Engine 的可选参数。
type EngineOption func(*Engine, *engineOptions)

// ── 基础选项 ──────────────────────────────────────────────────

// WithReadTimeout 设置 HTTP 读取超时。
func WithReadTimeout(d time.Duration) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.readTimeout = d
	}
}

// WithWriteTimeout 设置 HTTP 写入超时。
func WithWriteTimeout(d time.Duration) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.writeTimeout = d
	}
}

// WithIdleTimeout 设置 HTTP 空闲超时。
func WithIdleTimeout(d time.Duration) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.idleTimeout = d
	}
}

// WithCmdPath 设置 HTTP Cmd 入口路径，默认为 "/v1/cmd"。
func WithCmdPath(path string) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.cmdPath = path
	}
}

// WithHTTPMiddleware 注册全局 HTTP 中间件，按注册顺序执行。
func WithHTTPMiddleware(mws ...gin.HandlerFunc) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.middlewares = append(o.middlewares, mws...)
	}
}

// WithCORS 启用 CORS 中间件，配置跨域请求规则。
func WithCORS(opts ...middleware.CORSOption) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.corsMiddleware = middleware.CORS(opts...)
	}
}

// WithCheckOrigin 设置 WebSocket 跨域校验函数。
// nil 表示使用默认校验（仅允许相同来源）。
// 允许所有来源（不推荐生产使用）：func(*http.Request) bool { return true }
func WithCheckOrigin(fn func(*http.Request) bool) EngineOption {
	return func(e *Engine, o *engineOptions) {
		o.checkOrigin = fn
	}
}
