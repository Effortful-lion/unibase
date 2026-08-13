# websocketx

基于 gorilla/websocket 的 WebSocket 服务端开发工具包。

## 核心能力

- **Cmd 模式路由**：`router.Cmd("cmd", handler)` 注册，handler 接收 `*CmdMessage`
- **Session 管理**：每条连接对应一个 Session，携带 ID / UserID / Meta / State / Rooms
- **中间件链**：全局 + 路由级局部中间件，内置 Panic 兜底
- **心跳保活**：内建 Ping/Pong 心跳，Pong 超时自动断开，无需外部定时组件
- **并发限流**：基于 semaphore 的连接数门控，`NewHub(maxConn)` 控制上限
- **房间分组**：`JoinRoom` / `BroadcastToRoom` 实现房间级广播（roomIndex 原子更新）
- **踢下线**：`Hub.Kick(userID)` 按用户踢掉连接
- **监控埋点**：强类型 MetricsEvent 枚举 + StandardMetricLabels 辅助函数
- **可扩展编解码**：MessageCodec 接口支持非 JSON 协议
- **关闭原因常量**：标准 CloseReason 枚举，便于监控统计与前端提示

## 安装

```bash
go get github.com/Effortful-lion/unibase/websocketx
```

## 消息协议

所有消息均为 JSON 格式：

```jsonc
// 请求
{
  "cmd": "file.upload",
  "meta": { "requestId": "abc123", "userID": "u_001" },
  "body": { "fileName": "a.jpg", "fileSize": 10240 }
}

// 响应
{
  "cmd": "file.upload",
  "meta": { "code": "10200" },
  "body": { "url": "https://oss.example.com/a.jpg" }
}
```

## 快速开始

```go
package main

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/Effortful-lion/unibase/websocketx"
)

type UploadReq struct {
    FileName string `json:"fileName"`
}

func main() {
    hub := websocketx.NewHub(10000,
        websocketx.WithHeartbeat(30*time.Second, 60*time.Second),
        websocketx.WithMaxMessageSize(1<<20), // 1MB 防攻击
        websocketx.WithCloseReason("kicked_by_server", "server_maintenance"),
    )
    router := websocketx.NewRouter()

    // 注册路由
    router.Cmd("file.upload", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
        var req UploadReq
        if err := msg.Bind(&req); err != nil {
            return err
        }
        // 业务逻辑...
        return session.Conn().Write(ctx, &websocketx.CmdMessage{
            Cmd:  msg.Cmd,
            Meta: map[string]interface{}{"code": "10200"},
            Body: json.RawMessage(`{"url":"https://oss.example.com/` + req.FileName + `"}`),
        })
    })

    // 全局中间件（Panic 兜底 + Auth）
    router.Use(websocketx.RecoverMiddleware)
    router.Use(func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage, next websocketx.MessageHandler) error {
        // auth check...
        return next(ctx, session, msg)
    })

    http.Handle("/ws", websocketx.Upgrade(hub, router.Handle))
    http.ListenAndServe(":8080", nil)
}
```

## 核心类型

### CmdMessage

WebSocket 传输的统一消息格式。

```go
type CmdMessage struct {
    Cmd  string                 `json:"cmd"`  // 命令标识，如 "file.upload"
    Meta map[string]interface{} `json:"meta"` // 元数据：code、requestId、userID 等
    Body json.RawMessage        `json:"body"` // 业务载荷，延迟解析
}
```

### Router

按 Cmd 分发消息。

```go
// 创建
router := websocketx.NewRouter()

// 注册路由（支持路由级中间件）
router.Cmd("chat.message", handler.ChatMessage)
router.Cmd("file.upload", handler.FileUpload, authMiddleware, rateLimitMiddleware)

// 注册全局中间件（按顺序执行，在路由级中间件外层）
router.Use(websocketx.RecoverMiddleware)
router.Use(authMiddleware)
```

中间件签名：

```go
type MiddlewareFunc func(ctx context.Context, session *Session, msg *CmdMessage, next MessageHandler) error
```

