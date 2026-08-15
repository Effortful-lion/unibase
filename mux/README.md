# mux — 三聚合 Web 框架

> HTTP + WebSocket（RPC planned）

---

## 定位

mux 是 unibase 的核心框架模块，聚合 HTTP（基于 Gin）和 WebSocket（基于 gorilla/websocket）两种协议，提供统一的 Engine 入口。RPC 传输层 planned，尚未实现。

用户只需 `import "github.com/Effortful-lion/unibase/mux"` 一个包，即可构建完整的网络服务。

---

## 核心设计

### 1. 分层架构

```
Engine（生命周期）
  ├── Transport 层（HTTP + WebSocket）
  ├── Pipeline 层（中间件链 + 路由）
  ├── Cluster 层（可选，Redis 节点发现）
  └── Handler 层（业务逻辑）
```

### 2. 统一消息上下文（Context）

所有网络请求（HTTP、WebSocket）最终都包装成统一的 `Context`：

```go
type Context struct {
    mode      contextMode  // REST / CmdHTTP / CmdWS
    cmd       string       // 路由路径或 cmd 名
    body      []byte       // 原始请求体
    holder    map[string]any
    session   Session
    src       context.Context
    requestID string
    rawHTTP   *gin.Context   // HTTP 模式下有效，Reply 后置 nil
    rawWS     *msg.WSSession // WebSocket 模式下有效，Reply 后置 nil
}
```

**生命周期说明**：
- `rawHTTP` 和 `rawWS` 在 `Reply()` 完成后置 `nil`，避免持有底层连接引用导致内存泄漏
- WebSocket 模式下 `rawHTTP` 始终为 `nil`，HTTP 模式下 `rawWS` 始终为 `nil`
- Handler 返回后，Context 应被视为不可用

Context 提供统一的 API：
- `Set/Get/MustGet` — 数据传递
- `Bind` — 请求体反序列化（REST: gin.ShouldBind / Cmd: json.Unmarshal）
- `Session()` — 获取会话信息
- `Reply/ReplyOK/ReplyError` — 写入响应（自动根据模式选择格式，完成后断开底层引用）

### 3. Pipeline 核心

Pipeline 管理所有消息的处理流程：中间件链 + RESTful 路由 + Cmd 路由。

**三种入口，两条路径，一个 cmd 路由表**：

```
                    ┌──────────────────────────┐
                    │      Pipeline             │
                    │  ┌────────────────────┐  │
                    │  │ 全局中间件链         │  │
                    │  │ Auth → Log → ...   │  │
                    │  └─────────┬──────────┘  │
                    │            │              │
                    │  ┌─────────┴──────────┐  │
                    │  │                    │  │
                    │  ▼                    ▼  │
                    │ ┌──────────┐  ┌─────────────┐
                    │ │ REST 路由 │  │ Cmd 路由表  │
                    │ │ (gin)    │  │ (共享)      │
                    │ │          │  │             │
                    │ │GET /users│  │user.list    │
                    │ │POST /users│  │user.create  │
                    │ │          │  │chat.message │
                    │ └──────────┘  └──────┬──────┘
                    │                      │
                    └──────────┬───────────┘
                               │
                ┌──────────────┼──────────────┐
                ▼              ▼              ▼
         ┌─────────────┐ ┌──────────┐ ┌──────────┐
         │ HTTP REST   │ │HTTP Cmd  │ │ WS Cmd   │
         │             │ │          │ │          │
         │GET /users   │ │POST      │ │WS 消息   │
         │POST /users  │ │  /v1/cmd │ │          │
         │             │ │          │ │          │
         │→ REST Handler│ │→ cmdHTTP │ │→ cmdWS  │
         │             │ │  Handler │ │  Handler │
         │             │ │          │ │          │
         │             │ │ 同一份   │ │ 同一份   │
         │             │ │ Cmd 路由 │ │ Cmd 路由 │
         │             │ │ 表       │ │ 表       │
         └─────────────┘ └──────────┘ └──────────┘
```

**关键设计**：

| 维度 | 说明 |
|------|------|
| **入口分离** | HTTP REST、HTTP Cmd、WebSocket Cmd 是三个不同的入口 |
| **Cmd 路由表共享** | `pipeline.Cmd("user.create", handler)` 注册一次，HTTP Cmd 和 WebSocket Cmd 都能命中 |
| **REST 独立** | RESTful 路由走 gin router map，不经过 Cmd 路由表 |
| **WebSocket 只有 Cmd** | WebSocket 不支持 RESTful 模式 |
| **中间件共享** | REST 和 Cmd 共享同一套全局中间件链（Auth 同时覆盖 HTTP 和 WS） |
| **前缀中间件** | `pipeline.UsePrefix("file.*", mw)` 按 cmd 前缀匹配中间件 |

```go
pipeline := mux.NewPipeline(engine)

// 共享中间件（所有入口都走这套，Auth 自动适配 HTTP 和 WS）
pipeline.Use(
    mux.Log(logger),
    mux.Recover,
    mux.Auth(mux.WithJWTSecret(secret)),  // HTTP + WS 统一认证
)

// RESTful 路由（HTTP only，走 gin router map）
pipeline.GET("/api/users", listUsers)
pipeline.POST("/api/users", createUser)

// Cmd 路由（HTTP Cmd + WebSocket Cmd 共用）
// 注册一次，两个入口都能命中
pipeline.Cmd("user.list", listUsers)        // 与 GET /api/users 复用
pipeline.Cmd("user.create", createUser)     // 与 POST /api/users 复用
pipeline.Cmd("chat.message", handleChat)    // WebSocket 专属

// 命令级中间件（仅 file.* 前缀的 cmd 执行）
pipeline.UsePrefix("file.*", mux.RateLimit(10, 20))
```

**挂载方式**：

```go
// 一行挂载：自动注册三个入口
//   - REST 路由 → gin router map（已在 GET/POST 时实时注册）
//   - HTTP Cmd 入口 → POST /v1/cmd → cmdHTTPHandler
//   - WebSocket Cmd → ws router → cmdWSHandler
engine.UsePipeline(pipeline)
```

### 4. 会话管理（Session）

统一的 `Session` 接口：

```go
type Session interface {
    ID() string
    UserID() string
    SetState(key string, value any)
    GetState(key string) (any, bool)
}
```

- HTTP：per-request 生命周期（请求结束即销毁）
- WebSocket：连接生命周期（心跳保活、房间管理）

### 5. 安全特性

