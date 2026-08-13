# mux 实现计划

本文件是 mux 框架从设计到实现的完整行动计划。

---

## 阶段概览

| 阶段 | 内容 | 预计文件数 | 预计代码行数 |
|------|------|-----------|-------------|
| Phase 1 | 基础类型：Context + Session + Message | 4 | ~300 |
| Phase 2 | Pipeline 核心 | 4 | ~400 |
| Phase 3 | Transport 层 | 3 | ~350 |
| Phase 4 | Engine 集成 | 3 | ~250 |
| Phase 5 | Facade + 文档 | 3 | ~200 |
| Phase 6 | 集群模块 | 4 | ~400 |
| **合计** | | **~21** | **~1900** |

---

## Phase 1：基础类型

**目标**：建立 Pipeline 运行所需的三个基础类型。

### 1.1 `message.go` — 传输无关的消息格式

```
mux/message.go
```

```go
// Message 是传输层无关的消息格式。
type Message struct {
    Cmd  string
    Head map[string]string
    Body []byte
}
```

Transport 层负责将 HTTP/WS/RPC 请求转换成 `Message`，Pipeline 和 Handler 只处理 `Message`。

---

### 1.2 `session.go` — 统一会话接口

```
mux/session.go
```

```go
// Session 是统一会话接口。
type Session interface {
    ID() string
    UserID() string
    SetState(key string, value any)
    GetState(key string) (any, bool)
}
```

- HTTP：per-request 生命周期（请求结束即销毁）
- WebSocket：连接生命周期（心跳保活、房间管理）

Session 的具体实现在 `internal/msg/` 中，根据传输层类型创建不同的实现。

---

### 1.3 `context.go` — 统一消息上下文

```
mux/context.go
```

```go
// contextMode 标识 Context 的来源。
type contextMode int

const (
    modeREST contextMode = iota  // RESTful 路由（gin 直接调用）
    modeCmdHTTP                  // HTTP Cmd 入口（POST /v1/cmd）
    modeCmdWS                    // WebSocket Cmd（WS 消息）
)

// Protocol 标识传输协议类型。
type Protocol uint8

const (
    ProtocolHTTP Protocol = iota
    ProtocolWS
    ProtocolRPC  // 未来
)

// Context 是统一的消息上下文，屏蔽 HTTP 和 WebSocket 的差异。
type Context struct {
    mode    contextMode
    cmd     string
    body    any                 // 已解析的请求体（Bind 后的结果）
    holder  map[string]any      // 数据传递（替代 gin.Set/Get）
    session Session
    src     context.Context     // 原始 Go context
    writer  ResponseWriter      // 响应写入器
    
    // 底层引用
    rawHTTP *gin.Context
    rawWS   *websocketx.Session
}

// ── 核心 API ──────────────────────────────────────────

func (c *Context) Mode() contextMode       // 内部使用
func (c *Context) Protocol() Protocol      // 传输协议类型
func (c *Context) Cmd() string             // 命令名（REST: 路由 path, Cmd: cmd 字段）
func (c *Context) Set(key string, value any) *Context
func (c *Context) Get(key string) (any, bool)
func (c *Context) MustGet(key string) any
func (c *Context) Bind(target any) error   // 根据 mode 选择数据源反序列化
func (c *Context) Session() Session
func (c *Context) Source() context.Context

// ── 响应 API ──────────────────────────────────────────

func (c *Context) Reply(code int, body any) error
func (c *Context) ReplyOK(body any) error
func (c *Context) ReplyError(code int, msg string) error

// ── 底层访问（需要时直接操作原始对象）───────────────

func (c *Context) Raw() any  // 返回 *gin.Context 或 *websocketx.Session
func (c *Context) Gin() *gin.Context           // 仅 HTTP 模式可用
func (c *Context) WebSocket() *websocketx.Session  // 仅 WS 模式可用
```

**Bind 行为**：

| mode | 数据源 | 说明 |
|------|--------|------|
| `modeREST` | `gin.Context.ShouldBind` | 自动识别 JSON/Form/Query |
| `modeCmdHTTP` | `json.Unmarshal(body, target)` | 从 CmdMessage.Body 解析 |
| `modeCmdWS` | `json.Unmarshal(body, target)` | 从 CmdMessage.Body 解析 |

**Reply 行为**：

