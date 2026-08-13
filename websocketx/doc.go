// Package websocketx 基于 gorilla/websocket 的 WebSocket 服务端开发工具包。
//
// 核心能力：
//   - Cmd 模式消息路由（router.Cmd 注册，支持路由级中间件）
//   - 连接 Session 管理（ID / UserID / Meta / State / Rooms）
//   - 中间件链（全局 + 路由级，内置 RecoverMiddleware Panic 兜底）
//   - 心跳保活（内建 Ping/Pong，Pong 超时自动断开）
//   - 并发限流（semaphore 连接数门控）
//   - 房间分组广播（BroadcastToRoom / JoinRoom / LeaveRoom / ListRoomUsers）
//   - 用户维度推送（BroadcastToUser）
//   - 踢下线（Hub.Kick）
//   - 监控埋点（强类型 MetricsEvent 枚举 + StandardMetricLabels）
//   - 关闭原因常量（CloseReason 枚举）
//   - 可扩展编解码（MessageCodec 接口）
//   - 异步写缓冲 + 背压丢弃（Conn.sendCh + writePump）
//
// 协议约定：
//
//	所有消息均为 JSON 格式：
//	请求：{ "cmd": "file.upload", "meta": {}, "body": { "fileName": "a.jpg" } }
//	响应：{ "cmd": "file.upload", "meta": { "code": "10200" }, "body": { "url": "..." } }
//
// 快速开始：
//
//	// 1. 创建 Hub（最多 10000 并发连接，开启心跳，限制消息大小）
//	hub := websocketx.NewHub(10000,
//	    websocketx.WithHeartbeat(30*time.Second, 60*time.Second),
//	    websocketx.WithMaxMessageSize(1<<20),
//	    websocketx.WithCloseReason("kicked", "shutdown"),
//	)
//
//	// 2. 创建 Router，注册路由和中间件
//	router := websocketx.NewRouter()
//	router.Use(websocketx.RecoverMiddleware)
//	router.Cmd("chat.message", handler.ChatMessage)
//	router.Cmd("file.upload", handler.FileUpload, authMiddleware)
//
//	// 3. 挂载到 HTTP
//	http.Handle("/ws", websocketx.Upgrade(hub, router.Handle))
//
// 便捷方法：
//
//	msg.Bind(&req)                            // 解析 Body 到结构体
//	session.Conn().Reply(ctx, msg, "10500", err)  // 统一错误回复
//	session.JoinRoom("room_001")               // 加入房间
//	hub.BroadcastToRoom(ctx, "room_001", msg)  // 房间广播
//	hub.Kick("user_001")                       // 踢下线
//	hub.ListRoomUsers("room_001")              // 获取房间用户列表
package websocketx
