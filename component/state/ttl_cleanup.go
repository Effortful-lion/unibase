package state

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Effortful-lion/unibase/logx"
	"github.com/redis/go-redis/v9"
)

// TaskCleaner 负责清理过期的任务数据。
//
// 由于 Redis key 设置了 TTL，数据会自动过期。
// TaskCleaner 负责清理二级索引中的过期条目，并提供手动批量清理能力。
type TaskCleaner[D Task, S SubTask] struct {
	store           TaskStorage[D, S]
	prefix          string
	indexEnabled    bool
	cleanupInterval time.Duration
	logger          *logx.Logger
	stopCh          chan struct{}
	once            sync.Once
	wg              sync.WaitGroup
}

// CleanerOption 配置 TaskCleaner 的行为。
type CleanerOption func(*cleanerOptions)

type cleanerOptions struct {
	cleanupInterval time.Duration
	logger          *logx.Logger
}

// WithCleanupInterval 设置自动清理间隔，默认 1 小时。
func WithCleanupInterval(interval time.Duration) CleanerOption {
	return func(o *cleanerOptions) {
		o.cleanupInterval = interval
	}
}

// WithCleanupLogger 设置清理器日志器。
func WithCleanupLogger(logger *logx.Logger) CleanerOption {
	return func(o *cleanerOptions) {
		o.logger = logger
	}
}

