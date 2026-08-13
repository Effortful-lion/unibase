// Package middleware 提供 Gin 常用中间件。
//
// 快速开始：
//
//	// 日志中间件（可注入 logx logger）
//	r.Use(middleware.Log(middleware.WithLogger(myLogger)))
//
//	// CORS 中间件
//	r.Use(middleware.CORS(
//	    middleware.WithAllowOrigins("http://localhost:3000"),
//	    middleware.WithAllowCredentials(true),
//	))
//
//	// 限流中间件
//	limiter := middleware.NewRateLimiter(10, 100)
//	r.Use(middleware.RateLimit(limiter))
//
//	// JWT 中间件
//	r.Use(middleware.JWT([]byte("secret")))
//
//	// Panic 恢复中间件
//	r.Use(middleware.Panic())
//
// 能力：Log（可插拔 logger）、CORS（Option 模式配置）、
// RateLimit（令牌桶 + 自动清理过期 limiter）、JWT（薄包装 jwt 包）、
// Panic（捕获 panic + 日志 + 500 响应）。
package middleware
