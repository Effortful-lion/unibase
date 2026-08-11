package limiter

import (
	"context"
	"errors"
	"time"

	"golang.org/x/time/rate"
)

// Limiter 令牌桶限速器。
//
// 基于令牌桶算法，以固定速率 replenish 令牌，允许一定程度的突发流量。
// 线程安全，可多 goroutine 并发调用。
//
// 示例：
//
//	// 每秒 100 个令牌，突发上限 10
//	l := limiter.NewLimiter(100, 10)
//
//	if l.Allow() {
//	    // 放行
//	}
//
//	// 阻塞等待
//	if err := l.Wait(ctx); err != nil {
//	    return err
//	}
//
//	// 预留令牌
//	wait := l.Reserve()
//	time.Sleep(wait)
type Limiter struct {
	inner *rate.Limiter
}

// NewLimiter 创建令牌桶限速器。
//
// r: 每秒生成的令牌数，必须 > 0。
// burst: 令牌桶容量（最大突发量），必须 > 0。
func NewLimiter(r float64, burst int) *Limiter {
	return &Limiter{
		inner: rate.NewLimiter(rate.Limit(r), burst),
	}
}

// Allow 非阻塞尝试获取 1 个令牌。
// 有可用令牌时返回 true，否则返回 false。
func (l *Limiter) Allow() bool {
	return l.inner.Allow()
}

// AllowN 非阻塞尝试获取 n 个令牌。
// n 必须 > 0。有足够令牌时返回 true，否则返回 false。
func (l *Limiter) AllowN(n int) bool {
	if n <= 0 {
		return false
	}
	return l.inner.AllowN(time.Now(), n)
}

// Wait 阻塞等待直到获取 1 个令牌。
// ctx 控制等待超时或取消，返回 ctx 错误或限速器关闭时的错误。
func (l *Limiter) Wait(ctx context.Context) error {
	return l.WaitN(ctx, 1)
}

// WaitN 阻塞等待直到获取 n 个令牌。
// n 必须 > 0。ctx 控制等待超时或取消。
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	if n <= 0 {
		return errors.New("limiter: n must be positive")
	}
	return l.inner.WaitN(ctx, n)
}

// Reserve 预留 1 个令牌，返回需要等待的时间。
// 返回 0 表示令牌立即可用。
func (l *Limiter) Reserve() time.Duration {
	return l.ReserveN(1)
}

// ReserveN 预留 n 个令牌，返回需要等待的时间。
// n 必须 > 0。返回 0 表示令牌立即可用。
func (l *Limiter) ReserveN(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	r := l.inner.ReserveN(time.Now(), n)
	if r.Delay() < 0 {
		return 0
	}
	return r.Delay()
}

// Rate 返回每秒令牌生成速率。
func (l *Limiter) Rate() float64 {
	return float64(l.inner.Limit())
}

// Burst 返回令牌桶容量。
func (l *Limiter) Burst() int {
	return l.inner.Burst()
}
