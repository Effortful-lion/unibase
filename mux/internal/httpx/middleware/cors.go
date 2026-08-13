package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSOption CORS 中间件配置。
type CORSOption func(*corsConfig)

type corsConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

func defaultCORSConfig() *corsConfig {
	return &corsConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{},
		AllowCredentials: false,
		MaxAge:           86400,
	}
}

// WithAllowOrigins 设置允许的来源。
func WithAllowOrigins(origins ...string) CORSOption {
	return func(c *corsConfig) {
		c.AllowOrigins = origins
	}
}

// WithAllowMethods 设置允许的 HTTP 方法。
func WithAllowMethods(methods ...string) CORSOption {
	return func(c *corsConfig) {
		c.AllowMethods = methods
	}
}

// WithAllowHeaders 设置允许的请求头。
func WithAllowHeaders(headers ...string) CORSOption {
	return func(c *corsConfig) {
		c.AllowHeaders = headers
	}
}

// WithAllowCredentials 设置是否允许携带凭证。
func WithAllowCredentials(allow bool) CORSOption {
	return func(c *corsConfig) {
		c.AllowCredentials = allow
	}
}

// WithMaxAge 设置预检请求缓存时间（秒）。
func WithMaxAge(seconds int) CORSOption {
	return func(c *corsConfig) {
		c.MaxAge = seconds
	}
}

// CORS 返回 CORS 中间件。
func CORS(opts ...CORSOption) gin.HandlerFunc {
	cfg := defaultCORSConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" {
			c.Next()
			return
		}

		allowed := false
		for _, o := range cfg.AllowOrigins {
			if o == "*" || o == origin {
				allowed = true
				break
			}
		}

		if !allowed {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		if cfg.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		c.Header("Access-Control-Allow-Methods", joinStrings(cfg.AllowMethods))
		c.Header("Access-Control-Allow-Headers", joinStrings(cfg.AllowHeaders))
		if len(cfg.ExposeHeaders) > 0 {
			c.Header("Access-Control-Expose-Headers", joinStrings(cfg.ExposeHeaders))
		}
		c.Header("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func joinStrings(ss []string) string {
	return strings.Join(ss, ", ")
}