| 特性 | 说明 |
|------|------|
| **WebSocket CheckOrigin** | 默认仅允许相同来源，防止 CSRF；可通过 `WithCheckOrigin` 配置允许列表 |
| **JWT 认证** | `mux.Auth(mux.WithJWTSecret(secret))` 中间件，统一处理 HTTP 和 WebSocket 认证。从 Authorization header（HTTP）或 Session Meta（WS）提取 token，验证后通过 `Session.UserID()` 提供 userID |
| **CORS** | `WithCORS(opts...)` 配置跨域规则，自动应用到 HTTP 路由 |
| **Handler 超时** | `websocketx.WithHandlerTimeout(d)` 限制单条消息处理时间，防止慢操作阻塞连接 |
| **消息大小限制** | `WithMaxMessageSize` 限制单条 WebSocket 消息大小 |
| **连接数限制** | `WithMaxWebSocketConn` 限制最大并发连接数 |
| **消息速率限制** | `websocketx.WithMaxMessageRate` 限制单连接 QPS（Hub 级别配置） |

### 6. 错误处理

- **统一错误日志**：cluster heartbeat、Redis 操作、handler 错误均已记录日志
- **WebSocket 错误响应**：handler 返回 error 时，向客户端发送错误响应后断开连接
- **Panic 恢复**：全局 RecoverMiddleware 捕获 panic，记录错误日志并返回 500
- **错误传播**：能返回 error 的函数均返回 error，不静默吞掉
- **Reply 后断开引用**：`Context.Reply*` 完成后将 `rawHTTP`/`rawWS` 置 nil，避免持有底层连接导致内存泄漏

### 7. 集群三层架构

```go
engine := mux.New(
    mux.WithClusterEnabled(true),
    mux.WithClusterRole(mux.RoleAP),
    mux.WithClusterRedis("localhost:6379"),
    mux.WithClusterServiceName("myapp"),
    mux.WithClusterGroup("default"),
    mux.WithClusterAdvertiseAddr("127.0.0.1:8080"), // 节点对外通告地址，用于集群转发
)
```

| 角色 | 说明 |
|------|------|
| `RoleMix` | 单机混合模式（默认，无集群依赖） |
| `RoleAP` | 接入层（HTTP/WS 接入 + 认证 + 转发） |
| `RoleBU` | 业务层（只执行业务逻辑，不接受客户端直连） |

集群发现基于 Redis ZSet 实现，不依赖外部服务发现框架。

**集群生命周期**：Engine 自动管理 ClusterManager 生命周期（`initCluster` → `Serve` 启动 → `Shutdown` 停止），用户无需手动管理。

**Redis 超时配置**：Redis 客户端已配置 `DialTimeout/ReadTimeout/WriteTimeout/PoolTimeout`，Redis 故障时不会无限阻塞。

节点发现接口可替换：

```go
type Discovery interface {
    Register(ctx context.Context, node ClusterNode) error
    Unregister(ctx context.Context, node ClusterNode) error
    PullNodes(ctx context.Context, group string, role Role, ttl time.Duration) ([]ClusterNode, error)
    Watch(ctx context.Context, group string, role Role, ttl time.Duration) <-chan []ClusterNode
}
```

**Redis 集成测试**：设置环境变量 `RUN_REDIS_INTEGRATION=1` 运行 Redis 集成测试，验证注册/注销/拉取/Watch 完整流程。

### 8. 优雅关闭

```go
// 方式一：Run 阻塞直到收到 SIGTERM/SIGINT
engine.Run(":8080")

// 方式二：手动控制生命周期
go engine.Serve(listener)
// 收到信号后：
engine.Shutdown(ctx)  // 等待活跃请求完成，关闭所有连接
```

关闭流程：
1. 停止接受新连接
2. 等待活跃 HTTP 请求完成（5s 超时）
3. 发送 Close 帧给所有 WebSocket 连接
4. 等待所有 WebSocket goroutine 退出
5. 注销集群节点（如果启用）

---

## 项目结构

```
mux/
├── doc.go                    # 包文档
├── engine.go                 # Engine — 统一生命周期
├── options.go                # 基础选项（EngineOption 定义）
├── options_http.go           # HTTP 选项（WithHTTPAddr）
├── options_ws.go             # WebSocket 选项（WithWebSocketCompression 等）
├── options_cluster.go        # 集群选项（Role + WithCluster*）
├── context.go                # Context — 统一消息上下文
├── session.go                # Session 接口
├── message.go                # Message — 传输无关消息格式
├── pipeline.go               # Pipeline — 中间件链 + 路由
├── handler.go                # Handler / Middleware 接口
├── middleware.go             # 内置中间件（Log, Recover, Auth, RateLimit, PerKeyRateLimit, Sanitize, SecureHeaders, OnlyWS, OnlyHTTP）
├── facade.go                 # 公共 API 门面（便捷函数 + 类型别名：CmdMessage/MessageStore 等）
├── transport.go              # Transport 接口
├── engine_test.go            # Engine 集成测试
├── core_test.go              # 核心类型单元测试
├── pipeline_test.go          # Pipeline 单元测试
├── middleware_test.go        # 中间件单元测试
├── internal/
│   ├── transportx/           # Transport 实现
│   │   ├── transport_http.go # HTTPTransport（gin 封装）
│   │   └── transport_ws.go   # WSTransport（websocketx 封装）
│   ├── httpx/               # HTTP 工具箱（原 httpx 模块）
│   │   ├── middleware/       # gin 原生中间件（CORS / JWT / RateLimit / Panic）
│   │   ├── jwt/             # JWT 解析器
│   │   ├── response/         # ResponseOK / ResponseFail
│   │   └── ...
│   ├── websocketx/          # WebSocket 工具箱（原 websocketx 模块）
│   │   ├── hub.go           # Hub — 连接管理
│   │   ├── router.go        # Router — Cmd 路由
│   │   ├── handler.go       # Upgrade — WS 升级（含压缩配置）
│   │   ├── session.go       # Session — 连接状态
│   │   ├── heartbeat.go     # 心跳保活
│   │   ├── store.go         # MessageStore 接口 + StoredMessage
│   │   ├── store_noop.go    # NoOpMessageStore（空实现）
│   │   ├── store_redis.go   # RedisMessageStore（Redis ZSet + Hash）
│   │   └── ...
│   ├── cluster/             # 集群模块
│   │   ├── node.go          # Role + ClusterNode + Discovery 接口
│   │   ├── discovery_redis.go # Redis ZSet 节点发现
│   │   ├── manager.go       # ClusterManager — 心跳 + 节点拉取
│   │   └── forward.go       # Forwarder — HTTP 转发
│   ├── msg/                 # Context 内部实现（Session 适配）
│   │   ├── doc.go           # 包文档
│   │   └── session.go       # HTTPRequestSession + WSSession
│   └── types/               # 内部共享类型
│       ├── doc.go           # 包文档
│       └── message.go       # Message 类型
├── go.mod
└── README.md
```

---

## 快速开始