// NewTaskCleaner 创建任务清理器。
//
// store 是底层的 TaskStorage 实现（如 *RedisStorage）。
// prefix 是 Redis key 前缀，用于构造索引 key。
// indexEnabled 表示是否启用了二级索引。
// opts 用于配置清理行为。
func NewTaskCleaner[D Task, S SubTask](
	store TaskStorage[D, S],
	prefix string,
	indexEnabled bool,
	opts ...CleanerOption,
) *TaskCleaner[D, S] {
	cfg := cleanerOptions{
		cleanupInterval: 1 * time.Hour,
		logger:          logx.Module("state"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &TaskCleaner[D, S]{
		store:           store,
		prefix:          prefix,
		indexEnabled:    indexEnabled,
		cleanupInterval: cfg.cleanupInterval,
		logger:          cfg.logger,
		stopCh:          make(chan struct{}),
	}
}

// Start 启动后台清理 goroutine。
// 调用方应在 Manager 生命周期内保持清理器运行。
// ctx 控制清理器的生命周期。
func (c *TaskCleaner[D, S]) Start(ctx context.Context) {
	if c == nil {
		return
	}
	c.once.Do(func() {
		c.wg.Add(1)
		go c.run(ctx)
	})
}

// Stop 停止后台清理 goroutine，阻塞直到 goroutine 退出。
func (c *TaskCleaner[D, S]) Stop() {
	if c == nil {
		return
	}
	close(c.stopCh)
	c.wg.Wait()
}

// run 是后台清理循环。
func (c *TaskCleaner[D, S]) run(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	// 启动时先执行一次清理
	c.cleanupOrphanedIndexEntries(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.cleanupOrphanedIndexEntries(ctx)
		}
	}
}

// cleanupOrphanedIndexEntries 清理索引中已不存在的数据引用。
//
// 策略：遍历状态索引，检查每个 taskID 是否还有对应的状态数据。
// 对于不存在的数据，从索引中移除。
//
// 注意：Redis TTL 会自动清理过期 key，但索引条目需要单独清理。
func (c *TaskCleaner[D, S]) cleanupOrphanedIndexEntries(ctx context.Context) {
	if !c.indexEnabled {
		return
	}

	rs, ok := c.store.(*RedisStorage[D, S])
	if !ok {
		return
	}

	start := time.Now()
	cleaned := 0

	states := []State{PendingState, CreatedState, SuccessState, FailedState, CanceledState}
	for _, state := range states {
		idxKey := rs.keyIdxState(state)

		// 获取索引中所有 taskID
		taskIDs, err := rs.client.ZRange(ctx, idxKey, 0, -1).Result()
		if err != nil {
			c.logger.Error("cleanup: zrange failed",
				logx.Fields{"idx_key": idxKey, "err": err})
			continue
		}

		var toRemove []string
		for _, taskID := range taskIDs {
			// 检查任务状态数据是否还存在
			stateKey := rs.keyTaskState(taskID)
			exists, err := rs.client.Exists(ctx, stateKey).Result()
			if err != nil {
				continue
			}
			if exists == 0 {
				toRemove = append(toRemove, taskID)
			}
		}

		if len(toRemove) > 0 {
			if _, err := rs.client.ZRem(ctx, idxKey, toInterfaceSlice(toRemove)...).Result(); err != nil {
				c.logger.Error("cleanup: zrem failed",
					logx.Fields{"idx_key": idxKey, "err": err, "count": len(toRemove)})
			} else {
				cleaned += len(toRemove)
			}
		}
	}

	// 清理创建时间索引
	createdIdxKey := rs.keyIdxCreated()
	createdTaskIDs, err := rs.client.ZRange(ctx, createdIdxKey, 0, -1).Result()
	if err == nil {
		var toRemove []string
		for _, taskID := range createdTaskIDs {
			stateKey := rs.keyTaskState(taskID)
			exists, _ := rs.client.Exists(ctx, stateKey).Result()
			if exists == 0 {
				toRemove = append(toRemove, taskID)
			}
		}
		if len(toRemove) > 0 {
			if _, err := rs.client.ZRem(ctx, createdIdxKey, toInterfaceSlice(toRemove)...).Result(); err != nil {
				c.logger.Error("cleanup: zrem created idx failed",
					logx.Fields{"err": err, "count": len(toRemove)})
			} else {
				cleaned += len(toRemove)
			}
		}
	}

	elapsed := time.Since(start)
	if cleaned > 0 {
		c.logger.Info("cleanup completed",
			logx.Fields{"cleaned": cleaned, "elapsed": elapsed.String()})
	}
}

// CleanupExpired 手动触发一次过期索引清理。
// 可用于运维脚本或管理后台的手动清理操作。
func (c *TaskCleaner[D, S]) CleanupExpired(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.cleanupOrphanedIndexEntries(ctx)
	return nil
}

// CleanupTasksBefore 清理指定时间点之前已完成的任务数据。
//
// before 是截止时间，早于该时间且状态为终态的任务将被删除。
// 返回被清理的任务数量。
func (c *TaskCleaner[D, S]) CleanupTasksBefore(ctx context.Context, before time.Time) (int64, error) {
	rs, ok := c.store.(*RedisStorage[D, S])
	if !ok {
		return 0, fmt.Errorf("store is not *RedisStorage")
	}

	beforeScore := float64(before.UnixNano()) / 1e9
	createdIdxKey := rs.keyIdxCreated()

	// 获取截止时间前的所有 taskID
	taskIDs, err := rs.client.ZRangeByScore(ctx, createdIdxKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%f", beforeScore),
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("zrange created idx: %w", err)
	}

	if len(taskIDs) == 0 {
		return 0, nil
	}

	cleaned := int64(0)
	for _, taskID := range taskIDs {
		// 只清理已终态的任务
		state, err := rs.GetTaskState(ctx, taskID)
		if err != nil {
			continue
		}
		if !IsFinalState(state) {
			continue
		}

		if err := rs.DeleteTask(ctx, taskID); err != nil {
			c.logger.Error("cleanup: delete task failed",
				logx.Fields{"task_id": taskID, "err": err})
			continue
		}
		cleaned++
	}

	c.logger.Info("cleanup before completed",
		logx.Fields{"before": before.String(), "cleaned": cleaned})
	return cleaned, nil
}

// toInterfaceSlice 将 []string 转换为 []interface{}。
func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}