| mode | HTTP 响应 | Cmd 响应 |
|------|----------|----------|
| `modeREST` | `gin.Context.JSON(code, response)` | — |
| `modeCmdHTTP` | `{cmd, head: {code}, body}` JSON | `{cmd, head, body}` JSON |
| `modeCmdWS` | — | `CmdMessage{cmd, meta: {code}, body}` |

---

## Phase 2：Pipeline 核心

**目标**：实现中间件链 + RESTful 路由 + Cmd 路由。

### 2.1 `handler.go` — Handler / Middleware 接口

```
mux/handler.go
```

```go
// Handler 是消息处理函数。
type Handler func(ctx *Context) error

// Middleware 是 Pipeline 中间件。
type Middleware func(next Handler) Handler
```

接口极小，只有一行。中间件通过闭包实现链式调用，不需要额外的接口或注册表。

---

### 2.2 `pipeline.go` — Pipeline 主体

```
mux/pipeline.go
```

```go
// Pipeline 管理所有消息的处理流程：中间件链 + RESTful 路由 + Cmd 路由。
type Pipeline struct {
    engine     *Engine
    middleware []Middleware           // 全局中间件链（有序）
    
    restRoutes map[string]Handler     // RESTful 路由：path → Handler
    cmdRoutes  map[string]Handler     // Cmd 路由：cmd → Handler
    
    // HTTP Cmd 入口路径（可配置）
    cmdPath string
}

func NewPipeline(engine *Engine) *Pipeline

// ── 中间件 ──────────────────────────────────────────

func (p *Pipeline) Use(mws ...Middleware)
func (p *Pipeline) UsePrefix(prefix string, mws ...Middleware)  // 按 cmd 前缀匹配

// ── RESTful 路由（HTTP only）─────────────────────────

func (p *Pipeline) GET(path string, h Handler)
func (p *Pipeline) POST(path string, h Handler)
func (p *Pipeline) PUT(path string, h Handler)
func (p *Pipeline) DELETE(path string, h Handler)

// ── Cmd 路由（HTTP Cmd + WebSocket Cmd 共用）─────────

func (p *Pipeline) Cmd(name string, h Handler, mws ...Middleware)

// ── Handler 生成器（给 Transport 调用）────────────────

func (p *Pipeline) RESTHandler() gin.HandlerFunc           // 包装成 gin.HandlerFunc
func (p *Pipeline) CmdHTTPHandler() gin.HandlerFunc       // POST /v1/cmd 的 handler
func (p *Pipeline) CmdWSHandler() MessageHandler           // WS 的 message handler
```

**关键行为**：

1. `pipeline.GET(path, h)` — 注册 REST 路由，如果 `engine` 已设置，立即写入 gin router
2. `pipeline.Cmd(name, h)` — 注册 Cmd 路由到共享路由表
3. `pipeline.Use(mws)` — 追加到全局中间件链
4. `pipeline.UsePrefix("file.*", mws)` — 注册命令级中间件（按前缀匹配）

**UsePipeline 挂载逻辑**：

```go
func (e *Engine) UsePipeline(p *Pipeline) {
    p.engine = e
    
    // 1. REST 路由已通过 GET/POST/... 实时注册到 gin
    //    无需额外操作
    
    // 2. 注册 HTTP Cmd 入口
    if e.httpTransport != nil {
        e.httpTransport.engine.POST(p.cmdPath, p.CmdHTTPHandler())
    }
    
    // 3. 注册 WebSocket Cmd 入口
    if e.wsTransport != nil {
        e.wsTransport.router.Use(RecoverMiddleware)
        e.wsTransport.router.Cmd("*", p.CmdWSHandler())  // 通配 Cmd
    }
}
```

---

### 2.3 `middleware.go` — 内置中间件

```
mux/middleware.go
```

```go
// 全局中间件（REST + Cmd 共用）

func LogMiddleware(logger *logx.Logger) Middleware
func AuthMiddleware(opts ...AuthOption) Middleware
func RateLimitMiddleware(rate, burst int) Middleware
func RecoverMiddleware() Middleware
func SanitizeMiddleware(fields ...string) Middleware  // 出站字段过滤

// 协议限制中间件
func OnlyWS() Middleware     // 仅 WebSocket
func OnlyHTTP() Middleware   // 仅 HTTP Cmd
```

内置中间件的实现委托给 `internal/httpx/middleware/`：

```go
func LogMiddleware(logger *logx.Logger) Middleware {
    return func(next Handler) Handler {
        return func(ctx *Context) error {
            // 记录请求开始
            start := time.Now()
            err := next(ctx)
            // 记录请求结束
            duration := time.Since(start)
            logger.Info(...)
            return err
        }
    }
}
```

