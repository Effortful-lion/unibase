// Package params 提供 Gin 请求参数增强能力。
//
// 快速开始：
//
//	id := c.QueryInt("id")
//	active := c.QueryBoolDefault("active", false)
//
//	params.MustBindJSON(c, &req)  // 绑定 + struct tag 校验，失败 Abort 400
//
// 能力：Query 参数类型转换（Int / Bool / Float / Time / Slice），
// Bind 系列函数（强集成 validator），自定义校验规则注册。
package params
