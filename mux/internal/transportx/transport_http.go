package transportx

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/gin-gonic/gin"
)

// HTTPOptions 配置 HTTP 传输层。
type HTTPOptions struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	Logger       *logx.Logger
}

// DefaultHTTPOptions 返回默认 HTTP 配置。
func DefaultHTTPOptions() HTTPOptions {
	return HTTPOptions{
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
}

// HTTPTransport 封装 Gin HTTP 引擎，实现 Transport 接口。
type HTTPTransport struct {
	engine *gin.Engine
	opts   HTTPOptions
	srv    *http.Server
}

// NewHTTPTransport 创建 HTTP 传输层。
func NewHTTPTransport(opts HTTPOptions) *HTTPTransport {
	if opts.Logger == nil {
		opts.Logger = logx.Default().Module("mux")
	}

	engine := gin.New()
	engine.Use(gin.Recovery())

	return &HTTPTransport{
		engine: engine,
		opts:   opts,
	}
}

// Engine 返回底层 Gin 引擎，用于注册路由和中间件。
func (t *HTTPTransport) Engine() *gin.Engine {
	return t.engine
}

// Server 返回底层 http.Server，用于外部优雅关闭。
func (t *HTTPTransport) Server() *http.Server {
	return t.srv
}

// SetServer 设置由外部管理的 http.Server，用于 Close 时优雅关闭。
func (t *HTTPTransport) SetServer(srv *http.Server) {
	t.srv = srv
}

// Serve 启动 HTTP 服务（阻塞）。
func (t *HTTPTransport) Serve(ctx context.Context) error {
	t.srv = &http.Server{
		Addr:         t.opts.Addr,
		Handler:      t.engine,
		ReadTimeout:  t.opts.ReadTimeout,
		WriteTimeout: t.opts.WriteTimeout,
		IdleTimeout:  t.opts.IdleTimeout,
	}

	go func() {
		t.opts.Logger.Info("http server starting", logx.Fields{"addr": t.opts.Addr})
		if err := t.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.opts.Logger.Error("http server error", logx.Fields{"error": err})
		}
	}()

	<-ctx.Done()
	return t.srv.Shutdown(context.Background())
}

// ServeListener 在已有 listener 上启动 HTTP 服务，阻塞直到 listener 关闭或 ctx 取消。
func (t *HTTPTransport) ServeListener(ctx context.Context, ln net.Listener) error {
	t.srv = &http.Server{
		Handler:      t.engine,
		ReadTimeout:  t.opts.ReadTimeout,
		WriteTimeout: t.opts.WriteTimeout,
		IdleTimeout:  t.opts.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- t.srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return t.srv.Shutdown(shutdownCtx)
	}
}

// Close 优雅关闭 HTTP 服务。
func (t *HTTPTransport) Close(ctx context.Context) error {
	if t.srv == nil {
		return nil
	}
	return t.srv.Shutdown(ctx)
}

// ── 路由注册快捷方法 ────────────────────────────────────────

func (t *HTTPTransport) GET(path string, handler gin.HandlerFunc) {
	t.engine.GET(path, handler)
}

func (t *HTTPTransport) POST(path string, handler gin.HandlerFunc) {
	t.engine.POST(path, handler)
}

func (t *HTTPTransport) PUT(path string, handler gin.HandlerFunc) {
	t.engine.PUT(path, handler)
}

func (t *HTTPTransport) DELETE(path string, handler gin.HandlerFunc) {
	t.engine.DELETE(path, handler)
}

func (t *HTTPTransport) Use(mws ...gin.HandlerFunc) {
	t.engine.Use(mws...)
}