```go
package main

import (
    "time"
    
    "github.com/Effortful-lion/unibase/mux"
    "github.com/Effortful-lion/unibase/logx"
)

func main() {
    // 创建 Engine
    engine := mux.New(
        mux.WithReadTimeout(10 * time.Second),
        mux.WithMaxWebSocketConn(10000),
        mux.WithWebSocketHeartbeat(30*time.Second, 10*time.Second),
    )
    
    // 创建 Pipeline
    pipeline := mux.NewPipeline(engine)
    
    // 共享中间件
    pipeline.Use(
        mux.Log(logx.Default().Module("app")),
        mux.Recover,
        mux.Auth(mux.WithJWTSecret([]byte("your-secret"))),
    )
    
    // RESTful 路由
    pipeline.GET("/api/health", func(ctx *mux.Context) error {
        return ctx.ReplyOK(map[string]string{"status": "ok"})
    })
    pipeline.POST("/api/users", createUser)
    
    // Cmd 路由（HTTP Cmd + WebSocket Cmd 共用）
    pipeline.Cmd("user.create", createUser)
    pipeline.Cmd("chat.message", handleChat)
    
    // 一行挂载：自动注册三个入口
    engine.UsePipeline(pipeline)
    
    // 启动
    engine.Run(":8080")
}

func createUser(ctx *mux.Context) error {
    var req struct {
        Name  string `json:"name"`
        Email string `json:"email"`
    }
    
    if err := ctx.Bind(&req); err != nil {
        return ctx.ReplyError(400, "invalid params")
    }
    
    // 业务逻辑...
    
    return ctx.ReplyOK(user)
}
```

### WebSocket 客户端连接

```javascript
// 连接 WebSocket（JWT 通过 URL query 传递）
const ws = new WebSocket("ws://localhost:8080/ws?token=YOUR_JWT");

// 发送 Cmd 消息
ws.send(JSON.stringify({
    cmd: "chat.message",
    body: { text: "hello" }
}));

// 接收响应
ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    console.log(msg.cmd, msg.head, msg.body);
};
```

### 健康检查

```go
engine := mux.New(...)
engine.HTTP().GET("/health", mux.HealthHandler(engine))
```

### 路由日志输出

启动时通过 `LogRoutes` 输出所有已注册路由，包含三种入口类型：

| 前缀 | 入口类型 | 请求方式 |
|------|---------|---------|
| `GET/POST/PUT/DELETE` | REST 路由 | 直接 HTTP 请求 |
| `POST /v1/cmd` | HTTP Cmd 入口 | `POST /v1/cmd` + JSON body `{"cmd": "..."}` |
| `WS /ws` | WebSocket Cmd 入口 | 建立 WS 连接后发送 `{"cmd": "..."}` |

```go
engine := mux.New(...)
pipeline := mux.NewPipeline(engine)
// ... 注册路由 ...
engine.UsePipeline(pipeline)

// 输出路由信息到 logx
engine.LogRoutes(logx.Default().Module("routes"))
// 或使用便捷函数
mux.LogRoutes(engine, logx.Default().Module("routes"))
```

输出示例：

```
INFO routes count=3 list=[
  GET /api/health
  POST /api/users
  POST /v1/cmd → user.list, user.create, chat.message
  WS /ws → chat.message, notification.push
]
```

每个入口独立一行，`→` 右侧列出该入口可访问的 Cmd 名称。Cmd 路由天然同时支持 HTTP 和 WebSocket 两种入口，因此同一 Cmd 名称可能出现在 `POST /v1/cmd` 和 `WS /ws` 两行中。

---

## 核心 API

### Engine

```go
// 创建
engine := mux.New(opts...)

// 底层访问（向后兼容）
engine.HTTP()        // *gin.Engine
engine.WS()          // *websocketx.Router
engine.WSHub()       // *websocketx.Hub
engine.ClusterManager() // *cluster.ClusterManager（nil 表示单机）

// 生命周期
engine.Run(addr)     // 阻塞启动
engine.Serve(ln)     // 在已有 listener 上启动
engine.Shutdown(ctx) // 优雅关闭

// Pipeline 挂载
engine.UsePipeline(pipeline)
```

### Pipeline

```go
pipeline := mux.NewPipeline(engine)

// 中间件
pipeline.Use(mws...)           // 全局中间件
pipeline.UsePrefix(prefix, mws...) // 命令级中间件

// RESTful 路由（HTTP only）
pipeline.GET(path, handler)
pipeline.POST(path, handler)
pipeline.PUT(path, handler)
pipeline.DELETE(path, handler)

// Cmd 路由（HTTP Cmd + WebSocket Cmd 共用）
pipeline.Cmd(name, handler, mws...)
```

### Context

```go
func handler(ctx *mux.Context) error {
    // 数据传递
    ctx.Set("key", value)
    v, _ := ctx.Get("key")
    
    // 绑定请求体
    var req Request
    ctx.Bind(&req)
    
    // 会话
    userID := ctx.Session().UserID()
    
    // 响应
    return ctx.ReplyOK(data)
    return ctx.ReplyError(400, "bad request")
    
    // 元信息
    ctx.Cmd()       // 当前处理的 cmd 或路由路径
    ctx.Protocol()  // ProtocolHTTP / ProtocolWS
    ctx.RequestID() // 请求唯一标识
    ctx.Source()    // 原始 Go context
}
```

### 内置中间件

```go
mux.Log(logger)                              // 请求日志
mux.Recover                                  // Panic 恢复
mux.Auth(mux.WithJWTSecret(secret))          // JWT 认证（必须配置 secret）
mux.RateLimit(rate, burst)                   // 全局限流（令牌桶）
mux.PerKeyRateLimit(rate, burst)             // per-user/per-IP 限流（IM 推荐）
mux.Sanitize("password", "token")            // 响应字段过滤
mux.SecureHeaders                            // 安全响应头（X-Frame-Options 等）
mux.OnlyWS                                   // 仅 WebSocket
mux.OnlyHTTP                                 // 仅 HTTP
```

---

## 配置选项

### Engine 选项

```go
mux.WithReadTimeout(d)
mux.WithWriteTimeout(d)
mux.WithIdleTimeout(d)
mux.WithHTTPAddr(addr)
mux.WithWebSocketPath(path)
mux.WithMaxWebSocketConn(max)
mux.WithWebSocketHeartbeat(interval, pongWait)
mux.WithMaxMessageSize(size)            // WebSocket 单条消息最大字节数
mux.WithCmdPath(path)                   // HTTP Cmd 入口，默认 "/v1/cmd"
mux.WithHTTPMiddleware(mws...)
mux.WithCORS(opts...)                   // CORS 配置
mux.WithCheckOrigin(fn)                 // WebSocket 跨域校验
mux.WithWebSocketCompression(level)     // WebSocket permessage-deflate 压缩（1=BestSpeed ~ 9=BestCompression，-1=Default）
```

### 集群选项

```go
mux.WithClusterEnabled(true)
mux.WithClusterRole(mux.RoleAP)    // RoleMix / RoleAP / RoleBU
mux.WithClusterRedis("localhost:6379")
mux.WithClusterHeartbeatInterval(d)
mux.WithClusterNodeTTL(d)
mux.WithClusterServiceName("myapp")
mux.WithClusterGroup("default")
mux.WithClusterAdvertiseAddr("127.0.0.1:8080") // 节点对外通告地址
```

