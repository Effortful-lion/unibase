package cache

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// emptyValueSentinel 是 Redis 中存储的空值标记，当启用缓存空值选项时使用。
const emptyValueSentinel = "{}"

// ErrIsEmptyValue 在缓存命中但值为空时返回。调用方可通过 errors.Is 将其与缓存未命中区分。
var ErrIsEmptyValue = errors.New("cache: empty value")

// redisCacheOptions 保存 RedisCache 的可配置行为。
type redisCacheOptions struct {
	// localCache 是可选的进程内 LRU 回退缓存。
	localCache *LRUCache

	// localToRedisTTLRatio 控制本地缓存相对 Redis TTL 的存储时长比例。
	// 默认 0.5 表示本地 TTL = Redis TTL × 0.5。
	localToRedisTTLRatio float64

	// localCacheMinTTL 是 Redis 有 TTL 时本地缓存的最短存储时间。
	localCacheMinTTL time.Duration

	// localCacheDefaultTTL 在 Redis 无 TTL（ttl <= 0）时作为本地缓存的默认过期时间。
	localCacheDefaultTTL time.Duration

	// cacheEmptyValue 为 true 时，缓存未命中的空值会存入一个标记，避免重复查询不存在的 key 打穿到 Redis。
	cacheEmptyValue bool
}

// RCacheOption 是 NewRedisCache 的函数选项类型。
type RCacheOption func(*redisCacheOptions)

// WithLocalCache 附加一个 LRUCache 作为本地回退层。
func WithLocalCache(lc *LRUCache) RCacheOption {
	return func(o *redisCacheOptions) {
		o.localCache = lc
		if o.localToRedisTTLRatio == 0 {
			o.localToRedisTTLRatio = 0.5
		}
	}
}

// WithLocalToRedisTTLRatio 设置本地缓存 TTL 与 Redis TTL 的比例。
// localTTL = redisTTL × ratio。必须 > 0 且 <= 1，默认 0.5。
func WithLocalToRedisTTLRatio(ratio float64) RCacheOption {
	return func(o *redisCacheOptions) {
		o.localToRedisTTLRatio = ratio
	}
}

// WithLocalCacheMinTTL 设置本地缓存的最短 TTL。默认 10 秒。
func WithLocalCacheMinTTL(ttl time.Duration) RCacheOption {
	return func(o *redisCacheOptions) {
		o.localCacheMinTTL = ttl
	}
}

// WithLocalCacheDefaultTTL 设置 Redis 无过期时间时本地缓存的默认 TTL。默认 60 秒。
func WithLocalCacheDefaultTTL(ttl time.Duration) RCacheOption {
	return func(o *redisCacheOptions) {
		o.localCacheDefaultTTL = ttl
	}
}

// WithCacheEmptyValue 启用空值缓存，防止缓存穿透。
func WithCacheEmptyValue() RCacheOption {
	return func(o *redisCacheOptions) {
		o.cacheEmptyValue = true
	}
}

// === 导出方法 ===

// RedisCache 是基于 Redis 的分布式缓存，可选择使用本地 LRUCache 作为二级回退。
//
// 所有方法都支持多 goroutine 并发安全使用。
type RedisCache struct {
	opts    *redisCacheOptions
	client  redis.Cmdable
	sfGroup singleflight.Group

	hit   atomic.Uint64
	total atomic.Uint64
}

