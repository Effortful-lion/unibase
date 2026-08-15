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

	// WebSocket 压缩
	wsCompressionEnabled bool
	wsCompressionLevel   int

	// 传输层开关（默认 true，保持向后兼容）
	enableHTTP bool
	enableWS   bool

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
		enableHTTP:        true,
		enableWS:          true,
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

	// 条件性创建 HTTP transport
	if o.enableHTTP {
		e.httpTransport = transportx.NewHTTPTransport(transportx.HTTPOptions{
			Addr:         o.httpAddr,
			ReadTimeout:  o.readTimeout,
			WriteTimeout: o.writeTimeout,
			IdleTimeout:  o.idleTimeout,
		})
		e.httpEngine = e.httpTransport.Engine()

		// 应用全局 HTTP 中间件
		for _, mw := range o.middlewares {
			e.httpEngine.Use(mw)
		}
		// 应用 CORS 中间件（如果配置）
		if o.corsMiddleware != nil {
			e.httpEngine.Use(o.corsMiddleware)
		}
	}

	// 条件性创建 WS transport
	if o.enableWS {
		e.wsTransport = transportx.NewWSTransport(transportx.WSOptions{
			Path:              o.wsPath,
			MaxConn:           o.maxWSConn,
			HeartbeatInterval: o.heartbeatInterval,
			HeartbeatPongWait: o.heartbeatPongWait,
			MaxMessageSize:    o.maxWSMessageSize,
		})
		e.wsHub = e.wsTransport.Hub()
		e.wsRouter = e.wsTransport.Router()
	}

	// 条件性初始化集群管理器
	if o.clusterEnabled && o.clusterRedisAddr != "" {
		e.initCluster(o)
	}

	return e
}

// initCluster 初始化集群管理器。
// WS 组件（SessionRegistry、BroadcastBus）仅在 enableWS 时注入。
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

	// 仅在 WS 启用时注入跨 AP 集群组件到 Hub
	if o.enableWS && e.wsTransport != nil {
		nodeID := self.Tag
		e.wsTransport.Hub().SetNodeID(nodeID)
		e.wsTransport.Hub().SetSessionRegistry(websocketx.NewRedisSessionRegistry(rdb))
		e.wsTransport.Hub().SetBroadcastBus(websocketx.NewRedisBroadcastBus(rdb, self.Group, nodeID))
	}
}

// ── 底层访问（向后兼容）───────────────────────────────────────

// HTTP 返回底层 HTTP 引擎，用于注册路由和中间件。
// HTTP-only 模式返回最小引擎；WS-only 模式返回承载 Upgrade 的最小引擎；全栈模式返回主引擎。
// WS-only 模式下 httpEngine 在 Serve 阶段创建，通过 startMu 保护并发读取。
func (e *Engine) HTTP() *gin.Engine {
	e.startMu.RLock()
	defer e.startMu.RUnlock()
	return e.httpEngine
}

// WS 返回 WebSocket Router，用于注册 Cmd 路由和 WebSocket 中间件。
// HTTP-only 模式返回 nil。
func (e *Engine) WS() *websocketx.Router {
	return e.wsRouter
}

// WSHub 返回 WebSocket Hub，用于广播消息、查询 Session、获取连接数。
// HTTP-only 模式返回 nil。
func (e *Engine) WSHub() *websocketx.Hub {
	return e.wsHub
}

// ClusterManager 返回集群管理器，nil 表示单机模式。
func (e *Engine) ClusterManager() *cluster.ClusterManager {
	return e.cluster
}

// RouteUser 根据 user_id 通过一致性哈希路由到目标节点。
// 仅集群模式下有效，返回目标节点及是否找到。
func (e *Engine) RouteUser(userID string) (cluster.ClusterNode, bool) {
	if e.cluster == nil {
		return cluster.ClusterNode{}, false
	}
	return e.cluster.RouteUser(userID)
}

// ── 生命周期 ────────────────────────────────────────────────

// Run 启动服务（阻塞）。
// 使用 opts.httpAddr 作为监听地址。
// 默认同时启动 HTTP 和 WebSocket（向后兼容）。
// 可通过 DisableHTTP() / DisableWS() 选择性启用。
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

