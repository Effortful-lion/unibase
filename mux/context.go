package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Effortful-lion/unibase/mux/internal/msg"
	"github.com/Effortful-lion/unibase/mux/internal/websocketx"
	"github.com/gin-gonic/gin"
)

// contextMode 标识 Context 的来源。
type contextMode uint8

const (
	modeREST    contextMode = iota // RESTful 路由（gin 直接调用）
	modeCmdHTTP                    // HTTP Cmd 入口（POST /v1/cmd）
	modeCmdWS                      // WebSocket Cmd（WS 消息）
)

// Protocol 标识传输协议类型。
type Protocol uint8

const (
	ProtocolHTTP Protocol = iota
	ProtocolWS
)

// Context 是统一的消息上下文，屏蔽 HTTP 和 WebSocket 的差异。
type Context struct {
	mode      contextMode
	cmd       string
	body      []byte
	holder    map[string]any
	session   Session
	src       context.Context
	requestID string

	rawHTTP *gin.Context
	rawWS   *msg.WSSession
}

// newRESTContext 从 gin.Context 创建 REST 模式的 Context。
func newRESTContext(c *gin.Context, session Session) *Context {
	ctx, cancel := context.WithCancel(c.Request.Context())
	_ = cancel // 生命周期由 Go HTTP server 管理
	return &Context{
		mode:      modeREST,
		cmd:       c.FullPath(),
		holder:    make(map[string]any),
		session:   session,
		src:       ctx,
		requestID: session.ID(),
		rawHTTP:   c,
	}
}

// newCmdHTTPContext 从 gin.Context 创建 HTTP Cmd 模式的 Context。
func newCmdHTTPContext(c *gin.Context, cmd string, body []byte, session Session) *Context {
	ctx, cancel := context.WithCancel(c.Request.Context())
	_ = cancel
	return &Context{
		mode:      modeCmdHTTP,
		cmd:       cmd,
		body:      body,
		holder:    make(map[string]any),
		session:   session,
		src:       ctx,
		requestID: session.ID(),
		rawHTTP:   c,
	}
}

// newCmdWSContext 从 WebSocket 消息创建 WS Cmd 模式的 Context。
func newCmdWSContext(src context.Context, cmd string, body []byte, session *msg.WSSession) *Context {
	return &Context{
		mode:      modeCmdWS,
		cmd:       cmd,
		body:      body,
		holder:    make(map[string]any),
		session:   session,
		src:       src,
		requestID: session.ID(),
		rawWS:     session,
	}
}

// ── 元信息 API ──────────────────────────────────────────────

// Mode 返回内部模式标识（仅供框架内部使用）。
func (c *Context) Mode() contextMode {
	return c.mode
}

// Protocol 返回传输协议类型。
func (c *Context) Protocol() Protocol {
	switch c.mode {
	case modeCmdWS:
		return ProtocolWS
	default:
		return ProtocolHTTP
	}
}

// Cmd 返回当前处理的命令名或路由路径。
// REST 模式返回路由路径（如 "/api/users/:id"），Cmd 模式返回 cmd 字段（如 "user.create"）。
func (c *Context) Cmd() string {
	return c.cmd
}

// RequestID 返回当前请求的唯一标识符。
// HTTP: 每次请求生成唯一 ID；WebSocket: 使用 Session ID。
func (c *Context) RequestID() string {
	return c.requestID
}

// ── 数据传递 API ───────────────────────────────────────────

// Set 在 Context 中存储键值对，用于中间件和 Handler 之间传递数据。
func (c *Context) Set(key string, value any) *Context {
	c.holder[key] = value
	return c
}

// Get 从 Context 中读取键值对。
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.holder[key]
	return v, ok
}

// MustGet 从 Context 中读取键值对，不存在时 panic。
func (c *Context) MustGet(key string) any {
	v, ok := c.holder[key]
	if !ok {
		panic("mux: missing key in context: " + key)
	}
	return v
}

// ── 请求体绑定 ─────────────────────────────────────────────

