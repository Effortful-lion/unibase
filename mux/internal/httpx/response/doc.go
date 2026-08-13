// Package response 定义框架无关的统一响应结构和写入接口。
//
// 核心设计：
//   - Response 结构体不含任何框架依赖，可直接 JSON 序列化
//   - ResponseWriter 接口抽象写入能力，各框架提供自己的实现
//   - 当前提供 Gin 实现（GinResponseWriter）
//
// 快速开始：
//
//	// 写入成功响应
//	w := response.Gin(c)
//	w.JSON(200, response.NewResponse(user))
//
//	// 写入错误响应
//	w.JSON(400, response.NewError(code.BadRequest, "参数错误"))
//
//	// 快捷方式
//	response.ResponseOK(c, data)
//	response.ResponseFail(c, http.StatusBadRequest, code.BadRequest, "参数错误")
//
// 能力：Response 结构、ResponseWriter 接口、GinResponseWriter 实现。
package response
