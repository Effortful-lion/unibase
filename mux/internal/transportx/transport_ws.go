package transportx

import (
	"context"
	"time"

	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
)

// WSOptions 配置 WebSocket 传输层。
type WSOptions struct {
	Path              string
	MaxConn           int
	HeartbeatInterval time.Duration
	HeartbeatPongWait time.Duration
	MaxMessageSize    int64
}

// DefaultWSOptions 返回默认 WebSocket 配置。
func DefaultWSOptions() WSOptions {
	return WSOptions{
		Path:              "/ws",
		MaxConn:           10000,
		HeartbeatInterval: 30 * time.Second,
		HeartbeatPongWait: 10 * time.Second,
		MaxMessageSize:    1024 * 1024, // 1MB
	}
}

// WSTransport 封装 WebSocket Hub 和 Router，实现 Transport 接口。
type WSTransport struct {
	hub    *websocketx.Hub
	router *websocketx.Router
	opts   WSOptions
}

// NewWSTransport 创建 WebSocket 传输层。
func NewWSTransport(opts WSOptions) *WSTransport {
	hubOpts := []websocketx.HubOption{
		websocketx.WithHeartbeat(opts.HeartbeatInterval, opts.HeartbeatPongWait),
		websocketx.WithMaxMessageSize(opts.MaxMessageSize),
	}

	return &WSTransport{
		hub:    websocketx.NewHub(opts.MaxConn, hubOpts...),
		router: websocketx.NewRouter(),
		opts:   opts,
	}
}

// Hub 返回底层 Hub，用于广播和管理连接。
func (t *WSTransport) Hub() *websocketx.Hub {
	return t.hub
}

// Router 返回底层 Router，用于注册 Cmd 路由和中间件。
func (t *WSTransport) Router() *websocketx.Router {
	return t.router
}

// Serve 无需后台循环：WebSocket 连接通过 HTTP Upgrade 建立，Hub 在后台管理连接生命周期。
func (t *WSTransport) Serve(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Close 优雅关闭 WebSocket 服务。
func (t *WSTransport) Close(ctx context.Context) error {
	return t.hub.Shutdown(ctx)
}

// ── 路由注册快捷方法 ────────────────────────────────────────

func (t *WSTransport) Cmd(pattern string, handler websocketx.MessageHandler, mws ...websocketx.MiddlewareFunc) {
	t.router.Cmd(pattern, handler, mws...)
}

func (t *WSTransport) Use(mw websocketx.MiddlewareFunc) {
	t.router.Use(mw)
}
