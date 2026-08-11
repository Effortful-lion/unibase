package state

import (
	"context"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"
)

// cacheEntry 是本地缓存中的条目。
type cacheEntry struct {
	snapshot ExecutionSnapshot
	expireAt time.Time
}

// CachedStorageOption 配置缓存层的行为。
type CachedStorageOption func(*cachedStorageOptions)

type cachedStorageOptions struct {
	maxEntries    int
	snapshotTTL   time.Duration
	enableMetrics bool
}

type cachedStorageOptionsInternal struct {
	cachedStorageOptions
	store TaskStorage[Task, SubTask]
}

// WithCacheMaxEntries 设置本地缓存最大条目数，默认 1000。
func WithCacheMaxEntries(n int) CachedStorageOption {
	return func(o *cachedStorageOptions) {
		o.maxEntries = n
	}
}

// WithCacheSnapshotTTL 设置快照在本地缓存中的过期时间，默认 30 秒。
func WithCacheSnapshotTTL(ttl time.Duration) CachedStorageOption {
	return func(o *cachedStorageOptions) {
		o.snapshotTTL = ttl
	}
}

// WithCacheMetrics 启用缓存命中率统计。
func WithCacheMetrics() CachedStorageOption {
	return func(o *cachedStorageOptions) {
		o.enableMetrics = true
	}
}

// CachedTaskStorage 在 TaskStorage 之上增加本地 LRU 缓存层，减少 Redis 读取。
//
// 特性：
//   - 本地 LRU 缓存快照，热任务读取零 Redis 开销
//   - singleflight 合并并发请求，防止缓存击穿
//   - 可选命中率统计
type CachedTaskStorage[D Task, S SubTask] struct {
	inner   TaskStorage[D, S]
	cache   *lru.Cache[string, cacheEntry]
	sfGroup singleflight.Group
	opts    cachedStorageOptions
	hits    atomic.Uint64
	total   atomic.Uint64
}

// NewCachedTaskStorage 创建带本地缓存的 TaskStorage 包装器。
func NewCachedTaskStorage[D Task, S SubTask](
	store TaskStorage[D, S],
	opts ...CachedStorageOption,
) *CachedTaskStorage[D, S] {
	cfg := cachedStorageOptions{
		maxEntries:  1000,
		snapshotTTL: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	cache, err := lru.NewWithEvict[string, cacheEntry](cfg.maxEntries, func(key string, entry cacheEntry) {})
	if err != nil {
		// maxEntries > 0 guaranteed above, panic on unexpected error
		panic(err)
	}

	return &CachedTaskStorage[D, S]{
		inner: store,
		cache: cache,
		opts:  cfg,
	}
}

// CacheMetrics 返回缓存命中统计。
func (c *CachedTaskStorage[D, S]) CacheMetrics() CacheMetrics {
	hits := c.hits.Load()
	total := c.total.Load()
	return CacheMetrics{
		Hits:  hits,
		Total: total,
		Ratio: hitRatio(hits, total),
	}
}

// CacheMetrics 缓存命中统计。
type CacheMetrics struct {
	Hits  uint64
	Total uint64
	Ratio float64
}

func hitRatio(hits, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// ======================== TaskStorage 接口实现 ========================

// SaveTaskState 保存主任务状态（透传 + 淘汰缓存）。
func (c *CachedTaskStorage[D, S]) SaveTaskState(ctx context.Context, taskID string, state State) error {
	// 写入前淘汰缓存，保证后续读取能看到最新状态
	c.cache.Remove(taskID)
	return c.inner.SaveTaskState(ctx, taskID, state)
}

// SaveSubTasks 保存子任务列表（透传）。
func (c *CachedTaskStorage[D, S]) SaveSubTasks(ctx context.Context, taskID string, entry State, subTasks []SubTaskRecord) error {
	return c.inner.SaveSubTasks(ctx, taskID, entry, subTasks)
}

// SaveSubTaskState 保存子任务状态（透传）。
func (c *CachedTaskStorage[D, S]) SaveSubTaskState(ctx context.Context, taskID string, entry State, subTaskID string, state State) error {
	return c.inner.SaveSubTaskState(ctx, taskID, entry, subTaskID, state)
}

// SaveSnapshot 保存快照（透传 + 更新缓存）。
func (c *CachedTaskStorage[D, S]) SaveSnapshot(ctx context.Context, taskID string, snapshot ExecutionSnapshot) error {
	// 写入时同步更新缓存
	c.cache.Add(taskID, cacheEntry{
		snapshot: snapshot,
		expireAt: time.Now().Add(c.opts.snapshotTTL),
	})
	return c.inner.SaveSnapshot(ctx, taskID, snapshot)
}

// LoadSnapshot 加载快照（带本地缓存 + singleflight）。
func (c *CachedTaskStorage[D, S]) LoadSnapshot(ctx context.Context, taskID string) (ExecutionSnapshot, error) {
	c.total.Add(1)

	// 1. 先查本地缓存
	if entry, ok := c.cache.Get(taskID); ok {
		if time.Now().Before(entry.expireAt) {
			c.hits.Add(1)
			return entry.snapshot, nil
		}
		// 过期，淘汰
		c.cache.Remove(taskID)
	}

	// 2. 缓存未命中，用 singleflight 合并并发请求
	result, err, _ := c.sfGroup.Do(taskID, func() (any, error) {
		snapshot, err := c.inner.LoadSnapshot(ctx, taskID)
		if err != nil {
			return ExecutionSnapshot{}, err
		}
		// 回写缓存
		if snapshot.State != "" {
			c.cache.Add(taskID, cacheEntry{
				snapshot: snapshot,
				expireAt: time.Now().Add(c.opts.snapshotTTL),
			})
		}
		return snapshot, nil
	})

	if err != nil {
		return ExecutionSnapshot{}, err
	}
	return result.(ExecutionSnapshot), nil
}

// LoadSnapshots 批量加载快照（透传，不缓存批量结果）。
func (c *CachedTaskStorage[D, S]) LoadSnapshots(ctx context.Context, taskIDs []string) (map[string]ExecutionSnapshot, error) {
	type batchResult struct {
		snapshot ExecutionSnapshot
		err      error
	}

	// 使用 singleflight 合并同一批 taskIDs 的并发请求
	result, err, _ := c.sfGroup.Do("batch:"+joinTaskIDs(taskIDs), func() (any, error) {
		return c.inner.LoadSnapshots(ctx, taskIDs)
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]ExecutionSnapshot), nil
}

// LoadTaskStates 批量查询任务状态（透传）。
func (c *CachedTaskStorage[D, S]) LoadTaskStates(ctx context.Context, taskIDs []string) (map[string]State, error) {
	return c.inner.LoadTaskStates(ctx, taskIDs)
}

// DeleteTask 删除任务（透传 + 淘汰缓存）。
func (c *CachedTaskStorage[D, S]) DeleteTask(ctx context.Context, taskID string) error {
	c.cache.Remove(taskID)
	return c.inner.DeleteTask(ctx, taskID)
}

func joinTaskIDs(ids []string) string {
	const maxJoin = 5
	if len(ids) > maxJoin {
		ids = ids[:maxJoin]
	}
	var b []byte
	for _, id := range ids {
		b = append(b, id...)
		b = append(b, ',')
	}
	return string(b)
}
