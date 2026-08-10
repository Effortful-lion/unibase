// Package jwt 提供 JWT 完整链路能力：生成、解析、验证、Gin 中间件。
//
// 快速开始：
//
//	parser := jwt.NewHMACParser([]byte("secret"))
//	claims, err := parser.Parse(token)
//	tokenString, err := parser.Sign(jwt.NewClaims("uid", "name", time.Hour), secret)
//
//	// Gin 中间件
//	r.Use(jwt.Middleware(jwt.WithParser(parser)))
//	// 或快捷方式
//	r.Use(jwt.JWTMiddleware([]byte("secret")))
//
// 能力：Parser（Parse / Sign / Verify）、Claims 结构、自定义 Claims 扩展、
// JWT 中间件（token 提取、验证、注入 context）、ClaimsFromContext 提取。
package jwt
