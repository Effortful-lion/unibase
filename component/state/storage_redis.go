package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// defaultStateTTL 是任务状态数据的默认过期时间（7 天）。
	// 由调用方通过 WithRedisTTL 可覆盖；传 0 表示永不过期。
	defaultStateTTL = 7 * 24 * time.Hour
)

// RedisStorage 是基于 Redis 的 TaskStorage 实现。
// 提供任务状态、子任务、快照的持久化存储，支持 TTL 自动过期。
//
// 使用示例：
//
//	storage := NewRedisStorage[*MyTask, *MySubTask](redisClient,
//	    WithRedisPrefix("myapp"),
//	    WithRedisTTL(7*24*time.Hour),
//	)
type RedisStorage[D Task, S SubTask] struct {
	client       redis.Cmdable
	prefix       string
	ttl          time.Duration
	indexEnabled bool
}

// Ensure RedisStorage implements TaskStorage.
var _ TaskStorage[Task, SubTask] = (*RedisStorage[Task, SubTask])(nil)

// RedisStorageOption 配置 RedisStorage 的行为。
type RedisStorageOption func(*redisStorageOptions)

type redisStorageOptions struct {
	prefix   string
	ttl      time.Duration
	indexing bool
}

// WithRedisIndexing 启用二级索引，支持按状态和时间范围查询任务。
// 默认关闭，可通过此选项启用。
func WithRedisIndexing() RedisStorageOption {
	return func(o *redisStorageOptions) {
		o.indexing = true
	}
}

// WithRedisPrefix 设置 Redis key 前缀，用于多业务隔离。
// 默认前缀为 "state"。
func WithRedisPrefix(prefix string) RedisStorageOption {
	return func(o *redisStorageOptions) {
		if prefix != "" {
			o.prefix = prefix
		}
	}
}

// WithRedisTTL 设置状态数据的默认过期时间。
// 传 0 表示永不过期。默认 0。
func WithRedisTTL(ttl time.Duration) RedisStorageOption {
	return func(o *redisStorageOptions) {
		o.ttl = ttl
	}
}

