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
                    │  │ Log → Auth → ...   │  │
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
| **中间件共享** | REST 和 Cmd 共享同一套全局中间件链 |
| **前缀中间件** | `pipeline.UsePrefix("file.*", mw)` 按 cmd 前缀匹配中间件 |

```go
pipeline := mux.NewPipeline(engine)

// 共享中间件（所有入口都走这套）
pipeline.Use(mux.Log(logger), mux.Recover, mux.Auth(mux.WithJWTSecret(secret)))

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
| **JWT 认证** | `mux.Auth(mux.WithJWTSecret(secret))` 中间件，强制要求配置 secret（panic 提示），支持 HMAC 算法校验 |
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

### 6. 集群三层架构

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

### 7. 优雅关闭

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
├── facade.go                 # 公共 API 门面（便捷函数）
├── engine.go                 # Engine — 统一生命周期
├── options.go                # 基础选项（EngineOption 定义）
├── options_http.go           # HTTP 选项（WithHTTPAddr）
├── options_cluster.go        # 集群选项（Role + WithCluster*）
├── context.go                # Context — 统一消息上下文
├── session.go                # Session 接口
├── message.go                # Message — 传输无关消息格式
├── pipeline.go               # Pipeline — 中间件链 + 路由
├── handler.go                # Handler / Middleware 接口
├── middleware.go             # 内置中间件（Log, Recover, Auth, RateLimit, Sanitize, OnlyWS, OnlyHTTP）
├── transport.go              # Transport 接口
├── engine_test.go            # Engine 集成测试
├── core_test.go              # 核心类型单元测试
├── pipeline_test.go          # Pipeline 单元测试
├── internal/
│   ├── transportx/           # Transport 实现
│   │   ├── transport_http.go # HTTPTransport（gin 封装）
│   │   └── transport_ws.go   # WSTransport（websocketx 封装）
│   ├── httpx/               # HTTP 工具箱（原 httpx 模块）
│   │   ├── middleware/       # CORS / JWT / RateLimit / Panic
│   │   ├── jwt/             # JWT 解析器
│   │   ├── response/         # ResponseOK / ResponseFail
│   │   └── ...
│   ├── websocketx/          # WebSocket 工具箱（原 websocketx 模块）
│   │   ├── hub.go           # Hub — 连接管理
│   │   ├── router.go        # Router — Cmd 路由
│   │   ├── handler.go       # Upgrade — WS 升级
│   │   ├── session.go       # Session — 连接状态
│   │   ├── heartbeat.go     # 心跳保活
│   │   └── ...
│   ├── cluster/             # 集群模块
│   │   ├── node.go          # Role + ClusterNode + Discovery 接口
│   │   ├── discovery_redis.go # Redis ZSet 节点发现
│   │   ├── manager.go       # ClusterManager — 心跳 + 节点拉取
│   │   └── forward.go       # Forwarder — HTTP 转发
│   ├── msg/                 # Context 内部实现（Session 适配）
│   │   └── session.go       # HTTPRequestSession + WSSession
│   └── types/               # 内部共享类型
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
mux.Log(logger)                    // 请求日志
mux.Recover                        // Panic 恢复
mux.Auth(mux.WithJWTSecret(secret))                   // JWT 认证（必须配置 secret）
mux.RateLimit(rate, burst)         // 令牌桶限流
mux.Sanitize("password", "token")  // 响应字段过滤
mux.OnlyWS                         // 仅 WebSocket
mux.OnlyHTTP                       // 仅 HTTP
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
mux.WithCORS(opts...)              // CORS 配置
mux.WithCheckOrigin(fn)            // WebSocket 跨域校验
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
