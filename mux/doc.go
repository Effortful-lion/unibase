// Package mux 是基于 Gin + WebSocket 的 Web 应用开发框架。
//
// mux 聚合 httpx（HTTP 工具箱）和 websocketx（WebSocket 工具箱），
// 提供统一的 Engine 入口，让 Web 应用通过一个框架同时处理 HTTP 和 WebSocket 请求。
//
// 模块职责：
//   - Engine  — 核心入口，管理 HTTP 引擎和 WebSocket Hub
//   - 中间件编排 — 透传 httpx 中间件到 Gin，WebSocket 中间件走 websocketx Router
//   - 优雅关闭 — 同时关闭 HTTP Server 和 WebSocket Hub
//
// 设计原则：
//   - 不重新发明轮子：HTTP 侧完全透传 httpx 工具，WebSocket 侧完全透传 websocketx
//   - 框架无关接口：HTTP 引擎暴露为 *gin.Engine，WebSocket 路由暴露为 *websocketx.Router
//   - 最小抽象：mux 只做"粘合层"，不引入额外的框架概念
//
// 快速开始：
//
//	engine := mux.New()
//
//	// HTTP 路由
//	httpEngine := engine.HTTP()
//	httpEngine.GET("/api/health", func(c *gin.Context) {
//	    httpx.ResponseOK(c, "ok")
//	})
//	httpEngine.Use(httpx.JWT([]byte("secret")))
//
//	// WebSocket Cmd 路由
//	wsRouter := engine.WS()
//	websocketx.Cmd(wsRouter, "chat.message", handler.ChatMessage)
//
//	// 自定义 WebSocket 路径
//	engine := mux.New(mux.WithWebSocketPath("/socket"))
//
//	// 启动（阻塞）
//	engine.Run(":8080")
//
// 优雅关闭：
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	engine.Shutdown(ctx)
package mux
