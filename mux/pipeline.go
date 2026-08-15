package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/Effortful-lion/unibase/mux/internal/msg"
	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
	"github.com/gin-gonic/gin"
)

// cmdEntry 存储 Cmd 路由的 handler 和局部中间件。
type cmdEntry struct {
	handler    Handler
	middleware []Middleware
}

// prefixMiddleware 存储前缀匹配规则和对应的中间件。
type prefixMiddleware struct {
	prefix string
	mw     []Middleware
}

// Pipeline 管理所有消息的处理流程：中间件链 + RESTful 路由 + Cmd 路由。
type Pipeline struct {
	engine      *Engine
	middleware  []Middleware
	cmdPrefixes []prefixMiddleware

	restRoutes map[string]Handler   // RESTful 路由：path → Handler
	cmdRoutes  map[string]*cmdEntry // Cmd 路由：cmd → cmdEntry

	cmdPath string // HTTP Cmd 入口路径，默认 "/v1/cmd"

	chainMu sync.RWMutex
	chains  map[string]Handler // key → 缓存的中间件链

	// 清理函数（Shutdown 时调用）
	cleanupFns []func()
}

// RESTRoutes 返回所有已注册的 RESTful 路由路径（线程安全快照）。
func (p *Pipeline) RESTRoutes() []string {
	p.chainMu.RLock()
	defer p.chainMu.RUnlock()

	routes := make([]string, 0, len(p.restRoutes))
	for path := range p.restRoutes {
		routes = append(routes, path)
	}
	return routes
}

// CmdRoutes 返回所有已注册的 Cmd 路由名称（线程安全快照）。
func (p *Pipeline) CmdRoutes() []string {
	p.chainMu.RLock()
	defer p.chainMu.RUnlock()

	routes := make([]string, 0, len(p.cmdRoutes))
	for name := range p.cmdRoutes {
		routes = append(routes, name)
	}
	return routes
}

// LogRoutes 通过 logx 输出所有已注册路由信息。
// 格式：REST 路由显示 method + path，Cmd 路由按入口分组显示。
// 如果 logger 为 nil，使用 logx.Default()。
func (p *Pipeline) LogRoutes(logger *logx.Logger) {
	if logger == nil {
		logger = logx.Default()
	}

	var lines []string

	// REST 路由（从 gin 获取 Method + Path，排除 Cmd 入口路径）
	if p.engine != nil && p.engine.httpEngine != nil {
		for _, route := range p.engine.httpEngine.Routes() {
			if route.Path == p.cmdPath {
				continue
			}
			lines = append(lines, route.Method+" "+route.Path)
		}
	}

	// HTTP Cmd 入口：所有 Cmd 路由通过 POST /v1/cmd 访问
	p.chainMu.RLock()
	cmdNames := make([]string, 0, len(p.cmdRoutes))
	for name := range p.cmdRoutes {
		cmdNames = append(cmdNames, name)
	}
	p.chainMu.RUnlock()
	if len(cmdNames) > 0 {
		lines = append(lines, fmt.Sprintf("POST %s → %s", p.cmdPath, strings.Join(cmdNames, ", ")))
	}

	// WebSocket Cmd 入口：所有 Cmd 路由通过 WS /ws 消息访问
	if p.engine != nil && p.engine.wsRouter != nil {
		wsRoutes := p.engine.wsRouter.Routes()
		if len(wsRoutes) > 0 {
			wsCmds := make([]string, 0, len(wsRoutes))
			for _, r := range wsRoutes {
				if r.Cmd == "*" {
					continue
				}
				wsCmds = append(wsCmds, r.Cmd)
			}
			if len(wsCmds) > 0 {
				wsPath := p.engine.opts.wsPath
				lines = append(lines, fmt.Sprintf("WS %s → %s", wsPath, strings.Join(wsCmds, ", ")))
			}
		}
	}

	if len(lines) == 0 {
		logger.Info("routes: (none)")
		return
	}

	logger.Info("routes", logx.Fields{
		"count": len(lines),
		"list":  lines,
	})
}

// NewPipeline 创建一个 Pipeline。
// engine 可以为 nil，之后通过 engine.UsePipeline(pipeline) 关联。
func NewPipeline(engine *Engine) *Pipeline {
	return &Pipeline{
		engine:     engine,
		restRoutes: make(map[string]Handler),
		cmdRoutes:  make(map[string]*cmdEntry),
		chains:     make(map[string]Handler),
		cmdPath:    "/v1/cmd",
	}
}

