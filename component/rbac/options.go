package auth

import (
	"github.com/Effortful-lion/unibase/logx"
)

// Cache 缓存主体的权限集，减少每次请求时的 Redis 查询。
type Cache interface {
	// Get 获取缓存的权限列表。
	// 第二个返回值表示缓存是否存在。
	Get(subjectID string) ([]Permission, bool)

	// Set 缓存主体的权限列表。
	Set(subjectID string, perms []Permission, ttl int64)

	// Invalidate 清除主体的权限缓存。
	// 当角色或权限变更时应调用此方法。
	Invalidate(subjectID string)
}

// enforcerOptions 配置 Enforcer 行为。
type enforcerOptions struct {
	logger *logx.Logger
	cache  Cache
}

func defaultEnforcerOptions() enforcerOptions {
	return enforcerOptions{
		logger: logx.Module("auth"),
	}
}

// Option 配置 Enforcer 的可选参数。
type Option func(*enforcerOptions)

// WithLogger 注入自定义日志器。
func WithLogger(logger *logx.Logger) Option {
	return func(o *enforcerOptions) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithCache 注入权限缓存层。
// 适合高并发场景，减少 Redis 查询次数。
func WithCache(cache Cache) Option {
	return func(o *enforcerOptions) {
		o.cache = cache
	}
}
