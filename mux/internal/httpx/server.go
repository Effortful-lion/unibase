package httpx

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/gin-gonic/gin"
)

// ── 配置选项 ─────────────────────────────────────────────────────

// ServerOption 服务启动配置。
type ServerOption func(*serverConfig)

type serverConfig struct {
	timeout     time.Duration
	logger      *logx.Logger
	preShutdown func(context.Context) error
}

func defaultServerConfig() *serverConfig {
	return &serverConfig{
		timeout: 30 * time.Second,
	}
}

// WithShutdownTimeout 设置优雅关闭等待超时时间。
// 默认 30 秒。超时后强制退出。
func WithShutdownTimeout(timeout time.Duration) ServerOption {
	return func(c *serverConfig) {
		c.timeout = timeout
	}
}

// WithShutdownLogger 注入自定义日志器（默认使用 logx.Default()）。
func WithShutdownLogger(logger *logx.Logger) ServerOption {
	return func(c *serverConfig) {
		c.logger = logger
	}
}

// WithShutdownHook 注册关闭前钩子函数。
// 钩子在收到信号后、关闭 listener 前执行，用于清理资源（关闭 DB、发送退出事件等）。
// 多个钩子按注册顺序串行执行，任一钩子失败时记录日志并继续后续钩子。
// ctx 在超时时会被取消，钩子应尊重 ctx 信号。
func WithShutdownHook(hook func(context.Context) error) ServerOption {
	return func(c *serverConfig) {
		c.preShutdown = hook
	}
}

// ── 服务启动 ──────────────────────────────────────────────────────

// RunWithShutdown 启动 Gin 服务并监听退出信号。
//
// 监听 SIGTERM / SIGINT 信号，收到信号后执行：
//  1. 运行 WithShutdownHook 钩子（串行）
//  2. 调用 server.Shutdown 等待活跃请求完成
//  3. 超时则强制退出
//
// 示例：
//
//	r := gin.Default()
//	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
//
//	if err := httpx.RunWithShutdown(r, ":8080",
//	    httpx.WithShutdownTimeout(15*time.Second),
//	    httpx.WithShutdownHook(func(ctx context.Context) error {
//	        return db.Close()
//	    }),
//	); err != nil {
//	    log.Fatal(err)
//	}
func RunWithShutdown(r *gin.Engine, addr string, opts ...ServerOption) error {
	cfg := defaultServerConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	logger := cfg.logger
	if logger == nil {
		logger = logx.Default().Module("mux")
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// 启动 listener
	go func() {
		logger.Info("server starting", logx.Fields{"addr": addr})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server listen error", logx.Fields{"error": err})
		}
	}()

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutdown signal received", logx.Fields{"signal": sig.String()})

	// 创建带超时的关闭 context
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	// 异步执行关闭流程
	go func() {
		defer wg.Done()

		// 执行关闭前钩子
		if cfg.preShutdown != nil {
			logger.Info("running pre-shutdown hooks")
			if err := cfg.preShutdown(ctx); err != nil {
				logger.Error("pre-shutdown hook error", logx.Fields{"error": err})
			}
		}

		// 优雅关闭：停止接受新请求，等待活跃请求完成
		logger.Info("shutting down server", logx.Fields{"timeout": cfg.timeout.String()})
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("server shutdown error", logx.Fields{"error": err})
		}
	}()

	// 等待关闭完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("server exited gracefully")
		return nil
	case <-ctx.Done():
		logger.Warn("shutdown timeout exceeded, forcing exit")
		return ctx.Err()
	}
}

// Run 启动 Gin 服务，无优雅关闭（使用 Gin 原生阻塞方式）。
// 适合开发环境或不需要 graceful shutdown 的场景。
func Run(r *gin.Engine, addr string) error {
	return r.Run(addr)
}
