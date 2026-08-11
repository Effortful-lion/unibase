package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLRUCache(t *testing.T) {
	tests := []struct {
		name       string
		maxEntries int
		onEvicted  func(string, any)
		wantErr    bool
	}{
		{
			name:       "正常创建",
			maxEntries: 100,
			wantErr:    false,
		},
		{
			name:       "maxEntries=0 返回错误",
			maxEntries: 0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewLRUCache(LRUConfig{
				MaxEntries: tt.maxEntries,
				OnEvicted:  tt.onEvicted,
			})
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, c)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, c)
		})
	}
}

func TestLRUCache_PutAndGet(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 3})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	c.Put("b", 2, 0)
	c.Put("c", 3, 0)

	// 命中
	val, ok := c.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, val)

	// 不存在
	_, ok = c.Get("nonexistent")
	assert.False(t, ok)
}

func TestLRUCache_TTL(t *testing.T) {
	var evictedKey string
	var evictedVal any

	c, err := NewLRUCache(LRUConfig{
		MaxEntries: 10,
		OnEvicted: func(key string, value any) {
			evictedKey = key
			evictedVal = value
		},
	})
	assert.NoError(t, err)

	c.Put("key", "value", 50*time.Millisecond)

	// 过期前能命中
	val, ok := c.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)

	time.Sleep(100 * time.Millisecond)

	// 过期后未命中
	_, ok = c.Get("key")
	assert.False(t, ok)

	// 确认淘汰回调
	assert.Equal(t, "key", evictedKey)
	assert.Equal(t, "value", evictedVal)
}

func TestLRUCache_LRUEviction(t *testing.T) {
	var evicted []string

	c, err := NewLRUCache(LRUConfig{
		MaxEntries: 2,
		OnEvicted: func(key string, value any) {
			evicted = append(evicted, key)
		},
	})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	c.Put("b", 2, 0)

	// 添加 c 触发淘汰 a（最久未使用）
	c.Put("c", 3, 0)

	_, ok := c.Get("a")
	assert.False(t, ok)
	assert.Equal(t, []string{"a"}, evicted)
}

func TestLRUCache_UpdateExistingKey(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 3})
	assert.NoError(t, err)

	c.Put("key", "old", 0)
	_, ok := c.Get("key")
	assert.True(t, ok)

	c.Put("key", "new", 0)
	val, ok := c.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "new", val)
	// 更新不应增加条目数
	assert.Equal(t, 1, c.Len())
}

func TestLRUCache_Remove(t *testing.T) {
	var evicted bool

	c, err := NewLRUCache(LRUConfig{
		MaxEntries: 10,
		OnEvicted: func(key string, value any) {
			evicted = true
		},
	})
	assert.NoError(t, err)

	c.Put("key", "value", 0)
	val, ok := c.Remove("key")
	assert.True(t, ok)
	assert.Equal(t, "value", val)
	assert.Equal(t, 0, c.Len())
	assert.True(t, evicted)
}

func TestLRUCache_Contains(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	assert.True(t, c.Contains("a"))
	assert.False(t, c.Contains("b"))
}

func TestLRUCache_Keys(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	c.Put("b", 2, 0)
	c.Put("c", 3, 0)

	keys := c.Keys()
	assert.Len(t, keys, 3)
}

func TestLRUCache_Iterator(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	c.Put("b", 2, 0)
	c.Put("c", 3, 0)

	sum := 0
	c.Iterator(func(key string, value any) bool {
		sum += value.(int)
		return true
	})
	assert.Equal(t, 6, sum)
}

func TestLRUCache_HitRate(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	// 无查询时命中率 100
	rate, length := c.HitRate()
	assert.Equal(t, 100, rate)
	assert.Equal(t, 0, length)

	c.Put("a", 1, 0)

	// 命中
	c.Get("a")
	// 未命中
	c.Get("b")

	rate, _ = c.HitRate()
	assert.Equal(t, 50, rate) // 1 hit / 2 total = 50%

	// 下一次调用会重置计数器
	rate, _ = c.HitRate()
	assert.Equal(t, 100, rate)
}

func TestLRUCache_MaxEntries(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 100})
	assert.NoError(t, err)

	assert.Equal(t, 100, c.MaxEntries())
}

func TestLRUCache_TTLUpdateExistingKey(t *testing.T) {
	var expired atomic.Bool

	c, err := NewLRUCache(LRUConfig{
		MaxEntries: 10,
		OnEvicted: func(key string, value any) {
			if key == "key" {
				expired.Store(true)
			}
		},
	})
	assert.NoError(t, err)

	// 设置 50ms 过期
	c.Put("key", "value", 50*time.Millisecond)
	// 40ms 后重新写入，延长过期时间
	time.Sleep(40 * time.Millisecond)
	c.Put("key", "value", 100*time.Millisecond)

	// 再过 60ms，应该还没过期（因为被刷新了）
	time.Sleep(60 * time.Millisecond)

	val, ok := c.Get("key")
	assert.True(t, ok, "key should still be alive after refresh")
	assert.Equal(t, "value", val)
	assert.False(t, expired.Load(), "key should not have been evicted")
}

