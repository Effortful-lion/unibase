package cache

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	lru "github.com/hashicorp/golang-lru/v2"
)

// entry 是 LRU 缓存中存储的 entry，包含实际值和 TTL timer。
type entry[V any] struct {
	value  V
	expire *time.Timer
}

// LRUConfig 配置本地 LRU 缓存。
type LRUConfig struct {
	// MaxEntries 最大缓存条目数，必须大于 0。
	MaxEntries int

	// OnEvicted 条目被淘汰时的回调，可 nil。
	// 注意：回调在 LRU 锁外执行，但 TTL timer 会在回调返回前停止。
	OnEvicted func(key string, value any)
}

// LRUCache 是线程安全的本地 LRU 缓存，支持按条目 TTL 过期。
//
// 设计要点：
//   - 淘汰策略基于 LRU（最近最少使用），由 hashicorp/golang-lru/v2 提供。
//   - TTL 过期通过每个 entry 独立的 time.Timer 实现，避免轮询开销。
//   - 命中率通过原子计数器实时统计。
type LRUCache struct {
	mu       sync.RWMutex
	cache    *lru.Cache[string, entry[any]]
	config   LRUConfig
	hit      atomic.Uint64
	total    atomic.Uint64
	stopOnce sync.Once
	closed   atomic.Bool
	sfGroup  singleflight.Group
}

// === 导出方法 ===

// NewLRUCache 创建本地 LRU 缓存。
//
// 参数:
//   - cfg: 配置，MaxEntries 为必填项。
//
// 返回的缓存立即可用，调用 Close 可释放内部资源。
func NewLRUCache(cfg LRUConfig) (*LRUCache, error) {
	if cfg.MaxEntries <= 0 {
		return nil, errors.New("cache: MaxEntries must be positive")
	}

	// 包装用户传入的 OnEvicted 回调，在调用前停止 TTL timer。
	// LRU 库在内部锁之外调用淘汰回调，因此在此处安全访问 timer。
	var lruOnEvict func(string, entry[any])
	if cfg.OnEvicted != nil {
		wrapped := cfg.OnEvicted
		lruOnEvict = func(key string, ent entry[any]) {
			if ent.expire != nil {
				ent.expire.Stop()
			}
			wrapped(key, ent.value)
		}
	}

	inner, err := lru.NewWithEvict[string, entry[any]](cfg.MaxEntries, lruOnEvict)
	if err != nil {
		return nil, err
	}

	return &LRUCache{
		cache:  inner,
		config: cfg,
	}, nil
}

// Get 获取缓存值。
//
// 返回值:
//   - value: 缓存值，未命中时返回零值。
//   - ok: 是否命中。
func (c *LRUCache) Get(key string) (value any, ok bool) {
	if c == nil {
		return nil, false
	}

	c.total.Add(1)
	c.mu.RLock()
	ent, hit := c.cache.Get(key)
	c.mu.RUnlock()

	if !hit {
		return nil, false
	}

	c.hit.Add(1)
	return ent.value, true
}

// Put 写入缓存。
//
//   - 如果 key 已存在，会先取消旧 TTL timer，再写入新值和新的 TTL。
//   - ttl <= 0 表示永不过期（仅受 LRU 容量限制）。
//   - 关闭后的缓存不接受写入，静默返回。
func (c *LRUCache) Put(key string, value any, ttl time.Duration) {
	if c == nil || c.closed.Load() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	old, exists := c.cache.Peek(key)
	if exists && old.expire != nil {
		old.expire.Stop()
	}

	var expire *time.Timer
	if ttl > 0 {
		expire = time.AfterFunc(ttl, func() {
			c.mu.Lock()
			_, ok := c.cache.Peek(key)
			if ok {
				c.cache.Remove(key)
			}
			c.mu.Unlock()
		})
	}

	c.cache.Add(key, entry[any]{
		value:  value,
		expire: expire,
	})
}

// GetOrSet 先查缓存，未命中则调用 loader 计算值并写入缓存后返回。
//
// 同一 key 的并发请求会通过 singleflight 合并，仅执行一次 loader。
// loader 返回的错误不会被缓存，后续请求会重试。
//
// 参数:
//   - key: 缓存键。
//   - loader: 缓存未命中时的值计算函数，返回 (值, 错误)。
//   - ttl: 写入缓存的过期时间，<= 0 表示永不过期。
//
// 返回值: (值, 错误)。缓存命中时直接返回缓存值，loader 不会被调用。
func (c *LRUCache) GetOrSet(key string, loader func() (any, error), ttl time.Duration) (any, error) {
	if c == nil || loader == nil {
		return nil, nil
	}

	// 先查缓存，命中直接返回。
	if val, ok := c.Get(key); ok {
		return val, nil
	}

	// 未命中，通过 singleflight 合并并发请求。
	val, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		return loader()
	})
	if err != nil {
		return nil, err
	}

	c.Put(key, val, ttl)
	return val, nil
}