// Bind 根据当前模式将请求体反序列化到目标结构体。
//
// REST 模式：使用 gin.Context.ShouldBind，自动识别 JSON/Form/Query/Path 参数。
// Cmd 模式：使用 json.Unmarshal 解析 Message.Body。
func (c *Context) Bind(target any) error {
	switch c.mode {
	case modeREST:
		return c.rawHTTP.ShouldBind(target)
	case modeCmdHTTP, modeCmdWS:
		if len(c.body) == 0 {
			return fmt.Errorf("mux: empty request body")
		}
		return json.Unmarshal(c.body, target)
	default:
		return nil
	}
}

// ── 会话 API ───────────────────────────────────────────────

// Session 返回当前会话。
func (c *Context) Session() Session {
	return c.session
}

// ── 响应 API ───────────────────────────────────────────────

// Reply 以指定状态码和内容写入响应。
func (c *Context) Reply(code int, body any) error {
	switch c.mode {
	case modeREST:
		err := c.replyREST(code, body)
		c.rawHTTP = nil
		return err
	case modeCmdHTTP:
		err := c.replyCmdHTTP(code, body)
		c.rawHTTP = nil
		return err
	case modeCmdWS:
		err := c.replyCmdWS(code, body)
		c.rawWS = nil
		return err
	default:
		return fmt.Errorf("mux: unsupported context mode %d", c.mode)
	}
}

// ReplyOK 写入成功响应。
func (c *Context) ReplyOK(body any) error {
	return c.Reply(http.StatusOK, body)
}

// ReplyError 写入错误响应。
func (c *Context) ReplyError(code int, msg string) error {
	return c.Reply(code, map[string]any{"error": msg})
}

// replyREST 写入 REST 响应（gin JSON）。
func (c *Context) replyREST(code int, body any) error {
	c.rawHTTP.AbortWithStatusJSON(code, c.sanitize(body))
	return nil
}

// replyCmdHTTP 写入 HTTP Cmd 响应（CmdMessage JSON）。
func (c *Context) replyCmdHTTP(code int, body any) error {
	resp := map[string]any{
		"cmd":  c.cmd,
		"head": map[string]any{"code": code},
	}
	if body != nil {
		resp["body"] = c.sanitize(body)
	}
	c.rawHTTP.AbortWithStatusJSON(code, resp)
	return nil
}

// replyCmdWS 写入 WebSocket Cmd 响应（CmdMessage）。
func (c *Context) replyCmdWS(code int, body any) error {
	resp := &websocketx.CmdMessage{
		Cmd: c.cmd,
		Meta: map[string]interface{}{
			"code":     http.StatusText(code),
			"httpCode": code,
		},
	}
	if body != nil {
		b, err := json.Marshal(c.sanitize(body))
		if err != nil {
			return err
		}
		resp.Body = b
	}
	return c.rawWS.Conn().Write(c.src, resp)
}

// sanitize 根据 holder 中的过滤标记清理响应体。
func (c *Context) sanitize(body any) any {
	if body == nil {
		return body
	}

	// 收集需要过滤的字段
	var fields []string
	for key := range c.holder {
		if len(key) > 11 && key[:11] == "__sanitize_" {
			fields = append(fields, key[11:])
		}
	}
	if len(fields) == 0 {
		return body
	}

	// 仅对 map[string]any 类型执行过滤
	m, ok := body.(map[string]any)
	if !ok {
		return body
	}
	return SanitizeMap(m, fields...)
}

// ── 底层访问 ───────────────────────────────────────────────

// Source 返回原始 Go context，用于超时控制和链路追踪。
func (c *Context) Source() context.Context {
	return c.src
}

// Raw 返回底层传输对象（*gin.Context 或 *msg.WSSession）。
func (c *Context) Raw() any {
	if c.rawHTTP != nil {
		return c.rawHTTP
	}
	if c.rawWS != nil {
		return c.rawWS
	}
	return nil
}

// Gin 返回底层 gin.Context，仅在 HTTP 模式下可用。
func (c *Context) Gin() *gin.Context {
	return c.rawHTTP
}

// WebSocket 返回底层 WSSession，仅在 WebSocket 模式下可用。
func (c *Context) WebSocket() *msg.WSSession {
	return c.rawWS
}
