package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// testRedisClient 创建真实 Redis 客户端用于测试，如果 Redis 不可用则跳过测试。
func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at localhost:6379: %v", err)
	}
	return client
}

func TestRedisCache_PutAndGet(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	key := "test:put_get"
	defer client.Del(ctx, key)

	// 写入
	err := c.Put(ctx, key, "hello", 5*time.Minute)
	assert.NoError(t, err)

	// 读取
	var val string
	ok, err := c.Get(ctx, key, &val)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestRedisCache_Miss(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	var val string
	ok, err := c.Get(ctx, "test:miss:xyz123", &val)
	assert.False(t, ok)
	assert.NoError(t, err)
}

func TestRedisCache_Remove(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	key := "test:remove"
	client.Set(ctx, key, "value", 0)

	err := c.Remove(ctx, key)
	assert.NoError(t, err)

	var val string
	ok, err := c.Get(ctx, key, &val)
	assert.False(t, ok)
	assert.NoError(t, err)
}

func TestRedisCache_NilSafety(t *testing.T) {
	var nilCache *RedisCache

	// nil 缓存不应 panic
	ok, err := nilCache.Get(context.Background(), "any", new(string))
	assert.False(t, ok)
	assert.NoError(t, err)

	err = nilCache.Put(context.Background(), "any", "val", 0)
	assert.NoError(t, err)

	err = nilCache.Remove(context.Background(), "any")
	assert.NoError(t, err)
}

func TestRedisCache_HitRate(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	// 无查询时命中率 100
	hr := c.HitRate()
	assert.Equal(t, 100, hr.RedisHitRate)

	key := "test:hitrate"
	client.Set(ctx, key, `"val"`, 0)

	c.Get(ctx, key, new(string))    // 命中
	c.Get(ctx, "miss", new(string)) // 未命中

	hr = c.HitRate()
	assert.Equal(t, 50, hr.RedisHitRate)
}

func TestRedisCache_EmptyValue(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client, WithCacheEmptyValue())

	key := "test:emptyval"
	defer client.Del(ctx, key)

	// 缓存空值
	err := c.Put(ctx, key, nil, 5*time.Minute)
	assert.NoError(t, err)

	// 命中时应返回 ErrIsEmptyValue
	var val string
	ok, err := c.Get(ctx, key, &val)
	assert.True(t, ok)
	assert.ErrorIs(t, err, ErrIsEmptyValue)
}

func TestRedisCache_EmptyValueBlockedByDefault(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client) // 未启用 WithCacheEmptyValue

	err := c.Put(ctx, "test:emptyblocked", nil, 5*time.Minute)
	assert.ErrorIs(t, err, errCannotCacheEmptyValue)
}

func TestRedisCache_WithLocalCache(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()

	local, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)
	defer local.Close()

	c := NewRedisCache(client, WithLocalCache(local))

	key := "test:local"
	client.Set(ctx, key, `"local_val"`, 5*time.Minute)

	// 第一次读取命中 Redis，填充本地缓存
	var val string
	ok, err := c.Get(ctx, key, &val)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "local_val", val)

	// 第二次读取应命中本地缓存
	ok, err = c.Get(ctx, key, &val)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, "local_val", val)

	localRate, _ := c.LocalHitRate()
	assert.Greater(t, localRate, 0, "local cache should have hits")
}

func TestRedisCache_LocalCacheTTL(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	local, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)
	defer local.Close()

	c := NewRedisCache(client,
		WithLocalCache(local),
		WithLocalToRedisTTLRatio(0.5),
		WithLocalCacheMinTTL(100*time.Millisecond),
	)

	key := "test:localttl"
	client.Set(ctx, key, `"val"`, 2*time.Minute)

	// 填充本地缓存
	c.Get(ctx, key, new(string))

	// 本地 TTL 应为 2min × 0.5 = 1min，远大于最小值 100ms
	assert.True(t, local.Contains(key))

	// 经过 200ms 后本地条目应仍然存在（TTL ≈ 1min）
	time.Sleep(200 * time.Millisecond)
	assert.True(t, local.Contains(key))
}

func TestRedisCache_GetOrSet(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	key := "test:getorset"
	defer client.Del(ctx, key)

	callCount := 0
	loader := func() (any, error) {
		callCount++
		return "loaded_value", nil
	}

	// 第一次：缓存未命中，调用 loader
	val, err := c.GetOrSet(ctx, key, loader, 5*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, "loaded_value", val)
	assert.Equal(t, 1, callCount)

	// 第二次：缓存命中，不调用 loader
	val, err = c.GetOrSet(ctx, key, loader, 5*time.Minute)
	assert.NoError(t, err)
	assert.Equal(t, "loaded_value", val)
	assert.Equal(t, 1, callCount) // 仍为 1
}

func TestRedisCache_GetOrSet_LoaderError(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	key := "test:getorset_err"
	defer client.Del(ctx, key)

	callCount := 0
	loader := func() (any, error) {
		callCount++
		return nil, errors.New("loader failed")
	}

	// loader 返回错误
	_, err := c.GetOrSet(ctx, key, loader, 5*time.Minute)
	assert.Error(t, err)
	assert.Equal(t, 1, callCount)

	// 错误不被缓存，再次调用仍会触发 loader
	_, err = c.GetOrSet(ctx, key, loader, 5*time.Minute)
	assert.Error(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRedisCache_MGet(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	keys := []string{"test:mget:a", "test:mget:b", "test:mget:c", "test:mget:missing"}
	client.MSet(ctx, map[string]any{
		"test:mget:a": `"val_a"`,
		"test:mget:b": `"val_b"`,
		"test:mget:c": `"val_c"`,
	})

	values := make([]any, 4)
	results, err := c.MGet(ctx, keys, values)
	assert.NoError(t, err)
	assert.Equal(t, []bool{true, true, true, false}, results)
	assert.Equal(t, "val_a", values[0])
	assert.Equal(t, "val_b", values[1])
	assert.Equal(t, "val_c", values[2])
	assert.Nil(t, values[3])
}

func TestRedisCache_MSet(t *testing.T) {
	client := testRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	c := NewRedisCache(client)

	items := map[string]any{
		"test:mset:a": "alpha",
		"test:mset:b": "beta",
		"test:mset:c": "gamma",
	}

	err := c.MSet(ctx, items, 5*time.Minute)
	assert.NoError(t, err)

	// 验证写入
	for key, expected := range items {
		var val string
		ok, err := c.Get(ctx, key, &val)
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, expected, val)
	}
}