// ── 中间件 ──────────────────────────────────────────────────

// Use 注册全局中间件，按注册顺序在所有路由中外层执行。
func (p *Pipeline) Use(mws ...Middleware) {
	p.middleware = append(p.middleware, mws...)
	p.invalidateCache()
}

// UsePrefix 注册命令级中间件，仅当 cmd 或路由路径匹配 prefix 时执行。
// prefix 支持通配符 "*"，匹配任意字符序列。
func (p *Pipeline) UsePrefix(prefix string, mws ...Middleware) {
	p.cmdPrefixes = append(p.cmdPrefixes, prefixMiddleware{
		prefix: prefix,
		mw:     mws,
	})
	p.invalidateCache()
}

// ── RESTful 路由（HTTP only）───────────────────────────────

// GET 注册 GET 路由。
func (p *Pipeline) GET(path string, h Handler) {
	p.restRoutes[path] = h
	if p.engine != nil && p.engine.httpEngine != nil {
		p.engine.httpEngine.GET(path, p.wrapRESTHandler(path, h))
	}
}

// POST 注册 POST 路由。
func (p *Pipeline) POST(path string, h Handler) {
	p.restRoutes[path] = h
	if p.engine != nil && p.engine.httpEngine != nil {
		p.engine.httpEngine.POST(path, p.wrapRESTHandler(path, h))
	}
}

// PUT 注册 PUT 路由。
func (p *Pipeline) PUT(path string, h Handler) {
	p.restRoutes[path] = h
	if p.engine != nil && p.engine.httpEngine != nil {
		p.engine.httpEngine.PUT(path, p.wrapRESTHandler(path, h))
	}
}

// DELETE 注册 DELETE 路由。
func (p *Pipeline) DELETE(path string, h Handler) {
	p.restRoutes[path] = h
	if p.engine != nil && p.engine.httpEngine != nil {
		p.engine.httpEngine.DELETE(path, p.wrapRESTHandler(path, h))
	}
}

// ── Cmd 路由（HTTP Cmd + WebSocket Cmd 共用）───────────────

// Cmd 注册一条 Cmd 路由，HTTP Cmd 和 WebSocket Cmd 共用。
// mws 是该路由的局部中间件，仅在匹配该 cmd 时执行。
func (p *Pipeline) Cmd(name string, h Handler, mws ...Middleware) {
	p.cmdRoutes[name] = &cmdEntry{
		handler:    h,
		middleware: mws,
	}
}

// ── Handler 生成器（给 Transport 调用）──────────────────────

// RESTHandler 返回所有已注册 REST 路由的 gin.HandlerFunc。
// 用于在 Transport 层批量注册。
func (p *Pipeline) RESTHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 已通过 GET/POST/... 直接注册到 gin，此函数作为兜底
		c.Next()
	}
}

