// Package middleware 提供 Gin 常用中间件。
//
// 快速开始：
//
//	r.Use(middleware.CORS(
//	    middleware.WithAllowOrigins("http://localhost:3000"),
//	    middleware.WithAllowCredentials(true),
//	))
//
//	limiter := middleware.NewRateLimiter(rate.Limit(10), 100)
//	r.Use(middleware.RateLimit(limiter))
//
//	r.Use(middleware.Metrics())
//	r.GET("/metrics", middleware.MetricsHandler())
//
// 能力：CORS（Option 模式配置）、RateLimit（令牌桶 + 自动清理过期 limiter）、
// Metrics（Prometheus 请求计数 + 延迟直方图）。
package middleware
