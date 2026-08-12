// Package jwt 提供 JWT 完整链路能力：生成、解析、验证、Gin 中间件。
//
// 核心逻辑委托给 tools/auth/jwt（纯 Go 实现，无框架依赖），本包在其上封装
// Gin 中间件及 HTTP 相关的便捷方法。
//
// Claims 类型：
//   - Claims：标准结构体（UserID / Username / Role / Extra），适合大多数场景
//   - MapClaims：任意键值对，适合需要自定义字段的场景
//   - 自定义 struct：嵌入 gjwt.RegisteredClaims 即可扩展
//
// Extra 扩展字段：
//
//	claims.SetExtra("dept", "engineering")
//	dept, _ := claims.GetExtra("dept")
//
// 快速开始：
//
//	// 标准 struct claims
//	parser := jwt.NewHMACParser([]byte("secret"))
//	claims := jwt.NewClaims("uid", "name", time.Hour)
//	token, err := parser.Sign(claims, secret)
//	parsed, err := parser.Parse(token)
//	dept, _ := parsed.GetExtra("dept")
//
//	// Map claims（自定义字段）
//	mc := jwt.NewMapClaims(map[string]any{
//	    "user_id": "uid",
//	    "dept": "engineering",
//	}, time.Hour)
//	token, err := parser.Generate(mc, secret)
//
//	// Gin 中间件
//	r.Use(jwt.JWTMiddleware([]byte("secret")))
//	claims, err := jwt.ClaimsFromContext(c)
//
// 能力：Parser（Sign / Generate / Parse / ParseMap / Verify）、Claims 结构
// （含 Extra 扩展）、MapClaims 构造、JWT 中间件（token 提取、验证、注入 context）、
// ClaimsFromContext 提取。
package jwt
