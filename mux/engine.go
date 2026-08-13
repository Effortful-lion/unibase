package mux

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/cluster"
	"github.com/Effortful-lion/unibase/mux/internal/transportx"
	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Engine 是 mux 框架的核心入口。
type Engine struct {
	ctx    context.Context
	cancel context.CancelFunc

	// 传输层
	httpTransport *transportx.HTTPTransport
	wsTransport   *transportx.WSTransport

	// 向后兼容的底层访问（与旧测试 API 保持一致）
	httpEngine *gin.Engine
	wsHub      *websocketx.Hub
	wsRouter   *websocketx.Router

	opts    *engineOptions
	started bool
	startMu sync.RWMutex
	wg      sync.WaitGroup

	// 集群管理器（nil 表示单机模式）
	cluster *cluster.ClusterManager

	// 已挂载的 Pipeline（用于路由日志输出）
	pipeline *Pipeline
}

// engineOptions 存储 Engine 的所有配置项。
type engineOptions struct {
	httpAddr          string
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	wsPath            string
	maxWSConn         int
	heartbeatInterval time.Duration
	heartbeatPongWait time.Duration
	maxWSMessageSize  int64
	cmdPath           string
	middlewares       []gin.HandlerFunc
	corsMiddleware    gin.HandlerFunc
	checkOrigin       func(*http.Request) bool

	// 集群配置（Engine 自动管理 ClusterManager 生命周期）
	clusterEnabled           bool
	clusterRole              cluster.Role
	clusterRedisAddr         string
	clusterHeartbeatInterval time.Duration
	clusterNodeTTL           time.Duration
	clusterServiceName       string
	clusterGroup             string
	clusterAdvertiseAddr     string
}

func defaultEngineOptions() *engineOptions {
	return &engineOptions{
		httpAddr:          ":8080",
		readTimeout:       10 * time.Second,
		writeTimeout:      10 * time.Second,
		idleTimeout:       30 * time.Second,
		wsPath:            "/ws",
		maxWSConn:         10000,
		heartbeatInterval: 30 * time.Second,
		heartbeatPongWait: 10 * time.Second,
		maxWSMessageSize:  1024 * 1024,
		cmdPath:           "/v1/cmd",
	}
}

// New 创建一个 Engine（不启动服务）。
func New(opts ...EngineOption) *Engine {
	e := &Engine{}
	o := defaultEngineOptions()

	for _, opt := range opts {
		opt(e, o)
	}
	e.opts = o

	ctx, cancel := context.WithCancel(context.Background())
	e.ctx = ctx
	e.cancel = cancel

	// 创建传输层
	e.httpTransport = transportx.NewHTTPTransport(transportx.HTTPOptions{
		Addr:         o.httpAddr,
		ReadTimeout:  o.readTimeout,
		WriteTimeout: o.writeTimeout,
		IdleTimeout:  o.idleTimeout,
	})

	e.wsTransport = transportx.NewWSTransport(transportx.WSOptions{
		Path:              o.wsPath,
		MaxConn:           o.maxWSConn,
		HeartbeatInterval: o.heartbeatInterval,
		HeartbeatPongWait: o.heartbeatPongWait,
		MaxMessageSize:    o.maxWSMessageSize,
	})

	// 向后兼容：底层对象直接暴露
	e.httpEngine = e.httpTransport.Engine()
	e.wsHub = e.wsTransport.Hub()
	e.wsRouter = e.wsTransport.Router()

	// 应用全局 HTTP 中间件
	for _, mw := range o.middlewares {
		e.httpEngine.Use(mw)
	}

	// 应用 CORS 中间件（如果配置）
	if o.corsMiddleware != nil {
		e.httpEngine.Use(o.corsMiddleware)
	}

	// 创建集群管理器（如果启用）
	if o.clusterEnabled && o.clusterRedisAddr != "" {
		e.initCluster(o)
	}

	return e
}

// initCluster 初始化集群管理器。
func (e *Engine) initCluster(o *engineOptions) {
	advertiseAddr := o.clusterAdvertiseAddr
	if advertiseAddr == "" {
		advertiseAddr = o.httpAddr
	}
	if advertiseAddr == "" {
		logx.Default().Module("mux").Warn("cluster enabled but no advertise addr, using :8080")
		advertiseAddr = ":8080"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         o.clusterRedisAddr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		MinIdleConns: 2,
	})
	discovery := cluster.NewRedisDiscovery(rdb, "cluster:nodes")
	self := cluster.ClusterNode{
		Tag:         fmt.Sprintf("%s-%s", o.clusterServiceName, o.clusterRole.String()),
		ServiceName: o.clusterServiceName,
		Group:       o.clusterGroup,
		Role:        o.clusterRole,
		IPPort:      advertiseAddr,
		ConnectUrl:  "http://" + advertiseAddr,
	}
	e.cluster = cluster.NewClusterManager(
		e.ctx, self, discovery,
		cluster.WithClusterHeartbeatInterval(o.clusterHeartbeatInterval),
		cluster.WithClusterNodeTTL(o.clusterNodeTTL),
		cluster.WithClusterCmdPath(o.cmdPath),
	)
	e.cluster.SetCloseFn(func() error {
		return rdb.Close()
	})
}

// ── 底层访问（向后兼容）───────────────────────────────────────