---

## IM 场景能力

mux 内置了 IM 场景常用的能力，组合使用即可构建完整的 IM 后端。

### F1：per-user/per-IP 限流

防止单用户刷屏耗尽资源。已认证用户按 userID 限流，未认证按 client IP 限流（WS 无 IP 时回退到 session ID）。

```go
pipeline.Use(
    mux.Auth(mux.WithJWTSecret(secret)),
    // 每用户每秒 5 条消息，桶容量 10（允许短暂突发）
    mux.PerKeyRateLimit(5, 10),
)

// 自定义 key 提取策略
pipeline.Use(mux.PerKeyRateLimit(5, 10, mux.WithKeyFunc(func(ctx *mux.Context) string {
    // 例如：按 cmd + userID 组合限流
    return ctx.Cmd() + ":" + ctx.Session().UserID()
})))
```

**与 `mux.RateLimit` 的区别**：
- `mux.RateLimit`：全局令牌桶，限制服务整体 QPS
- `mux.PerKeyRateLimit`：每 key 独立令牌桶，限制单用户/IP 的 QPS（IM 推荐）

### F2：WebSocket permessage-deflate 压缩

降低带宽消耗，适用于消息体较大的 IM 场景。

```go
engine := mux.New(
    mux.WithWebSocketCompression(-1), // -1=DefaultCompression（推荐，平衡速度与压缩率）
    // 其他选项：1=BestSpeed ~ 9=BestCompression
)
```

**注意事项**：
- 压缩会增加 CPU 开销，小消息（< 1KB）可能因压缩头开销反而增大
- 客户端需支持 permessage-deflate（主流浏览器和 gorilla/websocket 均支持）
- 服务端会自动协商压缩，无需客户端显式配置

### F3：消息持久化（离线消息）

提供 `MessageStore` 接口，用于 IM 场景的离线消息存储。框架不自动调用，由业务 Handler 按需使用。

```go
// 创建 Redis 实现
store := mux.NewRedisMessageStore(rdb)

// 存储离线消息（用户不在线时调用）
err := store.Save(ctx, &mux.StoredMessage{
    ID:         "msg-123",
    FromUserID: "user-a",
    ToUserID:   "user-b",
    Cmd:        "chat.message",
    Body:       []byte(`{"text":"hello"}`),
    CreatedAt:  time.Now(),
})

// 用户上线后拉取离线消息
msgs, err := store.FetchOffline(ctx, "user-b", 100)

// 客户端确认后清除
err := store.Ack(ctx, "user-b", []string{"msg-123"})
```

**实现选择**：
- `mux.NewNoOpMessageStore()`：空实现，默认使用，适合不需要持久化的场景
- `mux.NewRedisMessageStore(rdb)`：基于 Redis ZSet + Hash，按时间顺序存储离线消息

---

## IM 场景使用指南

mux 作为 IM 框架的典型用法。所有能力均由内置模块组合实现，无需引入额外依赖。

### 1. 用户上线/下线

```
客户端                                服务端
  │                                    │
  │── WS 连接 ws://host/ws?token=JWT ──→│  Engine.Upgrade
  │                                    │  AuthMiddleware 解析 token → 注入 userID
  │                                    │  Hub.register → 内存索引 + Redis SessionRegistry
  │←──────── 连接建立 ─────────────────│
  │                                    │
  │── 拉取离线消息 cmd:offline.fetch ──→│  Handler 调用 MessageStore.FetchOffline
  │←──────── 离线消息列表 ─────────────│
  │                                    │
  │   ... 正常通信 ...                  │
  │                                    │
  │── 断开连接 ───────────────────────→│  Hub.unregister → 清理索引 + Redis 注销
```

**框架自动完成**：
- AuthMiddleware 统一处理 WS 认证（从 Session Meta 的 `jwt_token` 字段提取 token）
- Hub 自动注册/注销到 Redis SessionRegistry（支持多 AP 节点查找用户所在节点）
- 心跳保活（Ping/Pong），超时自动断开

**业务无需关心**：连接管理、用户路由表、心跳超时、跨节点查找。

### 2. 单聊消息

```go
pipeline.Cmd("chat.message", func(ctx *mux.Context) error {
    var req struct {
        ToUserID string `json:"to_user_id"`
        Text     string `json:"text"`
    }
    if err := ctx.Bind(&req); err != nil {
        return ctx.ReplyError(400, "invalid params")
    }

    fromUserID := ctx.Session().UserID()
    msgID := generateMsgID()
    payload := map[string]any{
        "id":           msgID,
        "from_user_id": fromUserID,
        "text":         req.Text,
    }

    // 检查接收方是否在线
    if _, online := engine.WSHub().GetSessionByUserID(req.ToUserID); online {
        // 在线：直接推送
        msg, _ := mux.NewCmdMessage("chat.message", payload)
        _ = engine.WSHub().BroadcastToUser(ctx.Source(), req.ToUserID, msg)
    } else {
        // 离线：持久化存储
        body, _ := json.Marshal(payload)
        _ = store.Save(ctx.Source(), &mux.StoredMessage{
            ID:         msgID,
            FromUserID: fromUserID,
            ToUserID:   req.ToUserID,
            Cmd:        "chat.message",
            Body:       body,
            CreatedAt:  time.Now(),
        })
    }

    return ctx.ReplyOK(map[string]string{"msg_id": msgID})
})
```

### 3. 群聊消息（房间）

```go
// 用户加入房间
pipeline.Cmd("room.join", func(ctx *mux.Context) error {
    var req struct{ RoomID string `json:"room_id"` }
    _ = ctx.Bind(&req)
    ctx.WebSocket().Raw().JoinRoom(req.RoomID)
    return ctx.ReplyOK(nil)
})

// 群聊广播（排除发送者自己）
pipeline.Cmd("room.message", func(ctx *mux.Context) error {
    var req struct {
        RoomID string `json:"room_id"`
        Text   string `json:"text"`
    }
    _ = ctx.Bind(&req)
    msg, _ := mux.NewCmdMessage("room.message", map[string]any{
        "from": ctx.Session().UserID(),
        "text": req.Text,
    })
    _ = engine.WSHub().BroadcastToRoom(ctx.Source(), req.RoomID, msg, ctx.Session().ID())
    return ctx.ReplyOK(nil)
})
```

### 4. 离线消息拉取

```go
pipeline.Cmd("offline.fetch", func(ctx *mux.Context) error {
    userID := ctx.Session().UserID()
    msgs, err := store.FetchOffline(ctx.Source(), userID, 100)
    if err != nil {
        return ctx.ReplyError(500, "fetch offline failed")
    }
    return ctx.ReplyOK(map[string]any{"messages": msgs})
})

pipeline.Cmd("offline.ack", func(ctx *mux.Context) error {
    var req struct{ IDs []string `json:"ids"` }
    _ = ctx.Bind(&req)
    _ = store.Ack(ctx.Source(), ctx.Session().UserID(), req.IDs)
    return ctx.ReplyOK(nil)
})
```