// MGet 批量获取。keys 和 values 长度必须一致，values 的每个元素必须是可写的指针。
// 返回一个 bool 切片，表示每个 key 是否命中（命中时 values[i] 已被赋值）。
func (c *LRUCache) MGet(keys []string, values []any) []bool {
	if c == nil || len(keys) == 0 {
		return make([]bool, len(keys))
	}

	results := make([]bool, len(keys))
	for i, key := range keys {
		val, ok := c.Get(key)
		if ok {
			values[i] = val
			results[i] = true
		}
	}
	return results
}

// MSet 批量写入。所有条目使用相同的 ttl。
func (c *LRUCache) MSet(items map[string]any, ttl time.Duration) {
	if c == nil {
		return
	}
	for key, value := range items {
		c.Put(key, value, ttl)
	}
}

// Remove 移除缓存条目，返回被移除的值。
func (c *LRUCache) Remove(key string) (value any, ok bool) {
	if c == nil {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	ent, ok := c.cache.Get(key)
	if !ok {
		return nil, false
	}

	c.cache.Remove(key)
	if ent.expire != nil {
		ent.expire.Stop()
	}

	return ent.value, true
}

// Contains 检查 key 是否存在（不更新 LRU 顺序）。
func (c *LRUCache) Contains(key string) bool {
	if c == nil {
		return false
	}

	c.mu.RLock()
	ok := c.cache.Contains(key)
	c.mu.RUnlock()

	return ok
}

// Len 返回当前缓存条目数。
func (c *LRUCache) Len() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	n := c.cache.Len()
	c.mu.RUnlock()

	return n
}

// MaxEntries 返回最大容量。
func (c *LRUCache) MaxEntries() int {
	if c == nil {
		return 0
	}
	return c.config.MaxEntries
}

// HitRate 返回当前统计周期的命中率和当前长度。
//
// 返回值: (命中率百分比 0-100, 当前条目数)。
// 当无查询时命中率为 100。
func (c *LRUCache) HitRate() (hitRate int, length int) {
	if c == nil {
		return 100, 0
	}

	hit := c.hit.Swap(0)
	total := c.total.Swap(0)

	c.mu.RLock()
	length = c.cache.Len()
	c.mu.RUnlock()

	if total == 0 {
		return 100, length
	}

	return int(hit * 100 / total), length
}

// Keys 返回所有 key（快照，按 LRU 顺序从旧到新）。
func (c *LRUCache) Keys() []string {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	keys := c.cache.Keys()
	c.mu.RUnlock()

	return keys
}

// Iterator 遍历所有 entry。
//
// 遍历期间不长时间持有锁：先通过 RLock 检查 key 存在性，
// 再单独获取 entry 值，避免锁升级冲突。
func (c *LRUCache) Iterator(do func(key string, value any) bool) {
	if c == nil {
		return
	}

	c.mu.RLock()
	keys := c.cache.Keys()
	c.mu.RUnlock()

	for _, key := range keys {
		val, ok := c.cache.Get(key)
		if !ok {
			continue
		}
		if !do(key, val.value) {
			return
		}
	}
}

// Close 停止所有 TTL timer 并释放资源。可安全多次调用，后续调用为 no-op。
// 关闭后，Put 和其他写入操作会被静默丢弃。
func (c *LRUCache) Close() error {
	if c == nil {
		return nil
	}

	c.stopOnce.Do(func() {
		c.closed.Store(true)

		c.mu.RLock()
		keys := c.cache.Keys()
		c.mu.RUnlock()

		for _, key := range keys {
			c.mu.Lock()
			ent, ok := c.cache.Peek(key)
			if ok && ent.expire != nil {
				ent.expire.Stop()
			}
			c.mu.Unlock()
		}
	})

	return nil
}

// Flush 清空所有缓存条目并停止相关 TTL timer。
func (c *LRUCache) Flush() {
	if c == nil {
		return
	}

	c.mu.RLock()
	keys := c.cache.Keys()
	c.mu.RUnlock()

	for _, key := range keys {
		c.Remove(key)
	}
}

// === 内部方法 ===