// CmdHTTPHandler 返回 HTTP Cmd 入口的 gin.HandlerFunc。
func (p *Pipeline) CmdHTTPHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Cmd  string          `json:"cmd"`
			Body json.RawMessage `json:"body"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, map[string]any{
				"error": "invalid message format",
			})
			return
		}

		session := msg.NewHTTPRequestSession()
		muxCtx := newCmdHTTPContext(c, req.Cmd, req.Body, session)
		if err := p.executeCmd(muxCtx); err != nil {
			logx.Default().Module("mux").Error("cmd execution failed", logx.Fields{"error": err, "cmd": req.Cmd})
		}
	}
}

// CmdWSHandler 返回 WebSocket Cmd 入口的 MessageHandler。
func (p *Pipeline) CmdWSHandler() websocketx.MessageHandler {
	return func(ctx context.Context, session *websocketx.Session, cmdMsg *websocketx.CmdMessage) error {
		wsSession := msg.NewWSSession(session)
		muxCtx := newCmdWSContext(ctx, cmdMsg.Cmd, cmdMsg.Body, wsSession)
		if err := p.executeCmd(muxCtx); err != nil {
			logx.Default().Module("mux").Error("cmd execution failed", logx.Fields{"error": err, "cmd": cmdMsg.Cmd})
		}
		return nil // 错误已通过 ReplyError 回复给客户端
	}
}

// ── 内部方法 ────────────────────────────────────────────────

// wrapRESTHandler 将 Handler 包装成 gin.HandlerFunc，注入中间件链。
func (p *Pipeline) wrapRESTHandler(path string, h Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := msg.NewHTTPRequestSession()
		muxCtx := newRESTContext(c, session)
		if err := p.executeHandler(muxCtx, path, h); err != nil {
			logx.Default().Module("mux").Error("rest handler execution failed", logx.Fields{"error": err, "path": path})
		}
	}
}

// executeHandler 执行单个 Handler（带中间件链），错误返回给上层显式处理。
func (p *Pipeline) executeHandler(ctx *Context, path string, h Handler) error {
	wrapped := p.buildChain(path, h)
	if err := wrapped(ctx); err != nil {
		if replyErr := ctx.ReplyError(http.StatusInternalServerError, err.Error()); replyErr != nil {
			logx.Default().Module("mux").Error("reply error response failed", logx.Fields{"error": replyErr})
		}
		return err
	}
	return nil
}

// executeCmd 执行 Cmd 路由（查找路由表 + 中间件链），错误返回给上层显式处理。
func (p *Pipeline) executeCmd(ctx *Context) error {
	entry, ok := p.cmdRoutes[ctx.Cmd()]
	if !ok {
		if replyErr := ctx.ReplyError(http.StatusNotFound, "cmd not found: "+ctx.Cmd()); replyErr != nil {
			logx.Default().Module("mux").Error("reply not found response failed", logx.Fields{"error": replyErr})
		}
		return fmt.Errorf("mux: cmd not found: %s", ctx.Cmd())
	}

	return p.executeHandler(ctx, ctx.Cmd(), entry.handler)
}

// buildChain 构建完整的中间件链：全局 + 前缀匹配 + 路由级。
// 执行顺序由外到内：全局中间件 → 前缀匹配中间件 → 路由级中间件 → Handler。
// 结果按 key 缓存，中间件变更时通过 invalidateCache 失效。
func (p *Pipeline) buildChain(key string, h Handler) Handler {
	p.chainMu.RLock()
	cached, ok := p.chains[key]
	p.chainMu.RUnlock()
	if ok {
		return cached
	}

	chain := h

	// 路由级中间件（最内层，先包裹）
	if entry, ok := p.cmdRoutes[key]; ok {
		for i := len(entry.middleware) - 1; i >= 0; i-- {
			chain = entry.middleware[i](chain)
		}
	}

	// 前缀匹配中间件（中间层）
	for _, pm := range p.cmdPrefixes {
		if matchPrefix(key, pm.prefix) {
			for i := len(pm.mw) - 1; i >= 0; i-- {
				chain = pm.mw[i](chain)
			}
		}
	}

	// 全局中间件（最外层，最后包裹）
	for i := len(p.middleware) - 1; i >= 0; i-- {
		chain = p.middleware[i](chain)
	}

	p.chainMu.Lock()
	p.chains[key] = chain
	p.chainMu.Unlock()
	return chain
}

// invalidateCache 清空中间件链缓存，在 middleware 变更时调用。
func (p *Pipeline) invalidateCache() {
	p.chainMu.Lock()
	p.chains = make(map[string]Handler)
	p.chainMu.Unlock()
}

// addCleanup 注册一个清理函数，在 Pipeline.Stop() 时调用。
// 用于中间件内部有后台 goroutine 的场景（如 rateLimitPool）。
func (p *Pipeline) addCleanup(fn func()) {
	p.cleanupFns = append(p.cleanupFns, fn)
}

// Stop 执行所有已注册的清理函数。
// 幂等：多次调用安全。
func (p *Pipeline) Stop() {
	for _, fn := range p.cleanupFns {
		fn()
	}
	p.cleanupFns = nil
}

// matchPrefix 检查 key 是否匹配 prefix。
// prefix 中的 "*" 匹配任意字符序列（包括空）。
// 示例："file.*" 匹配 "file.upload"、"file.download"；"*" 匹配所有。
func matchPrefix(key, prefix string) bool {
	if prefix == "*" || prefix == key {
		return true
	}
	if len(prefix) == 0 || len(key) == 0 {
		return false
	}

	// 支持通配符 *，如 "file.*" 匹配 "file.upload", "file.download"
	if prefix[len(prefix)-1] == '*' {
		base := prefix[:len(prefix)-1]
		return len(key) >= len(base) && key[:len(base)] == base
	}
	return prefix == key
}
