# mux

基于 Gin + WebSocket 的 Web 应用开发框架，聚合 [httpx](httpx/README.md) 和 [websocketx](websocketx/README.md) 两个独立模块，提供统一的 `Engine` 入口。

## 快速开始

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Effortful-lion/unibase/mux"
	"github.com/Effortful-lion/unibase/websocketx"
	"github.com/gin-gonic/gin"
)

func main() {
	engine := mux.New(
		mux.WithMaxWebSocketConn(10000),
		mux.WithReadTimeout(10*time.Second),
		mux.WithWriteTimeout(10*time.Second),
	)

	// ── HTTP 路由 ──────────────────────────────────────────
	httpEngine := engine.HTTP()
	httpEngine.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"ws_conn": engine.WSHub().Count(),
		})
	})

	// ── WebSocket Cmd 路由 ─────────────────────────────────
	wsRouter := engine.WS()
	wsRouter.Cmd("chat.echo", func(ctx context.Context, session *websocketx.Session, msg *websocketx.CmdMessage) error {
		var req struct{ Message string `json:"message"` }
		if err := msg.Bind(&req); err != nil {
			return err
		}
		return session.Conn().Write(ctx, &websocketx.CmdMessage{
			Cmd:  msg.Cmd,
			Meta: map[string]interface{}{"code": "10200"},
			Body: json.RawMessage(`{"reply":"` + req.Message + `"}`),
		})
	})

	// ── 启动服务 ───────────────────────────────────────────
	engine.Run(":8080")
}
```

## 核心 API

### Engine 创建

```go
engine := mux.New(opts...)
```

| Option | 说明 |
|--------|------|
| `WithReadTimeout(d)` | HTTP 读取超时 |
| `WithWriteTimeout(d)` | HTTP 写入超时 |
| `WithIdleTimeout(d)` | HTTP 空闲超时 |
| `WithMaxWebSocketConn(n)` | WebSocket 最大并发连接数，0=不限 |
| `WithWebSocketPath(path)` | WebSocket 升级端点路径，默认 "/ws" |
| `WithHTTPMiddleware(mws...)` | 全局 HTTP 中间件 |

### HTTP 路由

```go
engine.HTTP().GET("/api/users", listUsers)
engine.HTTP().POST("/api/users", createUser)
engine.HTTP().Use(httpx.JWT([]byte("secret")))
```

### WebSocket 路由

```go
wsRouter := engine.WS()
wsRouter.Cmd("ping", handler)
wsRouter.Use(websocketx.RecoverMiddleware)
```

### WebSocket Hub 操作

```go
hub := engine.WSHub()
hub.Broadcast(ctx, msg)                    // 全量广播
hub.BroadcastToRoom(ctx, "room_001", msg)  // 房间广播
hub.GetSession(id)                         // 查询 Session
hub.Count()                                // 连接数
```

### 服务生命周期

```go
// 启动（阻塞）
engine.Run(":8080")

// 优雅关闭
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
engine.Shutdown(ctx)
```

## 中间件编排

| 能力 | HTTP 侧 | WebSocket 侧 |
|------|---------|--------------|
| 日志 | `httpx/middleware.Log()` | 自定义 WebSocket 中间件 |
| CORS | `httpx/middleware.CORS()` | `websocketx.Upgrade(..., WithCheckOrigin)` |
| 限流 | `httpx/middleware.RateLimit()` | Hub 信号量连接数上限 |
| JWT | `httpx/jwt.JWTMiddleware()` | 自定义 WebSocket 中间件 |
| Panic 恢复 | Gin 默认 Recovery | `websocketx.RecoverMiddleware` |

## 集成 Demo

`engine_test.go` 中的 `TestChatDemo` 函数演示了完整 Chat Server 的构建方式：

```bash
go test -v -run TestChatDemo -timeout 30s
```

Demo 覆盖的能力：
- HTTP `GET /api/health` — 健康检查
- HTTP `GET /api/rooms` — 房间列表
- WebSocket `chat.join` — 加入房间
- WebSocket `chat.message` — 发送消息 + 房间广播
- WebSocket `chat.leave` — 离开房间
- WebSocket `ping` — 心跳检测

## 与现有模块的关系

- **不修改** httpx 和 websocketx 的任何代码
- mux 是纯组合层，不引入新的框架抽象
- httpx 和 websocketx 可独立于 mux 使用

## 模块结构

```
mux/
├── doc.go              # 包级文档
├── engine.go           # Engine 类型 + API
├── engine_test.go      # 集成测试（含 TestChatDemo 演示）
├── go.mod
└── go.sum
```