中间件可以：
- **拦截**：返回 error，不调用 `next`
- **透传**：调用 `next(ctx, session, msg)` 继续
- **前后置逻辑**：在 `next` 前后各写逻辑

执行顺序：全局中间件 → 路由级中间件 → Handler

### Hub

管理所有活跃连接，支持并发限流、房间广播、踢下线。

```go
// 无限制
hub := websocketx.NewHub(0)

// 最多 10000 并发连接
hub := websocketx.NewHub(10000)

// 完整配置
hub := websocketx.NewHub(10000,
    websocketx.WithHeartbeat(30*time.Second, 60*time.Second),
    websocketx.WithMaxMessageSize(1<<20), // 1MB
    websocketx.WithCloseReason("kicked", "shutdown"), // 自定义关闭原因
    websocketx.WithMetrics(onConnect, onDisconnect, onMessage, onBroadcast),
    websocketx.WithOnConnect(func(s *websocketx.Session) { /* ... */ }),
    websocketx.WithOnDisconnect(func(s *websocketx.Session) { /* ... */ }),
)
```

Hub 方法：

| 方法 | 说明 |
|------|------|
| `Register(session)` | 注册 Session（内部调用，触发 onConnect 回调） |
| `Unregister(id)` | 注销 Session（触发 onDisconnect 回调） |
| `Broadcast(ctx, msg, except...)` | 广播消息，except 中的 ID 被排除 |
| `BroadcastToRoom(ctx, roomID, msg, except...)` | 向房间广播 |
| `BroadcastToUser(ctx, userID, msg)` | 向指定用户发消息 |
| `Kick(userID)` | 按用户 ID 踢掉连接 |
| `JoinRoom(session, roomID)` | 将 Session 加入房间（原子更新 roomIndex） |
| `LeaveRoom(session, roomID)` | 将 Session 从房间移除 |
| `ListRoomUsers(roomID)` | 获取房间内所有 Session ID |
| `CountRooms()` | 返回当前房间数量 |
| `GetSession(id)` | 根据 ID 获取 Session |
| `GetSessionByUserID(userID)` | 根据用户 ID 获取 Session |
| `Count()` | 当前连接数 |
| `Shutdown(ctx)` | 优雅关闭 |

### Session

单条连接的状态容器。

```go
type Session struct {
    ID() string              // 唯一标识（8 字节随机 hex）
    UserID() string          // 关联用户 ID
    SetUserID(userID string) // 绑定用户 ID（自动同步 Hub.userIndex）
    Conn() *Conn             // 底层连接
    Meta() Meta              // 元数据副本（只读）
    SetMeta(key, val)        // 设置元数据
    GetState(key) (val, ok)  // 业务自定义状态
    SetState(key, val)       // 设置业务状态
    JoinRoom(roomID string)  // 加入房间（原子更新 Hub.roomIndex）
    LeaveRoom(roomID string) // 离开房间
    Rooms() []string         // 当前所在房间列表
}
```

### Conn

单条连接的读写封装。

```go
type Conn struct {
    Read(ctx) (*CmdMessage, error)         // 读取一条消息（自动 JSON 解析）
    Write(ctx, *CmdMessage) error          // 写入一条消息（异步缓冲，背压丢弃最旧）
    Reply(ctx, msg, code, err) error       // 统一错误回复
    Ping(ctx) error                        // 发送 Ping 帧
    Close(code, reason) error              // 关闭连接
    Session() *Session                     // 获取关联 Session
    SetPongHandler(h) error                // 设置 Pong 帧处理函数
    SendBufferUsage() (used, capacity int) // 发送缓冲区使用量
}
```

写路径采用异步缓冲 + 背压处理：`Write` 将编码后的字节推入 `sendCh` 立即返回，独立 goroutine（`writePump`）消费 `sendCh` 写入底层连接。队列满时丢弃最旧消息，保证不会阻塞调用方。

### CmdMessage 便捷方法

```go
// Bind 解析 Body 到目标结构体
var req UploadReq
if err := msg.Bind(&req); err != nil { return err }

// Reply 统一错误回复
return session.Conn().Reply(ctx, msg, "10500", err)
```

