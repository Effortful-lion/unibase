package websocketx

import (
	"context"
	"fmt"
	"sync"

	"github.com/Effortful-lion/unibase/logx"
)

// MessageHandler 是路由分发最终执行的函数签名。
type MessageHandler func(ctx context.Context, session *Session, msg *CmdMessage) error

// MiddlewareFunc 是中间件函数签名。
// next 是管道中的下一个处理环节。
type MiddlewareFunc func(ctx context.Context, session *Session, msg *CmdMessage, next MessageHandler) error

// Router 按 Cmd 分发消息到对应的 Handler，支持中间件链。
type Router struct {
	mu         sync.RWMutex
	routes     map[string]*routeEntry
	middleware []MiddlewareFunc
}

type routeEntry struct {
	handler    MessageHandler
	middleware []MiddlewareFunc
}

// NewRouter 创建一个 Router。
func NewRouter() *Router {
	return &Router{
		routes: make(map[string]*routeEntry),
	}
}

// Cmd 注册一条路由。
// handler 接收原始 CmdMessage，返回 error；响应写入由 handler 自行调用 session.Conn().Write 完成。
// mws 是该路由的局部中间件，仅在匹配该 cmd 时执行。
//
// 使用示例：
//
//	// 无中间件
//	router.Cmd("ping", handler.Ping)
//
//	// 带路由级中间件
//	router.Cmd("file.upload", handler.FileUpload, authMiddleware, rateLimitMiddleware)
func (r *Router) Cmd(cmd string, handler MessageHandler, mws ...MiddlewareFunc) *Router {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[cmd] = &routeEntry{
		handler:    handler,
		middleware: mws,
	}
	return r
}

// Use 注册全局中间件，按注册顺序执行。
// 全局中间件在每个路由级中间件外层执行。
func (r *Router) Use(middleware MiddlewareFunc) *Router {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, middleware)
	return r
}

// RouteInfo 描述一条路由信息，用于日志输出。
type RouteInfo struct {
	Cmd       string
	Handler   string
	IsDynamic bool // 是否包含通配符或参数
}

// Routes 返回当前 Router 的所有路由信息（快照，线程安全）。
func (r *Router) Routes() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make([]RouteInfo, 0, len(r.routes))
	for cmd, entry := range r.routes {
		routes = append(routes, RouteInfo{
			Cmd:       cmd,
			Handler:   funcName(entry.handler),
			IsDynamic: isDynamicCmd(cmd),
		})
	}
	return routes
}

func funcName(fn any) string {
	if fn == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", fn)
}

func isDynamicCmd(cmd string) bool {
	return cmd == "*" || len(cmd) > 0 && (cmd[0] == '*' || cmd[len(cmd)-1] == '*')
}

// 如果 cmd 未注册，自动发送错误响应并返回 nil（读循环继续）。
// 如果中间件或 Handler 返回 error，发送错误响应并返回 error（读循环终止）。
func (r *Router) Handle(ctx context.Context, session *Session, msg *CmdMessage) error {
	r.mu.RLock()
	entry, ok := r.routes[msg.Cmd]
	globalMw := make([]MiddlewareFunc, len(r.middleware))
	copy(globalMw, r.middleware)
	r.mu.RUnlock()

	if !ok {
		return session.Conn().Write(ctx, &CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10400", "message": "unknown cmd"},
		})
	}

	// 构建最终处理函数
	finalHandler := func(ctx context.Context, session *Session, msg *CmdMessage) error {
		err := entry.handler(ctx, session, msg)
		if err != nil {
			if replyErr := Reply(ctx, session.Conn(), msg, "10500", err); replyErr != nil {
				logx.Default().Module("router").Error("reply handler error failed", logx.Fields{"error": replyErr, "cmd": msg.Cmd})
			}
		}
		return nil
	}

	// 先包裹路由级中间件（内层）
	routeMw := entry.middleware
	for i := len(routeMw) - 1; i >= 0; i-- {
		m := routeMw[i]
		next := finalHandler
		finalHandler = func(ctx context.Context, session *Session, msg *CmdMessage) error {
			return m(ctx, session, msg, next)
		}
	}

	// 再包裹全局中间件（外层）
	for i := len(globalMw) - 1; i >= 0; i-- {
		m := globalMw[i]
		next := finalHandler
		finalHandler = func(ctx context.Context, session *Session, msg *CmdMessage) error {
			return m(ctx, session, msg, next)
		}
	}

	return finalHandler(ctx, session, msg)
}

// RecoverMiddleware 是全局 Panic 兜底中间件。
// 放在中间件链最外层，防止 Handler panic 导致 goroutine 崩溃。
var RecoverMiddleware MiddlewareFunc = func(ctx context.Context, session *Session, msg *CmdMessage, next MessageHandler) error {
	defer func() {
		if r := recover(); r != nil {
			if replyErr := Reply(ctx, session.Conn(), msg, "10500", fmt.Errorf("panic: %v", r)); replyErr != nil {
				logx.Default().Module("router").Error("reply panic failed", logx.Fields{"error": replyErr, "cmd": msg.Cmd})
			}
		}
	}()
	return next(ctx, session, msg)
}
