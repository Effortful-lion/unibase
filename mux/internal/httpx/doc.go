// Package httpx 是 Gin Web 开发者的 HTTP 工具箱。
//
// 模块划分：
//
//	code/      — 业务状态码（BusinessCode）
//	response/  — 统一响应结构 + ResponseWriter 接口
//	params/    — 参数绑定 + 集成校验
//	jwt/       — JWT 令牌生成、解析、Gin 中间件
//	middleware/ — 常用 Gin 中间件（日志、CORS、限流、Panic 恢复）
//	client/    — 出站 HTTP 请求 Builder
//	server.go  — 服务启动 + 优雅关闭（SIGTERM/SIGINT）
//
// 设计原则：
//   - 只有 middleware 层绑 Gin（返回 gin.HandlerFunc）
//   - 其余包尽量框架无关
//   - Response.Code 使用 BusinessCode 类型（非 string）
//
// 快速开始：
//
//	// 响应
//	w := httpx.Gin(c)
//	w.JSON(200, response.NewResponse(data))
//
//	// 参数绑定
//	params.MustBindJSON(c, &req)
//
//	// JWT
//	r.Use(httpx.JWT([]byte("secret")))
//
//	// 中间件
//	r.Use(middleware.Log())
//	r.Use(middleware.CORS())
//	r.Use(middleware.JWT([]byte("secret")))
//	r.Use(middleware.Panic())
//
//	// 出站请求
//	res := client.Get().URL(url).Do(ctx)
//	res.JSON(&result)
package httpx
