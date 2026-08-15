// Package mux 是三聚合 Web 框架：HTTP + WebSocket（RPC  planned）。
//
// 设计哲学：
//   - 单一入口：用户只需 import mux 一个包即可构建完整服务
//   - 传输无关：Context 统一封装 HTTP 和 WebSocket 的差异
//   - Pipeline 核心：所有请求共享同一套中间件链，RESTful 和 Cmd 两种路由模式
//   - 分层架构：Engine(生命周期) → Transport(传输) → Pipeline(消息处理) → Handler(业务)
//
// 核心类型：
//   - Engine    — 框架入口，管理 Transport、Pipeline、SessionManager、ClusterManager
//   - Pipeline  — 中间件链 + RESTful 路由 + Cmd 路由
//   - Context   — 统一消息上下文，屏蔽 HTTP/WebSocket 差异
//   - Session   — 统一会话接口（HTTP per-request / WebSocket 长连接）
//   - Message   — 传输无关的消息格式
//   - Handler   — 消息处理函数签名
//   - Middleware — 中间件签名
//
// 两种路由模式：
//
//	RESTful: pipeline.GET("/api/users", listUsers)
//	Cmd:     pipeline.Cmd("user.create", createUser)
//	         HTTP 入口: POST /v1/cmd {cmd: "user.create", ...}
//	         WS 入口:    {cmd: "user.create", ...}
//
// WebSocket 仅支持 Cmd 模式，不支持 RESTful。
//
// 中间件体系：
//   - Pipeline 中间件（mux.Middleware）：覆盖所有入口（REST + HTTP Cmd + WS Cmd），推荐使用
//   - gin 中间件（gin.HandlerFunc）：仅覆盖 HTTP REST 路由，通过 engine.Use() 注册
//   - 两者可共存：gin 中间件在外层（HTTP only），Pipeline 中间件在内层（统一）
//
// 快速开始：
//
//	engine := mux.New(mux.WithMaxWebSocketConn(10000))
//	pipeline := mux.NewPipeline(engine)
//	pipeline.Use(mux.LogMiddleware(logger), mux.RecoverMiddleware)
//	pipeline.GET("/api/health", func(ctx *mux.Context) error {
//	    return ctx.ReplyOK(map[string]string{"status": "ok"})
//	})
//	pipeline.Cmd("user.create", func(ctx *mux.Context) error {
//	    var req struct{ Name string }
//	    ctx.Bind(&req)
//	    return ctx.ReplyOK(req)
//	})
//	engine.UsePipeline(pipeline)
//	engine.Run(":8080")
package mux