---

### 2.4 `pipeline_test.go` — Pipeline 测试

```
mux/pipeline_test.go
```

覆盖场景：
1. REST 路由匹配
2. Cmd 路由匹配
3. 全局中间件链执行
4. 命令级中间件（UsePrefix）
5. Handler 同时注册在 REST 和 Cmd
6. Context.Bind 在不同 mode 下的行为
7. Context.Reply 在不同 mode 下的输出

---

## Phase 3：Transport 层

**目标**：将 HTTP 和 WebSocket 封装成 Transport 接口。

### 3.1 `transport.go` — Transport 接口

```
mux/transport.go
```

```go
// ResponseWriter 是响应写入抽象。
type ResponseWriter interface {
    Write(ctx context.Context, msg *Message) error
}

// Transport 是网络传输层抽象。
type Transport interface {
    Serve(ctx context.Context) error
    Close(ctx context.Context) error
}
```

**注意**：Engine 持 Transport 接口，但接口只暴露 `Serve` 和 `Close`。`Write` 和路由挂载由具体的 transport 结构体提供（不通过接口），因为 Engine 需要直接操作底层 router。

---

### 3.2 `internal/transport_http.go` — HTTPTransport

```
mux/internal/transport_http.go
```

```go
type HTTPTransport struct {
    engine *gin.Engine
    opts   httpOptions
}

func newHTTPTransport(opts httpOptions) *HTTPTransport

func (t *HTTPTransport) Serve(ctx context.Context) error {
    return t.engine.Run(t.opts.addr)
}

func (t *HTTPTransport) Close(ctx context.Context) error {
    return t.engine.Shutdown(ctx)
}

func (t *HTTPTransport) POST(path string, handler gin.HandlerFunc) {
    t.engine.POST(path, handler)
}

func (t *HTTPTransport) GET(path string, handler gin.HandlerFunc) {
    t.engine.GET(path, handler)
}

// ... PUT, DELETE, etc.
```

HTTPTransport 暴露 gin router 操作，Engine 通过它注册 REST 路由和 HTTP Cmd 入口。

---

### 3.3 `internal/transport_ws.go` — WSTransport

```
mux/internal/transport_ws.go
```

```go
type WSTransport struct {
    hub    *websocketx.Hub
    router *websocketx.Router
    opts   wsOptions
}

func newWSTransport(opts wsOptions) *WSTransport

func (t *WSTransport) Serve(ctx context.Context) error {
    t.hub.Run(ctx)
    return nil
}

func (t *WSTransport) Close(ctx context.Context) error {
    return t.hub.Shutdown(ctx)
}

func (t *WSTransport) Cmd(pattern string, handler websocketx.MessageHandler, mws ...websocketx.Middleware) {
    t.router.Cmd(pattern, handler, mws...)
}
```

WSTransport 暴露 websocketx router 操作，Engine 通过它注册 WS Cmd 入口。

---

## Phase 4：Engine 集成

**目标**：将 Pipeline + Transport + Session + Cluster 组装成统一的 Engine。

### 4.1 `engine.go` 重构

```
mux/engine.go
```

```go
type Engine struct {
    ctx      context.Context
    cancel   context.CancelFunc
    opts     *engineOptions
    
    // 传输层
    httpTransport *HTTPTransport
    wsTransport   *WSTransport
    
    // 核心
    pipeline *Pipeline
    sessions SessionManager
    cluster  *ClusterManager
    
    // 生命周期
    wg      sync.WaitGroup
    started bool
}

func New(opts ...EngineOption) *Engine
func (e *Engine) HTTP() *gin.Engine                    // 底层 gin 访问
func (e *Engine) WS() *websocketx.Router              // 底层 WS router 访问
func (e *Engine) WSHub() *websocketx.Hub              // 底层 WS hub 访问
func (e *Engine) UsePipeline(p *Pipeline)
func (e *Engine) Run(addr string) error
func (e *Engine) Serve(listener net.Listener) error
func (e *Engine) Shutdown(ctx context.Context) error
```

**使用流程**：

```go
engine := mux.New(
    mux.WithReadTimeout(10 * time.Second),
    mux.WithWebSocketPath("/ws"),
)

pipeline := mux.NewPipeline(engine)
pipeline.Use(mux.Log(), mux.Auth())
pipeline.GET("/api/users", listUsers)
pipeline.Cmd("user.list", listUsers)

engine.UsePipeline(pipeline)
engine.Run(":8080")
```

