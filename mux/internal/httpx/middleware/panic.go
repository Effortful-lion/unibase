package middleware

import (
	"runtime/debug"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/httpx/response"
	"github.com/gin-gonic/gin"
)

// PanicOption  panic 恢复中间件配置。
type PanicOption func(*panicConfig)

type panicConfig struct {
	logger *logx.Logger
}

func defaultPanicConfig() *panicConfig {
	return &panicConfig{}
}

// WithPanicLogger 注入自定义日志器（默认使用 logx.Default()）。
func WithPanicLogger(logger *logx.Logger) PanicOption {
	return func(c *panicConfig) {
		c.logger = logger
	}
}

// Panic 返回 panic 恢复中间件。
//
// 捕获 handler 中的 panic，记录错误日志并返回 500 响应，
// 避免进程崩溃。日志包含 panic 值和堆栈信息。
//
// 日志格式：
//
//	level=ERROR [httpx] panic recovered method=POST path=/api/users error="divide by zero" stack="..."
//
// 示例：
//
//	r.Use(middleware.Panic())
//	r.Use(middleware.Panic(middleware.WithPanicLogger(myLogger)))
func Panic(opts ...PanicOption) gin.HandlerFunc {
	cfg := defaultPanicConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	logger := cfg.logger
	if logger == nil {
		logger = logx.Default().Module("mux")
	}

	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				logger.Error("panic recovered", logx.Fields{
					"method": c.Request.Method,
					"path":   c.FullPath(),
					"error":  rec,
					"stack":  string(stack),
				})

				response.ResponseFail(c, 500, "10500", "internal server error")
				c.Abort()
			}
		}()

		c.Next()
	}
}