// HTTP 返回底层 HTTP 引擎，用于注册路由和中间件。
func (e *Engine) HTTP() *gin.Engine {
	return e.httpEngine
}

// WS 返回 WebSocket Router，用于注册 Cmd 路由和 WebSocket 中间件。
func (e *Engine) WS() *websocketx.Router {
	return e.wsRouter
}

// WSHub 返回 WebSocket Hub，用于广播消息、查询 Session、获取连接数。
func (e *Engine) WSHub() *websocketx.Hub {
	return e.wsHub
}

// ClusterManager 返回集群管理器，nil 表示单机模式。
func (e *Engine) ClusterManager() *cluster.ClusterManager {
	return e.cluster
}

// ── 生命周期 ────────────────────────────────────────────────

// Run 启动 HTTP + WebSocket 服务（阻塞）。
// 使用 opts.httpAddr 作为监听地址。
func (e *Engine) Run(addr string) error {
	if addr == "" {
		addr = e.opts.httpAddr
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return e.Serve(listener)
}

// Serve 在已有 listener 上启动服务。
func (e *Engine) Serve(listener net.Listener) error {
	e.startMu.Lock()
	if e.started {
		e.startMu.Unlock()
		return fmt.Errorf("mux: Engine.Serve already called")
	}
	e.started = true
	e.startMu.Unlock()
	defer func() { e.started = false }()

	e.wg.Add(1)

	// 启动集群管理器（如果启用）
	if e.cluster != nil {
		e.cluster.Start()
	}

	// 启动 WebSocket Hub（后台管理连接生命周期）
	go func() {
		defer e.wg.Done()
		if err := e.wsTransport.Serve(e.ctx); err != nil {
			logx.Default().Module("mux").Error("websocket transport stopped", logx.Fields{"error": err})
		}
	}()

	// 挂载 WebSocket Upgrade Handler
	e.httpEngine.GET(e.opts.wsPath, func(c *gin.Context) {
		// 从 URL query 参数提取 JWT token，注入到 Session meta（连接建立阶段）
		token := c.Query("token")
		upgradeOpts := []websocketx.UpgradeOption{}
		if token != "" {
			initFn := func(s *websocketx.Session) {
				s.SetMeta("jwt_token", token)
			}
			// 将 initFn 注入 request context，通过 context 传递避免 Hub 的 race
			ctx := context.WithValue(c.Request.Context(), websocketx.SessionInitCtxKey, initFn)
			c.Request = c.Request.WithContext(ctx)
		}
		if e.opts.checkOrigin != nil {
			upgradeOpts = append(upgradeOpts, websocketx.WithCheckOrigin(e.opts.checkOrigin))
		}
		websocketx.Upgrade(e.wsHub, e.wsRouter.Handle, upgradeOpts...).ServeHTTP(c.Writer, c.Request)
	})

	// 启动 HTTP 服务（阻塞，直到 listener 关闭）
	err := e.httpTransport.ServeListener(e.ctx, listener)

	// HTTP 服务已退出，取消 context 以通知 WebSocket Hub 停止
	e.cancel()

	// 停止集群管理器
	if e.cluster != nil {
		e.cluster.Stop()
	}

	// 等待 WebSocket Hub 停止
	e.wg.Wait()
	return err
}

// Shutdown 优雅关闭。
func (e *Engine) Shutdown(ctx context.Context) error {
	defer func() { e.started = false }()

	e.cancel()

	if e.cluster != nil {
		e.cluster.Stop()
	}

	e.startMu.RLock()
	started := e.started
	e.startMu.RUnlock()
	if started {
		e.wg.Wait()
	}

	if err := e.httpTransport.Close(ctx); err != nil {
		return err
	}
	if err := e.wsTransport.Close(ctx); err != nil {
		return err
	}
	return nil
}

// ServeHTTP 让 Engine 实现 http.Handler 接口。
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.httpEngine.ServeHTTP(w, r)
}

// Use 注册全局 HTTP 中间件（快捷方式）。
func (e *Engine) Use(mws ...gin.HandlerFunc) {
	e.httpEngine.Use(mws...)
}

// UsePipeline 将 Pipeline 挂载到 Engine，自动注册 HTTP Cmd 入口和 WS Cmd 入口。
// REST 路由已通过 pipeline.GET/POST/... 实时注册到 gin，无需重复操作。
func (e *Engine) UsePipeline(p *Pipeline) {
	p.engine = e
	e.pipeline = p

	// 同步 cmdPath：以 Engine 配置为准，Pipeline 默认值仅在未设置时生效
	if p.cmdPath == "/v1/cmd" && e.opts.cmdPath != "/v1/cmd" {
		p.cmdPath = e.opts.cmdPath
	}

	// 注册 HTTP Cmd 入口
	e.httpEngine.POST(p.cmdPath, p.CmdHTTPHandler())

	// 注册 WebSocket Cmd 入口
	e.wsRouter.Use(websocketx.RecoverMiddleware)
	e.wsRouter.Cmd("*", p.CmdWSHandler())
}

// LogRoutes 输出所有已注册路由信息（REST + HTTP Cmd + WebSocket Cmd）。
// 基于 logx 输出结构化日志，方便调试和运维排查。
func (e *Engine) LogRoutes(logger *logx.Logger) {
	if e.pipeline != nil {
		e.pipeline.LogRoutes(logger)
	}
}
