// Package swagger 提供 Swagger UI 集成能力。
//
// 基于 swaggo 工具链生成 API 文档后，通过本包一行代码挂载 Swagger UI。
//
// 前置条件：项目中已使用 swaggo/swag 生成 docs（swag init）。
//
// 快速开始：
//
//	// 1. 在 main.go 顶部添加 swagger 注释，然后运行 swag init
//	// 2. 挂载路由
//	r := gin.Default()
//	swagger.Setup(r, "/api/v1", "/swagger/doc.json")
//
// 能力：Setup（挂载 Swagger UI 路由）。
package swagger
