// Package code 提供业务状态码类型和预定义常量。
//
// BusinessCode 底层为 string，JSON 序列化时输出为字符串。
// 空字符串表示成功，非空表示业务错误。
//
// 快速开始：
//
//	// 成功响应
//	resp := response.NewResponse(data)
//	// 等价于 response.NewResponse(code.OK, "success", data)
//
//	// 错误响应
//	errResp := response.NewError(code.BadRequest, "参数错误")
//
// 预定义常量：OK / BadRequest / Unauthorized / Forbidden / NotFound /
// TooManyRequests / InternalError。
// 业务可自行扩展：const MyCustomCode code.BusinessCode = "10999"
package code