---

### 4.2 `options.go` — 基础选项

```
mux/options.go
```

```go
type EngineOption func(*engineOptions)

type engineOptions struct {
    readTimeout  time.Duration
    writeTimeout time.Duration
    idleTimeout  time.Duration
    maxWSConn    int
    wsPath       string
    cmdPath      string      // HTTP Cmd 入口路径，默认 "/v1/cmd"
}

func WithReadTimeout(d time.Duration) EngineOption
func WithWriteTimeout(d time.Duration) EngineOption
func WithIdleTimeout(d time.Duration) EngineOption
func WithMaxWebSocketConn(max int) EngineOption
func WithWebSocketPath(path string) EngineOption
func WithCmdPath(path string) EngineOption    // 🆕
```

### 4.3 `options_http.go` — HTTP 选项

```
mux/options_http.go
```

```go
type httpOptions struct {
    addr         string
    readTimeout  time.Duration
    writeTimeout time.Duration
    idleTimeout  time.Duration
}

func WithHTTPAddr(addr string) EngineOption
func WithHTTPReadTimeout(d time.Duration) EngineOption
```

### 4.4 `options_ws.go` — WebSocket 选项

```
mux/options_ws.go
```

```go
type wsOptions struct {
    path           string
    maxConn        int
    heartbeatInterval time.Duration
    heartbeatPongWait time.Duration
    maxMessageSize int64
}

func WithWebSocketPath(path string) EngineOption
func WithMaxWebSocketConn(max int) EngineOption
func WithWebSocketHeartbeat(interval, pongWait time.Duration) EngineOption
func WithMaxMessageSize(size int64) EngineOption
```

### 4.5 `options_cluster.go` — 集群选项

```
mux/options_cluster.go
```

```go
type clusterOptions struct {
    enabled       bool
    role          Role
    redisAddr     string
    heartbeatInterval time.Duration
    nodeTTL       time.Duration
    serviceName   string
    group         string
}

func WithClusterEnabled(enabled bool) EngineOption
func WithClusterRole(role Role) EngineOption
func WithClusterRedis(addr string) EngineOption
func WithClusterHeartbeatInterval(d time.Duration) EngineOption
func WithClusterNodeTTL(d time.Duration) EngineOption
func WithClusterServiceName(name string) EngineOption
func WithClusterGroup(group string) EngineOption
```

---

## Phase 5：Facade + 文档

**目标**：对外暴露统一的公共 API，完成用户文档。

### 5.1 `facade.go` — 公共 API 门面

```
mux/facade.go
```

**原则**：只做类型别名和函数转发，不包含任何业务逻辑。

```go
package mux

// ── Pipeline 相关 ─────────────────────────────────────

type Handler = internalHandler.Handler
type Middleware = internalHandler.Middleware

// ── Context 相关 ─────────────────────────────────────

type Context = internalContext.Context
type Protocol = internalContext.Protocol

const (
    ProtocolHTTP = internalContext.ProtocolHTTP
    ProtocolWS   = internalContext.ProtocolWS
)

// ── Message 相关 ─────────────────────────────────────

type Message = internalMessage.Message

// ── Session 相关 ─────────────────────────────────────

type Session = internalSession.Session

// ── 内置中间件 ───────────────────────────────────────

var Log = internalMiddleware.Log
var Auth = internalMiddleware.Auth
var RateLimit = internalMiddleware.RateLimit
var Recover = internalMiddleware.Recover
var Sanitize = internalMiddleware.Sanitize

// ── 集群角色 ─────────────────────────────────────────

type Role = internalCluster.Role

const (
    RoleMix = internalCluster.RoleMix
    RoleAP  = internalCluster.RoleAP
    RoleBU  = internalCluster.RoleBU
)
```

---

### 5.2 `doc.go` — 包文档

```
mux/doc.go
```

包级文档注释，包含：
- 框架定位
- 核心类型列表
- 快速开始代码示例（5 行以内）
- 链接到 README.md

---

### 5.3 `README.md` — 用户文档

已完成，包含：
- 框架定位
- 核心设计（Transport / Context / Pipeline / Session / Cluster）
- 项目结构
- 快速开始
- 与 IMS 对比
- 设计讨论记录

---

## Phase 6：集群模块

**目标**：实现三层集群架构（MIX → AP + BU）。