### MessageCodec

消息编解码抽象，默认 JSONCodec，可自定义实现非 JSON 协议。

```go
type MessageCodec interface {
    Encode(*CmdMessage) ([]byte, error)
    Decode([]byte) (*CmdMessage, error)
}

// 自定义编解码器
http.Handle("/ws", websocketx.Upgrade(hub, router.Handle,
    websocketx.WithCodec(myProtoCodec),
))
```

### Upgrade

HTTP 升级为 WebSocket 连接。

```go
http.Handle("/ws", websocketx.Upgrade(hub, router.Handle))

// 可选配置
http.Handle("/ws", websocketx.Upgrade(hub, router.Handle,
    websocketx.WithCheckOrigin(func(r *http.Request) bool {
        return r.Header.Get("Origin") == "https://example.com"
    }),
    websocketx.WithReadBufferSize(4096),
    websocketx.WithWriteBufferSize(4096),
    websocketx.WithHandshakeTimeout(10*time.Second),
    websocketx.WithMaxMessageSize(1<<20), // 1MB
))
```

## 关闭原因常量

标准 WebSocket 关闭原因，便于监控统计与前端提示。

```go
const (
    CloseReasonHeartbeatTimeout CloseReason = "heartbeat_timeout"  // Ping/Pong 超时
    CloseReasonServerShutdown    CloseReason = "server_shutdown"    // 优雅关闭
    CloseReasonInvalidMessage    CloseReason = "invalid_message"    // 非法报文
    CloseReasonAuthDenied        CloseReason = "auth_denied"        // 权限拦截
    CloseReasonKicked            CloseReason = "kicked"             // 被踢下线
)
```

可通过 `WithCloseReason` 自定义 Kick 和 Shutdown 时的关闭原因：

```go
hub := websocketx.NewHub(10000,
    websocketx.WithCloseReason("kicked_by_admin", "maintenance"),
)
```

## 监控埋点

强类型事件枚举 + 标准化标签生成：

```go
hub := websocketx.NewHub(10000, websocketx.WithMetrics(
    func(event string, labels map[string]string) {
        switch event {
        case "connect":
            // labels: event=connect, session_id, user_id
        case "disconnect":
            // labels: event=disconnect, session_id, user_id
        case "message":
            // labels: event=message, session_id, cmd, duration_ms, error
        case "broadcast":
            // labels: event=broadcast, count, error
        case "broadcast_room":
            // labels: event=broadcast_room, room_id, count, error
        }
    },
    nil, nil, nil, // onDisconnect, onMessage, onBroadcast 可留 nil
))
```

```go
// MetricEvent 常量
const (
    MetricEventConnect     MetricsEvent = "connect"
    MetricEventDisconnect  MetricsEvent = "disconnect"
    MetricEventMessage     MetricsEvent = "message"
    MetricEventBroadcast   MetricsEvent = "broadcast"
    MetricEventBroadcastRoom MetricsEvent = "broadcast_room"
)

// StandardMetricLabels 生成标准化标签
labels := websocketx.StandardMetricLabels(websocketx.MetricEventConnect, map[string]string{
    "session_id": "s_001",
    "user_id":    "u_001",
})
// labels["event"] == "connect"
```

## 心跳保活

通过 `WithHeartbeat(interval, pongWait)` 开启，不传则关闭。

```go
hub := websocketx.NewHub(10000, websocketx.WithHeartbeat(30*time.Second, 60*time.Second))
// 每 30 秒发送 Ping，60 秒内未收到 Pong 则断开连接
```

内建实现，连接断开时自动清理 goroutine，无需外部定时组件。

## 优雅关闭

```go
hub.Shutdown(context.Background())
// 关闭所有现有连接
```

## 模块依赖

| 依赖 | 用途 |
|------|------|
| gorilla/websocket v1.5.3 | WebSocket 底层实现 |

> 注：v1.x 已归档但广泛使用，上层通过 `wsConn` 接口隔离，未来可替换为活跃库。