### 5. 消息限流（防刷屏）

```go
pipeline.Use(
    mux.Auth(mux.WithJWTSecret(secret)),
    mux.PerKeyRateLimit(5, 10), // 每用户每秒 5 条，突发 10
)
```

### 6. 跨节点通信（集群模式）

集群模式下，所有跨节点细节由框架自动处理：

```go
engine := mux.New(
    mux.WithClusterEnabled(true),
    mux.WithClusterRole(mux.RoleAP),
    mux.WithClusterRedis("localhost:6379"),
    mux.WithClusterServiceName("myim"),
    mux.WithClusterAdvertiseAddr("node1.example.com:8080"),
)
```

| 场景 | 框架行为 |
|------|---------|
| 用户在 node1 上线 | Hub 自动注册 `user→node1` 到 Redis SessionRegistry |
| node2 上的用户给 node1 用户发消息 | `BroadcastToUser` 自动通过 Redis Pub/Sub 跨节点推送 |
| node1 宕机 | SessionRegistry 通过 TTL 自动清理，节点发现通过 ZSet 增量更新 |
| 新增 node3 节点 | discoveryLoop 自动拉取，一致性哈希环自动调整 |

**业务无需关心**：用户在哪台节点、跨节点消息路由、节点故障转移、一致性哈希路由。

---

## 中间件体系

mux 存在两套中间件，按需选用，可共存。

| 体系 | 类型 | 覆盖范围 | 注册方式 | 典型场景 |
|------|------|---------|---------|---------|
| **Pipeline 中间件** | `mux.Middleware`（`func(Handler) Handler`） | REST + HTTP Cmd + WS Cmd | `pipeline.Use()` | 统一认证、限流、日志（推荐） |
| **gin 中间件** | `gin.HandlerFunc` | 仅 HTTP REST 路由 | `engine.Use()` | gin 生态中间件复用 |

**执行顺序**：gin 中间件在外层（仅 HTTP），Pipeline 中间件在内层（HTTP + WS 统一）。

```go
// gin 中间件：仅作用于 REST 路由（GET /api/... 等）
engine.Use(gin.Logger(), gin.Recovery())

// Pipeline 中间件：同时覆盖 REST + Cmd + WS
pipeline.Use(
    mux.Log(logger),
    mux.Auth(mux.WithJWTSecret(secret)),
    mux.PerKeyRateLimit(5, 10),
)
```

**建议**：
- 优先使用 `mux.*` Pipeline 中间件，获得 HTTP + WS 统一覆盖
- `internal/httpx/middleware/` 是 gin 原生中间件工具箱（CORS/JWT/RateLimit/Panic），仅供直接使用 gin.Engine 的场景
- mux 用户用 `mux.WithCORS()` 选项即可，无需手动注册 gin CORS 中间件

---

## 安全建议

### 1. 传输层安全

- **生产环境必须使用 WSS**（WebSocket over TLS），HTTP 使用 HTTPS
- 通过 Nginx/反向代理终止 TLS，mux 本身监听 HTTP

### 2. JWT secret 管理

```go
// ❌ 错误：硬编码 secret
engine := mux.New()
pipeline.Use(mux.Auth(mux.WithJWTSecret([]byte("hardcoded-secret"))))

// ✅ 正确：从环境变量/配置中心读取
secret := []byte(os.Getenv("JWT_SECRET"))
if len(secret) == 0 {
    log.Fatal("JWT_SECRET environment variable is required")
}
pipeline.Use(mux.Auth(mux.WithJWTSecret(secret)))
```

- `mux.Auth` 强制要求配置 secret（不配置会 panic），防止忘记配置导致安全漏洞
- 建议使用 32 字节以上的随机字符串

### 3. WebSocket token 传递

WebSocket 协议限制：Upgrade 阶段无法设置自定义 HTTP header，token 通常通过 URL query 传递（`ws://host/ws?token=JWT`）。

**风险**：token 可能出现在 access log / browser history / Nginx 日志中。

**缓解建议**：
- 使用短时效 token（如 5 分钟），WS 连接建立后通过 cmd 续签
- Nginx/LB 配置 `log_format` 过滤 `token` 参数
- 避免将 WS URL 记录到业务日志

### 4. CheckOrigin 配置

```go
// ❌ 默认行为：放行空 Origin（curl/移动端 SDK），相同来源放行
engine := mux.New()

// ✅ 生产环境显式配置白名单
engine := mux.New(
    mux.WithCheckOrigin(func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        allowed := map[string]bool{
            "https://app.example.com": true,
            "https://web.example.com": true,
        }
        return allowed[origin]
    }),
)
```

- 默认 `defaultCheckOrigin` 放行空 Origin 是为了兼容非浏览器客户端
- 生产环境应通过 `WithCheckOrigin` 显式配置允许的来源白名单

### 5. 限流策略

| 场景 | 推荐方案 |
|------|---------|
| IM 消息发送 | `mux.PerKeyRateLimit`（per-user，防刷屏） |
| 公开 API（登录、注册） | `mux.RateLimit`（全局，防 DDoS） |
| 文件上传 | `mux.PerKeyRateLimit` 按 cmd + userID 组合限流 |

### 6. 消息鉴权

框架只做身份认证（JWT → userID），cmd 级权限由业务 Handler 自行校验：

```go
pipeline.Cmd("admin.shutdown", func(ctx *mux.Context) error {
    // 业务层自行校验权限
    if !isAdmin(ctx.Session().UserID()) {
        return ctx.ReplyError(403, "forbidden")
    }
    // ... 执行管理员操作
    return ctx.ReplyOK(nil)
})
```

### 7. 安全响应头

```go
pipeline.Use(
    mux.SecureHeaders, // 自动添加 X-Content-Type-Options、X-Frame-Options 等
)
```

仅对 HTTP 模式生效（WebSocket 无 HTTP 响应头）。

---

## 与 IMS 的对比

| 维度 | IMS | mux（本框架） |
|------|-----|---------------|
| **定位** | IM 专用网关 | 通用 Web 框架（IM 是子集） |
| **架构** | 单模块，flat 结构 | 多模块 monorepo，核心聚合在 mux |
| **Context** | ImjContext（必须） | Context（可选，也支持直接用 gin.Context） |
| **Pipeline** | 核心机制，强制使用 | 核心机制，但 REST 路由可绕过 |
| **消息模型** | 强制 Cmd 模型 | RESTful + Cmd 双模型 |
| **传输层** | 自定义 Transport 抽象 | Transport 接口（可插拔） |
| **集群** | AP/BU/MIX（依赖 oin/discovery） | 三层架构（Redis 实现，无外部依赖） |
| **协议** | HTTP + WebSocket | HTTP + WebSocket（RPC planned） |
| **框架绑定** | Echo | Gin（HTTP）+ gorilla/websocket（WS） |