// NewRedisCache 使用提供的 go-redis 客户端创建 RedisCache。
// 客户端可以是已连接的 *redis.Client、*redis.ClusterClient 或任何实现 redis.Cmdable 的类型。
//
// 可通过选项附加本地 LRU 回退缓存并调整 TTL 行为。
func NewRedisCache(client redis.Cmdable, opts ...RCacheOption) *RedisCache {
	cfg := &redisCacheOptions{
		localCacheMinTTL:     10 * time.Second,
		localCacheDefaultTTL: 60 * time.Second,
		localToRedisTTLRatio: 0.5,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return &RedisCache{
		opts:   cfg,
		client: client,
	}
}

// Put 将 value 以 key 为键、ttl 为过期时间写入 Redis。
//   - 如果启用了缓存空值且 value 为 nil，则存入空值标记。
//   - 如果配置了本地缓存，value 也会以缩短后的 TTL 写入本地。
func (c *RedisCache) Put(ctx context.Context, key string, value any, ttl time.Duration) error {
	if c == nil || c.client == nil {
		return nil
	}

	if isNilValue(value) {
		if !c.opts.cacheEmptyValue {
			return errCannotCacheEmptyValue
		}
		value = emptyValueSentinel
	}

	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if err := c.client.Set(ctx, key, jsonData, ttl).Err(); err != nil {
		return err
	}

	if localTTL := c.localTTL(ttl); localTTL > 0 && c.opts.localCache != nil {
		c.opts.localCache.Put(key, jsonData, localTTL)
	}

	return nil
}

// Get 从 Redis 根据 key 获取 value。
//   - 如果配置了本地缓存，先查本地。
//   - 通过 singleflight 合并并发的 Redis 未命中请求，防止缓存击穿。
//   - 缓存命中且值为空标记时，返回 ErrIsEmptyValue。
func (c *RedisCache) Get(ctx context.Context, key string, value any) (bool, error) {
	if c == nil || c.client == nil {
		return false, nil
	}

	c.total.Add(1)

	// 1. 本地缓存命中？
	if c.opts.localCache != nil {
		localVal, ok := c.opts.localCache.Get(key)
		if ok {
			localBytes, ok := localVal.([]byte)
			if !ok {
				// 不应发生，防御性清理损坏的本地条目。
				c.opts.localCache.Remove(key)
			} else {
				c.hit.Add(1)
				if string(localBytes) == emptyValueSentinel {
					return true, ErrIsEmptyValue
				}
				if err := json.Unmarshal(localBytes, value); err != nil {
					return false, err
				}
				return true, nil
			}
		}
	}

	// 2. 通过 singleflight 访问 Redis
	ret, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		return c._get(ctx, key)
	})
	if err != nil {
		return false, err
	}

	raw, ok := ret.([]byte)
	if !ok || raw == nil {
		return false, nil
	}

	c.hit.Add(1)

	// 检查空值标记
	if string(raw) == emptyValueSentinel {
		if c.opts.localCache != nil {
			c.opts.localCache.Put(key, raw, c.opts.localCacheMinTTL)
		}
		return true, ErrIsEmptyValue
	}

	if err := json.Unmarshal(raw, value); err != nil {
		return false, err
	}

	// 用 JSON 字节 + 计算出的 TTL 填充本地缓存
	if localTTL := c.localTTLFor(ctx, key); localTTL > 0 && c.opts.localCache != nil {
		c.opts.localCache.Put(key, raw, localTTL)
	}

	return true, nil
}

// GetOrSet 先查缓存，未命中则调用 loader 计算值、写入 Redis 并返回。
//
// 同一 key 的并发未命中请求会通过 singleflight 合并，仅执行一次 loader。
// loader 返回的错误不会被缓存，后续请求会重试。
//
// 参数:
//   - key: 缓存键。
//   - loader: 缓存未命中时的值计算函数，返回 (值, 错误)。
//   - ttl: 写入 Redis 的过期时间，<= 0 表示永不过期。
//
// 返回值: (值, 错误)。缓存命中时直接返回缓存值，loader 不会被调用。
func (c *RedisCache) GetOrSet(ctx context.Context, key string, loader func() (any, error), ttl time.Duration) (any, error) {
	if c == nil || c.client == nil || loader == nil {
		return nil, nil
	}

	// 先查缓存，命中直接返回。
	var val any
	ok, err := c.Get(ctx, key, &val)
	if err != nil {
		return nil, err
	}
	if ok {
		return val, nil
	}

	// 未命中，通过 singleflight 合并并发请求。
	result, err, _ := c.sfGroup.Do(key, func() (interface{}, error) {
		return loader()
	})
	if err != nil {
		return nil, err
	}

	// 写入 Redis 和本地缓存。
	if err := c.Put(ctx, key, result, ttl); err != nil {
		return nil, err
	}

	return result, nil
}

// MGet 批量获取。keys 和 values 长度必须一致，values 的每个元素必须是可写的指针。
// 返回一个 bool 切片，表示每个 key 是否命中（命中时 values[i] 已被赋值）。
// 空值命中（缓存穿透标记）计为命中，返回 ErrIsEmptyValue 可通过 errors.Is 逐项检查。
func (c *RedisCache) MGet(ctx context.Context, keys []string, values []any) ([]bool, error) {
	if c == nil || c.client == nil || len(keys) == 0 {
		return make([]bool, len(keys)), nil
	}

	if len(values) != len(keys) {
		return nil, errors.New("cache: values slice length must match keys length")
	}

	rawResults, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	results := make([]bool, len(keys))
	for i, raw := range rawResults {
		if raw == nil {
			results[i] = false
			continue
		}

		bytes, ok := raw.(string)
		if !ok {
			results[i] = false
			continue
		}

		if bytes == emptyValueSentinel {
			results[i] = true
			if c.opts.localCache != nil {
				c.opts.localCache.Put(keys[i], []byte(bytes), c.opts.localCacheMinTTL)
			}
			continue
		}

		if err := json.Unmarshal([]byte(bytes), values[i]); err != nil {
			return nil, err
		}
		results[i] = true

		if localTTL := c.localTTLFromRaw([]byte(bytes)); localTTL > 0 && c.opts.localCache != nil {
			c.opts.localCache.Put(keys[i], []byte(bytes), localTTL)
		}
	}

	return results, nil
}

