package middleware

import (
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/gin-gonic/gin"
)

// Log 返回日志中间件，用 logx 替换 Gin 内置 logger。
// logger 为 nil 时降级为 logx.Default()。
func Log(logger *logx.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = logx.Default().Module("httpx")
	}

	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		clientIP := c.ClientIP()

		fields := logx.Fields{
			"method":    method,
			"path":      path,
			"status":    status,
			"latency":   latency.String(),
			"client_ip": clientIP,
		}

		if errs := c.Errors.String(); errs != "" {
			fields["error"] = errs
		}

		if status >= 400 {
			logger.Error("HTTP request failed", fields)
		} else {
			logger.Info("HTTP request", fields)
		}
	}
}