---

## 设计讨论记录

本文件记录了 mux 框架从零到完整设计的所有关键决策。

### 讨论日期：2026-08-14

**参与者**：框架设计者 + AI 助手

**核心决策**：

1. **采用路径 B**：httpx 和 websocketx 作为 mux 的 internal 子模块，不再独立对外发布
   - 理由：减少维护成本，提供统一的用户入口
   - 影响：httpx 和 websocketx 成为 mux 的实现细节，可自由重构

2. **Pipeline 是核心架构，不是可选层**
   - 所有请求（RESTful + Cmd）共享同一套中间件链
   - WebSocket 仅支持 Cmd 模式
   - HTTP 同时支持 RESTful 和 Cmd 两种模式

3. **统一 Context**
   - Context 屏蔽 HTTP 和 WebSocket 的差异
   - 业务逻辑不感知底层传输协议
   - Session 接口统一 HTTP（per-request）和 WebSocket（长连接）

4. **自定义 Transport 层**
   - Engine 持有 Transport 接口而非具体的 gin.Engine / websocketx.Hub
   - 协议扩展（RPC）不影响核心逻辑
   - 测试时可用 MockTransport

5. **集群三层架构**
   - MIX（默认单机）→ AP+BU 分离 → 完整分布式
   - 基于 Redis ZSet 实现节点发现（不依赖外部服务发现框架）
   - 分阶段实现，MIX 模式零配置

6. **选项按域拆分**
   - options.go（基础）+ options_http.go + options_cluster.go

7. **参考 IMS 的优秀设计**
   - ImjContext 的统一消息抽象
   - Pipeline 的中间件链 + 命令路由
   - Gateway 生命周期管理
   - 三层集群架构

8. **生产安全加固**
   - WebSocket CheckOrigin 默认拒绝跨域（防止 CSRF）
   - JWT 中间件强制要求配置 secret（panic 提示，无默认值）
   - Pipeline 中间件链缓存（buildChain 结果按路由缓存，middleware 变更时失效）
   - RateLimit 改为 per-session 限流（避免单用户耗尽全局配额）
   - Engine.Run 防重入保护（started 标志 + 互斥锁）
   - HTTPTransport.ServeListener 感知 context 取消（Shutdown 时优雅关闭）
    - Hub.Shutdown 等待所有 goroutine 退出（无泄漏）
    - MessageCodec 支持自定义消息类型（TextMessage/BinaryMessage）
    - Redis 客户端配置超时（DialTimeout/ReadTimeout/WriteTimeout/PoolTimeout）
    - ClusterManager 使用 WaitGroup 管理 goroutine 生命周期（Start/Stop 无泄漏）
    - heartbeatLoop 记录 Register 错误日志（Redis 故障可观测）
    - Context.Reply 完成后断开 rawHTTP/rawWS 引用（避免内存泄漏）
    - RecoverMiddleware 记录错误日志（ReplyError 失败可观测）
    - 新增 Redis 集成测试（RUN_REDIS_INTEGRATION=1）

### 设计讨论记录（2026-08-15）

**用户身份认证统一**：

1. **统一 AuthMiddleware**：`AuthMiddleware` 同时处理 HTTP 和 WebSocket 的 JWT 认证，不再有独立的 `WSAuthMiddleware`
   - HTTP：从 `Authorization` header 提取 token（通过 `headerAccessor` 接口适配 gin.Context）
   - WS：从 Session Meta 的 `jwt_token` 字段提取 token（Upgrade 阶段由 Engine 注入）
   - 验证后统一通过 `Session.UserID()` 提供 userID

2. **setUserID 分发**：`AuthMiddleware` 内部通过类型断言区分 HTTP 和 WS Session 实现
   - `*msg.HTTPRequestSession` → `WithUserID(userID)`
   - `*msg.WSSession` → `Raw().SetUserID(userID)`

3. **headerAccessor 修复**：接口方法从 `Header(string) string` 修正为 `GetHeader(string) string`，匹配 gin.Context 实际提供的 API

**多实例部署准备**：

1. **WS 认证中间件已就位**：`AuthMiddleware` 统一覆盖 HTTP + WS，无需额外注册
2. **Session 映射注册**：Hub 在 register/unregister 时通过 `SessionRegistry` 接口自动注册/注销到 Redis
3. **跨 AP 广播**：`BroadcastBus` 接口基于 Redis Pub/Sub，Hub 广播时自动发布到集群
4. **Session 冲突处理**：`SetUserID` 增加冲突检测，同一 userID 已存在时踢掉旧连接

**IM 场景能力补全**：

1. **per-user/per-IP 限流（F1）**：新增 `PerKeyRateLimitMiddleware`，按 userID（已认证）或 client IP（未认证）隔离令牌桶，自动清理 30 分钟未访问的 limiter。与全局 `RateLimit` 互补：全局防 DDoS，per-key 防单用户刷屏
2. **WebSocket 压缩（F2）**：新增 `WithWebSocketCompression(level)` 选项，启用 permessage-deflate。透传路径：EngineOption → engineOptions → WSTransport → Upgrade → upgrader.EnableCompression + conn.SetCompressionLevel
3. **消息持久化（F3）**：定义 `MessageStore` 接口（Save/FetchOffline/Ack），提供 NoOp 和 Redis 实现。框架不自动调用，由业务 Handler 按需使用。Redis 实现使用 ZSet 维护时间顺序 + Hash 存储消息内容
4. **`CmdMessage` 导出**：facade.go 导出 `CmdMessage` 类型别名和 `NewCmdMessage` 构造函数，使外部用户可调用 `Hub.Broadcast/BroadcastToRoom/BroadcastToUser` 等广播 API

**易用性提升**：

1. **文档齐全性（D1/D2/D3）**：修复 README 重复标题和章节编号错乱；补全 `internal/msg/` 和 `internal/types/` 的 doc.go；新增 IM 场景使用指南章节
2. **注释完整性（C1）**：补充 context/pipeline/middleware/hub/engine/cluster 关键路径注释；强化 `defaultCheckOrigin` 安全注意事项
3. **命名一致性（N1）**：文档统一使用 facade 便捷函数（`mux.Log`/`mux.Auth`/`mux.Recover` 等），不混用 `*Middleware` 长名；`extractToken` 改用专用 `errTokenNotFound` 替代 `http.ErrNoCookie`
4. **架构清晰性（A1）**：doc.go 和 README 补充中间件双体系说明（Pipeline 中间件 vs gin 中间件），澄清覆盖范围和执行顺序
5. **安全性（S1）**：新增 `SecureHeadersMiddleware`（X-Content-Type-Options、X-Frame-Options、X-XSS-Protection、Referrer-Policy）；README 新增安全建议章节（TLS/JWT secret/WS token/CheckOrigin/限流策略/消息鉴权/安全响应头）