// Serve 在已有 listener 上启动服务，根据协议开关选择启动模式。
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

	var err error
	switch {
	case e.opts.enableHTTP && e.opts.enableWS:
		err = e.serveBoth(listener)
	case e.opts.enableHTTP && !e.opts.enableWS:
		err = e.serveHTTPOnly(listener)
	case !e.opts.enableHTTP && e.opts.enableWS:
		err = e.serveWSOnly(listener)
	default:
		err = fmt.Errorf("mux: at least one protocol must be enabled")
	}

	// 停止集群管理器
	if e.cluster != nil {
		e.cluster.Stop()
	}

	return err
}

// serveBoth 全栈模式：HTTP + WebSocket（当前默认行为）。
// 适用于同时提供 REST API 和 WebSocket 长连接的服务（如 IM 后端）。
func (e *Engine) serveBoth(listener net.Listener) error {
	// 启动 WebSocket Hub（后台管理连接生命周期）
	go func() {
		defer e.wg.Done()
		if err := e.wsTransport.Serve(e.ctx); err != nil {
			logx.Default().Module("mux").Error("websocket transport stopped", logx.Fields{"error": err})
		}
	}()

	// 挂载 WebSocket Upgrade Handler
	e.mountWSUpgrade()

	// 注册 Cmd 入口
	if e.pipeline != nil {
		e.pipeline.engine = e
		e.httpEngine.POST(e.opts.cmdPath, e.pipeline.CmdHTTPHandler())
		e.wsRouter.Use(websocketx.RecoverMiddleware)
		e.wsRouter.Cmd("*", e.pipeline.CmdWSHandler())
	}

	// 启动 HTTP 服务（阻塞，直到 listener 关闭）
	err := e.httpTransport.ServeListener(e.ctx, listener)

	// HTTP 服务已退出，取消 context 以通知 WebSocket Hub 停止
	e.cancel()

	// 等待 WebSocket Hub 停止
	e.wg.Wait()
	return err
}

// serveHTTPOnly 纯 HTTP 模式：仅 REST 路由 + HTTP Cmd，无 WebSocket。
// 适用于纯 REST API 微服务，无需长连接。
func (e *Engine) serveHTTPOnly(listener net.Listener) error {
	// 注册 HTTP Cmd 入口
	if e.pipeline != nil {
		e.pipeline.engine = e
		e.httpEngine.POST(e.opts.cmdPath, e.pipeline.CmdHTTPHandler())
	}

	// 启动 HTTP 服务（阻塞，直到 listener 关闭）
	err := e.httpTransport.ServeListener(e.ctx, listener)

	e.cancel()
	return err
}

// serveWSOnly 纯 WebSocket 模式：WS Upgrade + WS Cmd，无 REST 路由，无 HTTP Cmd。
// 使用最小 Gin 引擎承载 Upgrade 握手（WebSocket 协议本身的限制）。
// 适用于纯长连接服务（如实时推送、游戏服务器）。
func (e *Engine) serveWSOnly(listener net.Listener) error {
	// 创建最小 Gin 引擎承载 WS Upgrade
	wsEngine := gin.New()
	wsEngine.Use(gin.Recovery())

	// 挂载 WS Upgrade
	e.mountWSUpgradeOn(wsEngine)

	// 注册 WS Cmd 入口
	if e.pipeline != nil {
		e.pipeline.engine = e
		e.wsRouter.Use(websocketx.RecoverMiddleware)
		e.wsRouter.Cmd("*", e.pipeline.CmdWSHandler())
	}

	// 创建最小 HTTP transport 承载 Upgrade（无 REST 路由、无 HTTP Cmd）
	e.httpTransport = transportx.NewHTTPTransport(transportx.HTTPOptions{
		Addr:         e.opts.httpAddr,
		ReadTimeout:  e.opts.readTimeout,
		WriteTimeout: e.opts.writeTimeout,
		IdleTimeout:  e.opts.idleTimeout,
	})
	e.httpTransport.SetEngine(wsEngine)
	e.startMu.Lock()
	e.httpEngine = wsEngine
	e.startMu.Unlock()

	// 启动 WebSocket Hub
	go func() {
		defer e.wg.Done()
		if err := e.wsTransport.Serve(e.ctx); err != nil {
			logx.Default().Module("mux").Error("websocket transport stopped", logx.Fields{"error": err})
		}
	}()

	// 使用最小引擎启动 HTTP 服务（承载 Upgrade 握手）
	err := e.httpTransport.ServeListener(e.ctx, listener)

	e.cancel()
	e.wg.Wait()
	return err
}