// MSet 批量写入。所有条目使用相同的 ttl。
// 通过 Pipeline 在一次网络往返内完成，性能优于逐个 Put。
func (c *RedisCache) MSet(ctx context.Context, items map[string]any, ttl time.Duration) error {
	if c == nil || c.client == nil || len(items) == 0 {
		return nil
	}

	pipe := c.client.Pipeline()
	for key, value := range items {
		if isNilValue(value) {
			if !c.opts.cacheEmptyValue {
				return errCannotCacheEmptyValue
			}
			value = emptyValueSentinel
		}

		jsonData, err := json.Marshal(value)
		if err != nil {
			return err
		}
		pipe.Set(ctx, key, jsonData, ttl)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// Remove 从 Redis 和本地缓存中删除 key。
func (c *RedisCache) Remove(ctx context.Context, key string) error {
	if c == nil || c.client == nil {
		return nil
	}

	if err := c.client.Del(ctx, key).Err(); err != nil {
		return err
	}

	if c.opts.localCache != nil {
		c.opts.localCache.Remove(key)
	}

	return nil
}

// FlushLocal 清空本地缓存（如有配置）。Redis 数据不受影响。
func (c *RedisCache) FlushLocal() {
	if c.opts.localCache != nil {
		c.opts.localCache.Flush()
	}
}

// HitRate 返回当前 Redis 层的命中率。
type HitRate struct {
	// RedisHitRate 是 Redis 层的命中率（0-100）。
	RedisHitRate int
}

// HitRate 返回当前 Redis 层的命中率。
func (c *RedisCache) HitRate() HitRate {
	var hr HitRate

	hit := c.hit.Swap(0)
	total := c.total.Swap(0)
	if total == 0 {
		hr.RedisHitRate = 100
	} else {
		hr.RedisHitRate = int(hit * 100 / total)
	}

	return hr
}

// LocalHitRate 返回本地缓存的命中率，未配置本地缓存时返回 (100, 0)。
func (c *RedisCache) LocalHitRate() (int, int) {
	if c.opts.localCache == nil {
		return 100, 0
	}
	return c.opts.localCache.HitRate()
}

// === 内部方法 ===

// _get 执行实际的 Redis GET，独立出来以便 singleflight 包裹。
func (c *RedisCache) _get(ctx context.Context, key string) ([]byte, error) {
	raw, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	return raw, err
}

// localTTL 根据 Redis 的 TTL 计算本地缓存的 TTL。
func (c *RedisCache) localTTL(redisTTL time.Duration) time.Duration {
	if c.opts.localCache == nil {
		return 0
	}
	if redisTTL <= 0 {
		return c.opts.localCacheDefaultTTL
	}
	ttl := time.Duration(float64(redisTTL) * c.opts.localToRedisTTLRatio)
	if ttl < c.opts.localCacheMinTTL {
		return c.opts.localCacheMinTTL
	}
	return ttl
}

// localTTLFor 查询 Redis 中 key 的剩余 TTL，并计算对应的本地缓存 TTL。
// 如果 key 无 TTL 或查询失败，则回退到默认值。
func (c *RedisCache) localTTLFor(ctx context.Context, key string) time.Duration {
	if c.opts.localCache == nil {
		return 0
	}

	ttl, err := c.client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		return c.opts.localCacheDefaultTTL
	}
	return c.localTTL(ttl)
}

// localTTLFromRaw 从 raw 数据长度估算本地缓存 TTL，用于 MGet 批量场景。
// 无法从 raw 数据中获取真实 TTL，回退到默认值。
func (c *RedisCache) localTTLFromRaw(raw []byte) time.Duration {
	if c.opts.localCache == nil {
		return 0
	}
	_ = raw
	return c.opts.localCacheDefaultTTL
}

// isNilValue 判断 v 是否为 nil 或有类型的 nil（指针、切片、映射、通道、接口、函数）。
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// errCannotCacheEmptyValue 在未启用空值缓存选项时传入 nil/空值，返回此错误。
var errCannotCacheEmptyValue = errors.New("cache: cannot cache empty value, enable WithCacheEmptyValue")