---

## 开发归档

> 以下内容从 `spec/` 目录归档合并，记录框架从设计到实现的完整过程。

### A. 实现阶段概览

| 阶段 | 内容 | 说明 |
|------|------|------|
| Phase 1 | 基础类型：Context + Session + Message | 传输无关的消息格式 + 统一会话接口 + 统一消息上下文 |
| Phase 2 | Pipeline 核心 | Handler/Middleware 接口 + 中间件链 + RESTful 路由 + Cmd 路由 |
| Phase 3 | Transport 层 | Transport 接口 + HTTPTransport（gin 封装）+ WSTransport（websocketx 封装） |
| Phase 4 | Engine 集成 | 统一生命周期管理 + 选项按域拆分（http/ws/cluster） |
| Phase 5 | Facade + 文档 | 公共 API 门面（类型别名 + 便捷函数）+ 包文档 |
| Phase 6 | 集群模块 | 三层架构（MIX/AP/BU）+ Redis 节点发现 + 消息转发 |

### B. 代码审查记录

> 审查时间：2026-08-14 ~ 2026-08-15

#### P0 — 已修复

| # | 问题 | 修复 |
|---|------|------|
| 1 | `GetNodes` 未排除自身节点 | 增加 `node.Tag != cm.self.Tag` 判断 |
| 2 | `heartbeatLoop` 段错误（ticker nil） | 入口添加 nil 防御性检查 |

#### P1 — 已修复

| # | 问题 | 修复 |
|---|------|------|
| 1 | JWT Token 注入时机（WS 连接建立阶段未注入） | Hub 新增 `initSession sync.Once` + `SetSessionInit(fn)`，`runSession` 创建 Session 后立即调用 |
| 2 | Pipeline 中间件链职责澄清 | 明确设计意图：REST 走 gin 原生中间件，Cmd 走 Pipeline 统一中间件链 |
| 3 | `extractToken` 隐式接口 | 定义显式 `headerAccessor` 接口类型 |

#### P2 — 已修复

| # | 问题 | 修复 |
|---|------|------|
| 1 | `Conn.Write` 竞态窗口 | 三次 select 合并为带循环的单逻辑块 |
| 2 | `runSession` 断开原因未区分 | 使用 `websocket.IsCloseError` 区分正常关闭和异常断开 |
| 3 | `discoveryLoop` 全量替换阻塞读者 | 改为增量更新：只删除不存在于新集合的旧节点，只添加新节点 |

#### P3 — 已修复

| # | 问题 | 修复 |
|---|------|------|
| 1 | 错误处理不一致 | `replyREST`/`replyCmdHTTP` 移除无意义 error 返回；`executeHandler`/`executeCmd` 返回 error 上层显式处理 |
| 2 | 日志模块名不统一 | 全部 `logx.Default().Module(...)` 统一为 `"mux"` |
| 3 | Context 生命周期注释缺失 | `rawHTTP` 字段补充 Reply 后置 nil 的注释 |

#### 审查发现修复（2026-08-15）

| # | 问题 | 修复 |
|---|------|------|
| 1 | `ServeHTTP` nil 解引用（WS-only 模式 Serve 前 panic） | 增加 `e.httpEngine == nil` 检查，返回 503 |
| 2 | `Use` nil 解引用（WS-only 模式调用 panic） | 增加 nil 检查，静默忽略 |
| 3 | `register`/`unregister` 数据竞争 | 加锁时快照 `userID` 到局部变量 |
| 4 | `BroadcastToUser` 不发布到跨 AP broadcastBus | 本地未找到时通过 `broadcastBus.Publish` 跨 AP 投递 |
| 5 | `HashRing.removeLocked()` 死代码 | 删除无用 `newRing` 分配 |
| 6 | `internal/httpx/go.mod` / `internal/websocketx/go.mod` 错误存在 | 删除，内部包不应有独立 go.mod |
| 7 | `PerKeyRateLimitMiddleware` goroutine 泄漏 | 新增 `PerKeyRateLimitCleanup()` 工厂函数 |

#### 已知限制

| # | 问题 | 建议 |
|---|------|------|
| 1 | `PerKeyRateLimitCleanup` 创建新 pool 而非停止中间件使用的 pool | 后续重构为返回 `(Middleware, func())` |
| 2 | `rateLimitPool` 无自动 GC | 现有 30 分钟不活跃自动清理已缓解 |

### C. 多实例部署架构分析

> 审查时间：2026-08-15

#### 关键发现

1. **HTTP 与 WebSocket 共享端口不影响 LB 分发**：外部负载均衡器在 TCP/HTTP 层工作，可正常分发 HTTP REST 请求和 WebSocket Upgrade 请求

2. **WebSocket 长连接有状态性**：连接一旦建立固定在某台 AP，Hub 和 Session 均为内存实现。已通过 `SessionRegistry`（Redis）+ `BroadcastBus`（Redis Pub/Sub）解决跨节点 Session 定位和广播

3. **一致性哈希在 LB 层而非 Go 代码内**：Go 代码配合的是"AP 节点向 Redis 报告 Session 映射"，以便 BU 能定位用户所在 AP

4. **用户身份传递统一**：`AuthMiddleware` 统一覆盖 HTTP + WS，通过类型断言区分 Session 实现，分别注入 userID

#### 多实例部署完整清单

| 优先级 | 内容 | 状态 |
|--------|------|------|
| P0 | AP + BU 角色分离 | ✅ 已有 |
| P0 | LB 层 IP Hash 路由 | 📋 部署配置 |
| P0 | AP 节点向 Redis 注册 Session 映射 | ✅ 已完成 |
| P1 | WS 路径认证中间件 | ✅ 已完成（统一 AuthMiddleware） |
| P1 | AP Session 注册到 Redis（SessionRegistry） | ✅ 已完成 |
| P1 | 跨 AP 广播机制（Redis Pub/Sub） | ✅ 已完成 |
| P1.5 | 网络切换时的 Session 冲突处理 | ✅ 已完成 |
| P2 | 断开重连 + 消息补偿 | 📋 后续 |

### D. 框架适用性评估

> 审查时间：2026-08-15

**结论：可以作为 IM 和 Web 应用框架使用。**

#### 已具备的核心能力

| 能力 | 说明 |
|------|------|
| 统一 Context 抽象 | 所有请求包装成统一 Context，Handler 编写一套代码，HTTP/WS 自动适配 |
| Pipeline 双路由模式 | REST 路由直接注册 gin + Cmd 路由 HTTP Cmd/WS Cmd 共用 |
| 协议解耦 | 三种启动模式：全栈 / 纯 HTTP / 纯 WS |
| WebSocket 管理 | 连接数限制、心跳保活、消息速率限制、Room 广播、用户索引、冲突检测、跨 AP 广播 |
| 集群支持 | Redis 节点发现、HTTP 消息转发、一致性哈希（CRC32 + 虚拟节点） |
| 内置中间件 | Auth（JWT 双通道）、PerKeyRateLimit、Log、Recover、Sanitize、SecureHeaders |
| 扩展能力 | MessageStore 离线消息持久化、HealthHandler 自动返回 WS 连接数 |

