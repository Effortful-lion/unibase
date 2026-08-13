package mux

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/websocketx"
	"github.com/gin-gonic/gin"
)

// EngineOption 配置 Engine 的可选参数。
type EngineOption func(*Engine)

// WithReadTimeout 设置 HTTP 读取超时。
func WithReadTimeout(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.readTimeout = d
	}
}

// WithWriteTimeout 设置 HTTP 写入超时。
func WithWriteTimeout(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.writeTimeout = d
	}
}

// WithIdleTimeout 设置 HTTP 空闲超时。
func WithIdleTimeout(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.idleTimeout = d
	}
}

// WithMaxWebSocketConn 设置 WebSocket 最大并发连接数，0 表示不限制。
func WithMaxWebSocketConn(max int) EngineOption {
	return func(e *Engine) {
		e.maxWSConn = max
	}
}

// WithWebSocketPath 设置 WebSocket 升级端点路径，默认为 "/ws"。
func WithWebSocketPath(path string) EngineOption {
	return func(e *Engine) {
		e.wsPath = path
	}
}

// WithHTTPMiddleware 注册全局 HTTP 中间件，按注册顺序执行。
func WithHTTPMiddleware(mws ...gin.HandlerFunc) EngineOption {
	return func(e *Engine) {
		e.middlewares = append(e.middlewares, mws...)
	}
}

// Engine 是 mux 框架的核心入口，聚合 HTTP 引擎和 WebSocket 服务。
type Engine struct {
	httpServer *http.Server
	wsHub      *websocketx.Hub
	wsRouter   *websocketx.Router
	httpEngine *gin.Engine

	addr        string
	wsPath      string
	middlewares []gin.HandlerFunc

	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
	maxWSConn    int
}

// New 创建一个 Engine（不启动服务）。
//
// 使用示例：
//
//	engine := mux.New(
//	    mux.WithReadTimeout(10 * time.Second),
//	    mux.WithMaxWebSocketConn(10000),
//	)
func New(opts ...EngineOption) *Engine {
	e := &Engine{
		httpEngine: gin.New(),
		wsRouter:   websocketx.NewRouter(),
		wsPath:     "/ws",
	}

	for _, opt := range opts {
		opt(e)
	}

	// 根据 maxWSConn 创建 Hub（仅创建一次）
	e.wsHub = websocketx.NewHub(e.maxWSConn)

	// 应用全局 HTTP 中间件
	for _, mw := range e.middlewares {
		e.httpEngine.Use(mw)
	}

	return e
}

// HTTP 返回底层 HTTP 引擎，用于注册路由和中间件。
//
// 使用示例：
//
//	engine.HTTP().GET("/api/health", func(c *gin.Context) {
//	    httpx.ResponseOK(c, "ok")
//	})
//	engine.HTTP().Use(httpx.JWT([]byte("secret")))
func (e *Engine) HTTP() *gin.Engine {
	return e.httpEngine
}

// WS 返回 WebSocket Router，用于注册 Cmd 路由和 WebSocket 中间件。
//
// 使用示例：
//
//	websocketx.Cmd(engine.WS(), "chat.message", handler.ChatMessage)
//	engine.WS().Use(authMiddleware)
func (e *Engine) WS() *websocketx.Router {
	return e.wsRouter
}

// WSHub 返回 WebSocket Hub，用于广播消息、查询 Session、获取连接数。
//
// 使用示例：
//
//	engine.WSHub().Broadcast(ctx, &websocketx.CmdMessage{
//	    Cmd:  "system.notice",
//	    Body: json.RawMessage(`{"text":"hello"}`),
//	})
func (e *Engine) WSHub() *websocketx.Hub {
	return e.wsHub
}

// Run 启动 HTTP + WebSocket 服务（阻塞）。
//
// addr 格式遵循标准 net.Listen 约定，如 ":8080"、"127.0.0.1:8080"。
// 内部流程：
//  1. 创建 TCP listener（或使用已提供的 listener）
//  2. 将 WebSocket Upgrade Handler 挂载到 WebSocket 路径（默认 "/ws"）
//  3. 启动 HTTP 服务器
func (e *Engine) Run(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return e.Serve(listener)
}

// Serve 在已有 listener 上启动服务。测试时用于精确控制端口和关闭。
func (e *Engine) Serve(listener net.Listener) error {
	e.addr = listener.Addr().String()

	// 挂载 WebSocket 到 Gin 的 /ws 路径（仅 GET 方法支持升级）
	e.httpEngine.GET(e.wsPath, func(c *gin.Context) {
		websocketx.Upgrade(e.wsHub, e.wsRouter.Handle).ServeHTTP(c.Writer, c.Request)
	})

	e.httpServer = &http.Server{
		Handler:      e.httpEngine,
		ReadTimeout:  e.readTimeout,
		WriteTimeout: e.writeTimeout,
		IdleTimeout:  e.idleTimeout,
	}

	return e.httpServer.Serve(listener)
}

// Shutdown 优雅关闭 HTTP Server 和 WebSocket Hub。
func (e *Engine) Shutdown(ctx context.Context) error {
	if e.httpServer != nil {
		if err := e.httpServer.Shutdown(ctx); err != nil {
			return err
		}
	}

	if e.wsHub != nil {
		return e.wsHub.Shutdown(ctx)
	}

	return nil
}

// ServeHTTP 让 Engine 实现 http.Handler 接口，方便嵌入其他 HTTP 服务器。
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.httpEngine.ServeHTTP(w, r)
}

// Use 注册全局 HTTP 中间件（等效于 engine.HTTP().Use(...) 的快捷方式）。
func (e *Engine) Use(mws ...gin.HandlerFunc) {
	e.httpEngine.Use(mws...)
}
