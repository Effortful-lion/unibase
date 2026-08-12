package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/Effortful-lion/httpx/response"
	"github.com/Effortful-lion/unibase/tools/limiter"
	"github.com/gin-gonic/gin"
)

// RateLimiter 基于令牌桶的限流器。
// 按 key 隔离限流，自动清理超过 30 分钟未访问的 limiter。
type RateLimiter struct {
	limiters map[string]*limiterEntry
	mu       sync.Mutex
	rate     float64
	burst    int
	stopCh   chan struct{}
	doneCh   chan struct{}
}

type limiterEntry struct {
	limiter    *limiter.Limiter
	lastAccess time.Time
}

// NewRateLimiter 创建限流器。
// rate: 每秒产生的令牌数
// burst: 桶容量（允许的突发请求数）
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*limiterEntry),
		rate:     rate,
		burst:    burst,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop 停止清理 goroutine。
// 调用后 RateLimiter 仍可继续使用，但不再自动清理过期 limiter。
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.stopCh:
		// already stopped
	default:
		close(rl.stopCh)
	}
	<-rl.doneCh
}

func (rl *RateLimiter) cleanupLoop() {
	defer close(rl.doneCh)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			threshold := time.Now().Add(-30 * time.Minute)
			for key, entry := range rl.limiters {
				if entry.lastAccess.Before(threshold) {
					delete(rl.limiters, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// getLimiter 按 key 获取限流器，不存在则创建。
func (rl *RateLimiter) getLimiter(key string) *limiter.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if entry, exists := rl.limiters[key]; exists {
		entry.lastAccess = time.Now()
		return entry.limiter
	}

	l := limiter.NewLimiter(rl.rate, rl.burst)
	rl.limiters[key] = &limiterEntry{limiter: l, lastAccess: time.Now()}
	return l
}

// RateLimitOption 限流中间件配置。
type RateLimitOption func(*rateLimitConfig)

type rateLimitConfig struct {
	keyFunc func(*gin.Context) string
}

func defaultRateLimitConfig() *rateLimitConfig {
	return &rateLimitConfig{
		keyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
	}
}

// WithRateLimitKey 自定义限流 key 提取函数（默认按 IP）。
func WithRateLimitKey(fn func(*gin.Context) string) RateLimitOption {
	return func(c *rateLimitConfig) {
		c.keyFunc = fn
	}
}

// RateLimit 返回限流中间件。
// 超过限流阈值时返回 429。
func RateLimit(limiter *RateLimiter, opts ...RateLimitOption) gin.HandlerFunc {
	cfg := defaultRateLimitConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		key := cfg.keyFunc(c)
		if !limiter.getLimiter(key).Allow() {
			c.Header("Retry-After", "1")
			response.ResponseFail(c, http.StatusTooManyRequests, "10429", "rate limit exceeded")
			return
		}
		c.Next()
	}
}