// mountWSUpgrade 在主引擎上挂载 WS Upgrade 路由（全栈模式）。
func (e *Engine) mountWSUpgrade() {
	e.httpEngine.GET(e.opts.wsPath, func(c *gin.Context) {
		e.handleWSUpgrade(c)
	})
}

// mountWSUpgradeOn 在指定引擎上挂载 WS Upgrade 路由（纯 WS 模式）。
func (e *Engine) mountWSUpgradeOn(engine *gin.Engine) {
	engine.GET(e.opts.wsPath, func(c *gin.Context) {
		e.handleWSUpgrade(c)
	})
}

// handleWSUpgrade 处理 WebSocket 升级请求的公共逻辑。
func (e *Engine) handleWSUpgrade(c *gin.Context) {
	token := c.Query("token")
	upgradeOpts := []websocketx.UpgradeOption{}
	if token != "" {
		initFn := func(s *websocketx.Session) {
			s.SetMeta("jwt_token", token)
		}
		ctx := context.WithValue(c.Request.Context(), websocketx.SessionInitCtxKey, initFn)
		c.Request = c.Request.WithContext(ctx)
	}
	if e.opts.checkOrigin != nil {
		upgradeOpts = append(upgradeOpts, websocketx.WithCheckOrigin(e.opts.checkOrigin))
	}
	if e.opts.wsCompressionEnabled {
		upgradeOpts = append(upgradeOpts, websocketx.WithCompression(e.opts.wsCompressionLevel))
	}
	websocketx.Upgrade(e.wsHub, e.wsRouter.Handle, upgradeOpts...).ServeHTTP(c.Writer, c.Request)
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

	if e.httpTransport != nil {
		if err := e.httpTransport.Close(ctx); err != nil {
			return err
		}
	}
	if e.wsTransport != nil {
		if err := e.wsTransport.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ServeHTTP 让 Engine 实现 http.Handler 接口。
// WS-only 模式下，Serve() 之前 httpEngine 为 nil，返回 503。
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.httpEngine == nil {
		http.Error(w, "mux: HTTP transport not enabled", http.StatusServiceUnavailable)
		return
	}
	e.httpEngine.ServeHTTP(w, r)
}

// Use 注册全局 HTTP 中间件（快捷方式）。
// 注意：此方法注册的是 gin 中间件，仅作用于 HTTP REST 路由。
// Cmd 路由（HTTP Cmd + WS Cmd）的中间件应通过 pipeline.Use() 注册。
// WS-only 模式下静默忽略（无 HTTP 路由可挂载）。
func (e *Engine) Use(mws ...gin.HandlerFunc) {
	if e.httpEngine == nil {
		return
	}
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

	// 条件性注册 HTTP Cmd 入口
	if e.opts.enableHTTP {
		e.httpEngine.POST(p.cmdPath, p.CmdHTTPHandler())
	}

	// 条件性注册 WS Cmd 入口
	if e.opts.enableWS && e.wsRouter != nil {
		e.wsRouter.Use(websocketx.RecoverMiddleware)
		e.wsRouter.Cmd("*", p.CmdWSHandler())
	}
}

// LogRoutes 输出所有已注册路由信息（REST + HTTP Cmd + WebSocket Cmd）。
// 基于 logx 输出结构化日志，方便调试和运维排查。
func (e *Engine) LogRoutes(logger *logx.Logger) {
	if e.pipeline != nil {
		e.pipeline.LogRoutes(logger)
	}
}