### 6.1 `internal/cluster/node.go` — 节点定义

```
mux/internal/cluster/node.go
```

```go
type Role uint8

const (
    RoleMix Role = iota  // 单机混合
    RoleAP               // 接入层
    RoleBU               // 业务层
)

type ClusterNode struct {
    Tag         string
    ServiceName string
    Group       string
    Role        Role
    Env         string
    IPPort      string
    ConnectUrl  string
    Ts          int64
}
```

---

### 6.2 `internal/cluster/discovery_redis.go` — Redis 节点发现

```
mux/internal/cluster/discovery_redis.go
```

```go
type RedisDiscovery struct {
    rdb    *redis.Client
    prefix string
}

func NewRedisDiscovery(rdb *redis.Client, prefix string) *RedisDiscovery

// Register 注册/更新当前节点
func (d *RedisDiscovery) Register(ctx context.Context, node ClusterNode) error

// Unregister 注销节点
func (d *RedisDiscovery) Unregister(ctx context.Context, tag string) error

// PullNodes 拉取指定 group + role 的存活节点
func (d *RedisDiscovery) PullNodes(ctx context.Context, group string, role Role) ([]ClusterNode, error)

// Watch 监听节点变化
func (d *RedisDiscovery) Watch(ctx context.Context, group string, role Role) <-chan []ClusterNode
```

Redis key 设计：
```
cluster:nodes:{role}:{group}        → ZSet(nodeTag → timestamp)
cluster:nodes:{role}:{group}:{tag}  → String(JSON serialized ClusterNode)
```

---

### 6.3 `internal/cluster/manager.go` — 集群管理器

```
mux/internal/cluster/manager.go
```

```go
type ClusterManager struct {
    ctx       context.Context
    self      ClusterNode
    discovery Discovery
    nodes     *sync.Map              // tag → ClusterNode
    forward   *Forwarder
    ticker    *time.Ticker
}

func NewClusterManager(ctx context.Context, self ClusterNode, discovery Discovery) *ClusterManager

func (m *ClusterManager) Start()
func (m *ClusterManager) Stop()

// GetNodes 获取同组同角色的存活节点
func (m *ClusterManager) GetNodes(group string, role Role) []ClusterNode

// Forward 将消息转发到目标节点
func (m *ClusterManager) Forward(ctx context.Context, target *ClusterNode, msg *Message) error
```

---

### 6.4 `internal/cluster/forward.go` — 转发器

```
mux/internal/cluster/forward.go
```

```go
type Forwarder struct {
    client *http.Client
}

func NewForwarder() *Forwarder

func (f *Forwarder) Forward(ctx context.Context, target *ClusterNode, msg *Message) error
```

使用 `internal/httpx/client` 的 HTTP Client Builder 向目标节点发送请求：

```go
func (f *Forwarder) Forward(ctx context.Context, target *ClusterNode, msg *Message) error {
    body, _ := json.Marshal(msg)
    _, err := client.NewRequest().
        URL(target.ConnectUrl + "/v1/cmd").
        Method("POST").
        Body(body).
        Do(ctx)
    return err
}
```

---

## 实现顺序

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

  基础类型    Pipeline    Transport    Engine      Facade     集群
   (~300行)    (~400行)    (~350行)     (~250行)    (~200行)    (~400行)
```

每个 Phase 完成后运行测试，确认通过后再进入下一个 Phase。

---

## 关键决策记录

| # | 决策 | 时间 |
|---|------|------|
| 1 | 采用路径 B：httpx + websocketx 作为 mux 的 internal | 2026-08-14 |
| 2 | Pipeline 是核心架构，不是可选层 | 2026-08-14 |
| 3 | 统一 Context：屏蔽 HTTP/WS 差异 | 2026-08-14 |
| 4 | 自定义 Transport 接口（先内部抽象，不对外暴露） | 2026-08-14 |
| 5 | 集群三层架构：MIX → AP+BU → 完整分布式 | 2026-08-14 |
| 6 | 选项按域拆分 | 2026-08-14 |
| 7 | Pipeline 持 Engine 引用，注册路由时实时写入 | 2026-08-14 |
| 8 | UsePipeline 一行挂载，自动注册三个入口 | 2026-08-14 |
| 9 | pipeline.Cmd() 注册，HTTP Cmd + WS Cmd 共用路由表 | 2026-08-14 |
| 10 | facade.go 只做类型别名和函数转发，不包含业务逻辑 | 2026-08-14 |