func TestLRUCache_TTLCancelOnEvict(t *testing.T) {
	// 验证 LRU 淘汰时，TTL timer 被正确取消，不会 double-free
	c, err := NewLRUCache(LRUConfig{
		MaxEntries: 2,
		OnEvicted: func(key string, value any) {
			// onEvict 里什么都不做，只验证 timer 已停止
		},
	})
	assert.NoError(t, err)

	c.Put("a", 1, 50*time.Millisecond)
	c.Put("b", 2, 50*time.Millisecond)

	// 等 TTL 到期
	time.Sleep(100 * time.Millisecond)

	// timer 的 goroutine 应该安全退出，不会 panic
	c.Put("c", 3, 0)
	_, ok := c.Get("c")
	assert.True(t, ok)
}

func TestLRUCache_NilSafety(t *testing.T) {
	var c *LRUCache

	_, _ = c.Get("key")
	assert.Equal(t, 0, c.Len())
	assert.Equal(t, 0, c.MaxEntries())
	assert.False(t, c.Contains("key"))

	// Nil Put should not panic
	c.Put("key", "val", 0)
	c.Remove("key")

	rate, length := c.HitRate()
	assert.Equal(t, 100, rate)
	assert.Equal(t, 0, length)
}

func TestLRUCache_Close(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 100*time.Millisecond)

	// Close 应该不 panic
	assert.NoError(t, c.Close())
	// 重复调用安全
	assert.NoError(t, c.Close())
}

func TestLRUCache_PutAfterClose(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	assert.NoError(t, c.Close())

	// Post-Close Put should be silently dropped, not panic
	c.Put("b", 2, 0)
	assert.Equal(t, 1, c.Len())
}

func TestLRUCache_CloseConcurrentPut(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 100})
	assert.NoError(t, err)

	// Concurrent Put and Close
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.Put(fmt.Sprintf("key-%d", i), i, 50*time.Millisecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		c.Close()
	}()

	wg.Wait()

	// 不应 panic，timer goroutine 应已停止
	_, _ = c.HitRate()
}

func TestLRUCache_GetOrSet(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	callCount := 0
	loader := func() (any, error) {
		callCount++
		return "loaded", nil
	}

	// 第一次：缓存未命中，调用 loader
	val, err := c.GetOrSet("k1", loader, 0)
	assert.NoError(t, err)
	assert.Equal(t, "loaded", val)
	assert.Equal(t, 1, callCount)

	// 第二次：缓存命中，不调用 loader
	val, err = c.GetOrSet("k1", loader, 0)
	assert.NoError(t, err)
	assert.Equal(t, "loaded", val)
	assert.Equal(t, 1, callCount) // 仍为 1
}

func TestLRUCache_GetOrSet_Concurrent(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 100})
	assert.NoError(t, err)

	var callCount int32
	loader := func() (any, error) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(10 * time.Millisecond) // 模拟慢计算
		return "shared", nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := c.GetOrSet("shared_key", loader, 0)
			assert.NoError(t, err)
			assert.Equal(t, "shared", val)
		}()
	}
	wg.Wait()

	// singleflight 保证 loader 只执行一次
	assert.Equal(t, int32(1), callCount)
}

func TestLRUCache_GetOrSet_NilCache(t *testing.T) {
	var c *LRUCache

	val, err := c.GetOrSet("key", func() (any, error) { return "v", nil }, 0)
	assert.Nil(t, val)
	assert.Nil(t, err)
}

func TestLRUCache_GetOrSet_NilLoader(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	val, err := c.GetOrSet("key", nil, 0)
	assert.Nil(t, val)
	assert.Nil(t, err)
}

func TestLRUCache_MGet(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 0)
	c.Put("b", 2, 0)
	c.Put("c", 3, 0)

	values := make([]any, 5)
	results := c.MGet([]string{"a", "b", "c", "d", "e"}, values)

	assert.Equal(t, []bool{true, true, true, false, false}, results)
	assert.Equal(t, 1, values[0])
	assert.Equal(t, 2, values[1])
	assert.Equal(t, 3, values[2])
	assert.Nil(t, values[3])
	assert.Nil(t, values[4])
}

func TestLRUCache_MSet(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.MSet(map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}, 0)

	assert.Equal(t, 3, c.Len())
	val, _ := c.Get("b")
	assert.Equal(t, 2, val)
}

func TestLRUCache_Flush(t *testing.T) {
	c, err := NewLRUCache(LRUConfig{MaxEntries: 10})
	assert.NoError(t, err)

	c.Put("a", 1, 100*time.Millisecond)
	c.Put("b", 2, 100*time.Millisecond)
	c.Put("c", 3, 100*time.Millisecond)
	assert.Equal(t, 3, c.Len())

	c.Flush()
	assert.Equal(t, 0, c.Len())

	// 被移除的 key 不应再命中
	_, ok := c.Get("a")
	assert.False(t, ok)
}
