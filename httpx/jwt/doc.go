// Package jwt 提供 JWT 完整链路能力：生成、解析、验证、Gin 中间件。
//
// 核心逻辑委托给 tools/auth/jwt（纯 Go 实现，无框架依赖），本包在其上封装
// Gin 中间件及 HTTP 相关的便捷方法。
//
// 快速开始：
//
//	// 创建解析器
//	parser := jwt.NewHMACParser([]byte("secret"))
//	claims, err := parser.Parse(token)
//	tokenString, err := parser.Sign(jwt.NewClaims("uid", "name", time.Hour), secret)
//
//	// Gin 中间件
//	r.Use(jwt.JWTMiddleware([]byte("secret")))
//	// 在 handler 中提取 claims
//	claims, err := jwt.ClaimsFromContext(c)
//
// 能力：Parser（Parse / Sign / Verify）、Claims 结构、JWT 中间件
// （token 提取、验证、注入 context）、ClaimsFromContext 提取。
package jwt