#### 需要补充的缺口

| 优先级 | 缺口 | 影响 |
|--------|------|------|
| P0 | 跨 AP 单播投递 | `BroadcastToUser` 仅查本地 Hub，集群模式下远程用户消息可能丢失。已通过 `broadcastBus.Publish` 跨 AP 投递缓解 |
| P1 | Message 元数据 | Message 只有 Cmd/Head/Body，IM 场景的 sender_id/conversation_id 等需业务层在 body 中编码 |
| P1 | Room 管理增强 | 当前只有 Join/Leave/Broadcast，缺少 ListMembers/RoomInfo/History |

#### 与 IMS 的对齐度

| 维度 | IMS | mux | 对齐度 |
|------|-----|-----|--------|
| 协议开关 | `protocols["http"/"ws"]` | `DisableHTTP()` / `DisableWS()` | 等价 |
| 统一 Context | ImjContext | Context | 等价 |
| Cmd 路由 | `pipes.BindHandler` | `pipeline.Cmd` | 等价 |
| 中间件链 | `pipes.BindMiddleWare` | `pipeline.UsePrefix` | 等价 |
| WS Session | ImjSession | websocketx.Session | 等价 |
| 集群发现 | ClusterDiscovery | RedisDiscovery | 等价 |
| 消息转发 | ClusterSession.Invoke | Forwarder.Forward | 等价 |
| 负载均衡 | N/A | HashRing + ClusterManager | 超出 |
| 跨 AP 广播 | Redis Pub/Sub | Redis Pub/Sub (BroadcastBus) | 等价 |
| 跨 AP 单播 | FindUserImjSessions | ✅ 已补充（broadcastBus） | 已对齐 |

### E. 协议解耦设计

> 背景解决 `Engine.Serve()` 强制同时启动 HTTP 和 WebSocket 的问题

#### 三种启动模式

```go
// 全栈模式（默认，向后兼容）
engine := mux.New()  // HTTP + WS

// 纯 REST 微服务（BU 角色）
engine := mux.New(mux.DisableWS())

// 纯 WebSocket 服务
engine := mux.New(mux.DisableHTTP())
```

| 模式 | HTTP Transport | WS Transport | 适用场景 |
|------|---------------|-------------|---------|
| 全栈 | ✅ REST + HTTP Cmd | ✅ WS Upgrade + WS Cmd | IM 后端（AP/MIX 角色） |
| 纯 HTTP | ✅ REST + HTTP Cmd | ❌ | 纯 REST API 微服务 |
| 纯 WS | ✅ 最小引擎（仅 Upgrade） | ✅ WS Upgrade + WS Cmd | 纯长连接服务（实时推送、游戏） |

**设计说明**：纯 WS 模式仍需一个最小 HTTP 服务承载 Upgrade 握手（WebSocket 协议限制），但用户感知上无 REST 路由、无 HTTP Cmd 入口。

#### 条件性创建与启动

- `New()`：按 `enableHTTP`/`enableWS` 条件创建 transport
- `Serve()`：拆分为 `serveBoth`/`serveHTTPOnly`/`serveWSOnly`
- `UsePipeline()`：条件性注册 Cmd 入口（HTTP Cmd 仅 enableHTTP 时注册）
- `initCluster()`：仅在 `enableWS` 时注入 Hub 集群组件

### F. WebSocket 负载均衡

#### 问题本质

IM 的 WebSocket 连接是**有状态**的：客户端连到节点 A 后，后续所有消息必须走节点 A。扩容新增节点 B 时新客户端可分配到 B，但已在 A 上的客户端不能迁移（除非断线重连）。

#### 工业标准方案

```
客户端 → 负载均衡器（Nginx/云LB）
            ↓  sticky session（IP hash / cookie）
       WS 节点 A  ←─── Redis Pub/Sub ───→  WS 节点 B
            ↓                              ↓
       SessionRegistry（Redis）       SessionRegistry（Redis）
            ↓                              ↓
       Cluster Discovery（Redis ZSet）
```

| 层 | 职责 | 技术选型 |
|----|------|---------|
| 负载均衡 | 新连接分配 + sticky session | Nginx `ip_hash` / 云 LB sticky cookie |
| 节点发现 | 知道哪些节点在线 | Redis ZSet（已实现） |
| 消息投递 | 跨节点转发消息 | Redis Pub/Sub（已实现） |

#### 一致性哈希不适用 IM WS 连接

一致性哈希解决"给定 key 应该去哪个节点"的分配问题。在 IM 场景中：
1. 连接分配靠 LB 的 sticky session，不需要一致性哈希
2. 消息投递靠节点发现 + 消息队列，目标节点的 `ConnectUrl` 已足够
3. 一致性哈希适用于无状态服务（如缓存集群），IM 的 WS 连接不适合

#### Nginx ip_hash 配置示例

```nginx
upstream ap_backend {
    ip_hash;
    server ap-1:8080;
    server ap-2:8080;
    server ap-3:8080;
}
```

**注意**：NAT 环境下多个客户端共享出口 IP 时，`ip_hash` 会导致负载不均。可考虑用 `$http_x_forwarded_for` 或基于 userID 的哈希。

### G. 关键决策记录

| # | 决策 | 时间 |
|---|------|------|
| 1 | 采用路径 B：httpx + websocketx 作为 mux 的 internal | 2026-08-14 |
| 2 | Pipeline 是核心架构，不是可选层 | 2026-08-14 |
| 3 | 统一 Context：屏蔽 HTTP/WS 差异 | 2026-08-14 |
| 4 | 自定义 Transport 接口（先内部抽象，不对外暴露） | 2026-08-14 |
| 5 | 集群三层架构：MIX → AP+BU → 完整分布式 | 2026-08-14 |
| 6 | 选项按域拆分（options_http/ws/cluster） | 2026-08-14 |
| 7 | Pipeline 持 Engine 引用，注册路由时实时写入 | 2026-08-14 |
| 8 | UsePipeline 一行挂载，自动注册三个入口 | 2026-08-14 |
| 9 | pipeline.Cmd() 注册，HTTP Cmd + WS Cmd 共用路由表 | 2026-08-14 |
| 10 | facade.go 只做类型别名和函数转发，不包含业务逻辑 | 2026-08-14 |
| 11 | 统一 AuthMiddleware 替代独立 WSAuthMiddleware | 2026-08-15 |
| 12 | 协议解耦：DisableHTTP/DisableWS 三种启动模式 | 2026-08-15 |
| 13 | SessionRegistry + BroadcastBus 实现跨 AP 通信 | 2026-08-15 |
| 14 | per-key 限流 + WS 压缩 + 消息持久化补全 IM 能力 | 2026-08-15 |