// NewRedisStorage 创建 Redis 存储实例。
//
// client 可以是 *redis.Client 或 *redis.ClusterClient（都实现了 redis.Cmdable）。
// opts 用于配置 key 前缀和 TTL。
func NewRedisStorage[D Task, S SubTask](
	client redis.Cmdable,
	opts ...RedisStorageOption,
) *RedisStorage[D, S] {
	cfg := redisStorageOptions{
		prefix: "state",
		ttl:    defaultStateTTL,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &RedisStorage[D, S]{
		client:       client,
		prefix:       cfg.prefix,
		ttl:          cfg.ttl,
		indexEnabled: cfg.indexing,
	}
}

// keyTaskStates 生成任务状态轨迹 key：{prefix}:task_states:{taskID}
func (r *RedisStorage[D, S]) keyTaskStates(taskID string) string {
	return r.prefix + ":task_states:" + taskID
}

// keySubTasks 生成子任务列表 key：{prefix}:subtasks:{taskID}
func (r *RedisStorage[D, S]) keySubTasks(taskID string) string {
	return r.prefix + ":subtasks:" + taskID
}

// keySubTaskStates 生成子任务状态轨迹 key：{prefix}:subtask_states:{subTaskID}
func (r *RedisStorage[D, S]) keySubTaskStates(subTaskID string) string {
	return r.prefix + ":subtask_states:" + subTaskID
}

// keySnapshot 生成快照 key：{prefix}:snapshot:{taskID}
func (r *RedisStorage[D, S]) keySnapshot(taskID string) string {
	return r.prefix + ":snapshot:" + taskID
}

// keyTaskState 生成当前状态 key：{prefix}:task_state:{taskID}
func (r *RedisStorage[D, S]) keyTaskState(taskID string) string {
	return r.prefix + ":task_state:" + taskID
}

// keyIdxState 生成状态索引 key：{prefix}:idx:state:{state}
func (r *RedisStorage[D, S]) keyIdxState(state State) string {
	return r.prefix + ":idx:state:" + string(state)
}

// keyIdxCreated 创建时间索引 key：{prefix}:idx:created
func (r *RedisStorage[D, S]) keyIdxCreated() string {
	return r.prefix + ":idx:created"
}

// SaveTaskState 将任务状态追加到状态轨迹列表（LPUSH），并更新二级索引和当前状态追踪。
func (r *RedisStorage[D, S]) SaveTaskState(ctx context.Context, taskID string, state State) error {
	now := time.Now().Unix()
	data, err := json.Marshal(stateEntry{State: state, Timestamp: now})
	if err != nil {
		return err
	}

	key := r.keyTaskStates(taskID)
	stateKey := r.keyTaskState(taskID)
	pipe := r.client.Pipeline()
	pipe.LPush(ctx, key, data)
	if r.ttl > 0 {
		pipe.Expire(ctx, key, r.ttl)
	}

	// 更新当前状态（仅当状态变化时更新索引）
	if oldState, err := r.client.Get(ctx, stateKey).Result(); err == nil && oldState != "" && oldState != string(state) {
		// 从旧状态索移除
		if r.indexEnabled {
			pipe.ZRem(ctx, r.keyIdxState(State(oldState)), taskID)
		}
	}
	pipe.Set(ctx, stateKey, string(state), 0)

	if r.ttl > 0 {
		pipe.Expire(ctx, stateKey, r.ttl)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	// 更新二级索引（状态索引 + 创建时间索引）
	if r.indexEnabled {
		score := float64(time.Now().UnixNano()) / 1e9
		if _, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.ZAdd(ctx, r.keyIdxState(state), redis.Z{Score: score, Member: taskID})
			pipe.ZAdd(ctx, r.keyIdxCreated(), redis.Z{Score: score, Member: taskID})
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// SaveSubTasks 保存子任务列表（HSET，field = entry state，value = JSON 数组）。
func (r *RedisStorage[D, S]) SaveSubTasks(ctx context.Context, taskID string, entry State, subTasks []SubTaskRecord) error {
	if len(subTasks) == 0 {
		return nil
	}
	data, err := json.Marshal(subTasks)
	if err != nil {
		return err
	}
	key := r.keySubTasks(taskID)
	if err := r.client.HSet(ctx, key, string(entry), data).Err(); err != nil {
		return err
	}
	if r.ttl > 0 {
		return r.client.Expire(ctx, key, r.ttl).Err()
	}
	return nil
}

// SaveSubTaskState 将子任务状态追加到状态轨迹列表（LPUSH）。
func (r *RedisStorage[D, S]) SaveSubTaskState(ctx context.Context, taskID string, _ State, subTaskID string, state State) error {
	data, err := json.Marshal(stateEntry{State: state, Timestamp: time.Now().Unix()})
	if err != nil {
		return err
	}
	key := r.keySubTaskStates(subTaskID)
	pipe := r.client.Pipeline()
	pipe.LPush(ctx, key, data)
	if r.ttl > 0 {
		pipe.Expire(ctx, key, r.ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return nil
}

// LoadSnapshot 加载任务执行快照（GET + JSON 反序列化）。
// 快照不存在时返回空 ExecutionSnapshot，不返回错误。
func (r *RedisStorage[D, S]) LoadSnapshot(ctx context.Context, taskID string) (ExecutionSnapshot, error) {
	data, err := r.client.Get(ctx, r.keySnapshot(taskID)).Result()
	if err == redis.Nil {
		return ExecutionSnapshot{}, nil
	}
	if err != nil {
		return ExecutionSnapshot{}, err
	}
	var snapshot ExecutionSnapshot
	if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
		return ExecutionSnapshot{}, err
	}
	return snapshot, nil
}

// SaveSnapshot 保存任务执行快照（SET）。
func (r *RedisStorage[D, S]) SaveSnapshot(ctx context.Context, taskID string, snapshot ExecutionSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	key := r.keySnapshot(taskID)
	if err := r.client.Set(ctx, key, data, r.ttl).Err(); err != nil {
		return err
	}
	return nil
}

// stateEntry 是 Redis 中存储的状态轨迹条目。
type stateEntry struct {
	State     State `json:"state"`
	Timestamp int64 `json:"timestamp"`
}

// ======================== 查询 API ========================

// TaskSummary 是任务摘要信息。
type TaskSummary struct {
	TaskID    string
	State     State
	CreatedAt int64
}

// GetTaskState 查询任务的当前状态。
func (r *RedisStorage[D, S]) GetTaskState(ctx context.Context, taskID string) (State, error) {
	stateKey := r.keyTaskState(taskID)
	data, err := r.client.Get(ctx, stateKey).Result()
	if err == redis.Nil {
		return "", ErrTaskNotFound
	}
	if err != nil {
		return "", err
	}
	return State(data), nil
}

// ListTasksByState 按状态分页查询任务 ID 列表。
// cursor 是上次返回的游标，首次调用传空字符串。
// limit 控制每页数量，建议 10-100。
// 返回 (taskIDs, nextCursor, error)，nextCursor 为空表示最后一页。
func (r *RedisStorage[D, S]) ListTasksByState(ctx context.Context, state State, cursor string, limit int64) ([]string, string, error) {
	if !r.indexEnabled {
		return nil, "", &StateError{message: "indexing not enabled, use WithRedisIndexing()"}
	}
	if limit <= 0 {
		limit = 20
	}

	idxKey := r.keyIdxState(state)
	var taskIDs []string
	var nextCursor string

	if cursor == "" {
		// 首次查询：取前 limit 个
		cmds, err := r.client.ZRange(ctx, idxKey, 0, limit-1).Result()
		if err != nil {
			return nil, "", err
		}
		taskIDs = cmds
		if int64(len(taskIDs)) >= limit {
			nextCursor = taskIDs[len(taskIDs)-1]
		}
	} else {
		// 分页查询：从 cursor 之后取 limit 个
		rank, err := r.client.ZRank(ctx, idxKey, cursor).Result()
		if err != nil {
			return nil, "", err
		}
		start := rank + 1
		cmds, err := r.client.ZRange(ctx, idxKey, start, start+limit-1).Result()
		if err != nil {
			return nil, "", err
		}
		taskIDs = cmds
		if int64(len(taskIDs)) >= limit {
			nextCursor = taskIDs[len(taskIDs)-1]
		}
	}

	return taskIDs, nextCursor, nil
}

// ListTasksByTime 按创建时间范围查询任务 ID 列表。
// start/end 为 Unix 时间戳（秒），传 0 表示不限制。
func (r *RedisStorage[D, S]) ListTasksByTime(ctx context.Context, start, end int64, cursor string, limit int64) ([]string, string, error) {
	if !r.indexEnabled {
		return nil, "", &StateError{message: "indexing not enabled, use WithRedisIndexing()"}
	}
	if limit <= 0 {
		limit = 20
	}

	idxKey := r.keyIdxCreated()
	min := "-inf"
	max := "+inf"
	if start > 0 {
		min = fmt.Sprintf("%d", start)
	}
	if end > 0 {
		max = fmt.Sprintf("%d", end)
	}

	var taskIDs []string
	var nextCursor string

	if cursor == "" {
		cmds, err := r.client.ZRangeByScore(ctx, idxKey, &redis.ZRangeBy{
			Min:    min,
			Max:    max,
			Offset: 0,
			Count:  limit,
		}).Result()
		if err != nil {
			return nil, "", err
		}
		taskIDs = cmds
		if int64(len(taskIDs)) >= limit {
			nextCursor = taskIDs[len(taskIDs)-1]
		}
	} else {
		rank, err := r.client.ZRank(ctx, idxKey, cursor).Result()
		if err != nil {
			return nil, "", err
		}
		startOffset := rank + 1
		cmds, err := r.client.ZRangeByScore(ctx, idxKey, &redis.ZRangeBy{
			Min:    min,
			Max:    max,
			Offset: startOffset,
			Count:  limit,
		}).Result()
		if err != nil {
			return nil, "", err
		}
		taskIDs = cmds
		if int64(len(taskIDs)) >= limit {
			nextCursor = taskIDs[len(taskIDs)-1]
		}
	}

	return taskIDs, nextCursor, nil
}

// DeleteTask 删除任务及其所有关联数据（状态轨迹、快照、子任务、索引）。
func (r *RedisStorage[D, S]) DeleteTask(ctx context.Context, taskID string) error {
	keys := []string{
		r.keyTaskStates(taskID),
		r.keyTaskState(taskID),
		r.keySubTasks(taskID),
		r.keySnapshot(taskID),
	}

	// 收集子任务状态轨迹 key
	subTaskKeys, err := r.client.Keys(ctx, r.prefix+":subtask_states:*"+taskID+"*").Result()
	if err != nil {
		return err
	}
	keys = append(keys, subTaskKeys...)

	if _, err := r.client.Del(ctx, keys...).Result(); err != nil {
		return err
	}

	// 清理二级索引
	if r.indexEnabled {
		states := []State{PendingState, CreatedState, SuccessState, FailedState, CanceledState}
		idxKeys := make([]string, 0, len(states)+1)
		for _, s := range states {
			idxKeys = append(idxKeys, r.keyIdxState(s))
		}
		idxKeys = append(idxKeys, r.keyIdxCreated())

		if _, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for _, idxKey := range idxKeys {
				pipe.ZRem(ctx, idxKey, taskID)
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

// ======================== 批量操作 ========================

// LoadSnapshots 批量加载快照。
// 返回 map[taskID] → ExecutionSnapshot，不存在的 taskID 不会出现在返回中。
func (r *RedisStorage[D, S]) LoadSnapshots(ctx context.Context, taskIDs []string) (map[string]ExecutionSnapshot, error) {
	if len(taskIDs) == 0 {
		return map[string]ExecutionSnapshot{}, nil
	}

	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(taskIDs))
	for i, taskID := range taskIDs {
		cmds[i] = pipe.Get(ctx, r.keySnapshot(taskID))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string]ExecutionSnapshot, len(taskIDs))
	for i, taskID := range taskIDs {
		data, err := cmds[i].Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load snapshot for %s: %w", taskID, err)
		}
		var snapshot ExecutionSnapshot
		if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
			return nil, fmt.Errorf("unmarshal snapshot for %s: %w", taskID, err)
		}
		result[taskID] = snapshot
	}
	return result, nil
}

// LoadTaskStates 批量查询任务当前状态。
// 返回 map[taskID] → State，不存在的 taskID 不会出现在返回中。
func (r *RedisStorage[D, S]) LoadTaskStates(ctx context.Context, taskIDs []string) (map[string]State, error) {
	if len(taskIDs) == 0 {
		return map[string]State{}, nil
	}

	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(taskIDs))
	for i, taskID := range taskIDs {
		cmds[i] = pipe.Get(ctx, r.keyTaskState(taskID))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string]State, len(taskIDs))
	for i, taskID := range taskIDs {
		data, err := cmds[i].Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load state for %s: %w", taskID, err)
		}
		result[taskID] = State(data)
	}
	return result, nil
}
