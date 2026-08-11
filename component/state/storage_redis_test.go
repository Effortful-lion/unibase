package state

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupRedis(t *testing.T) (*RedisStorage[*testTask, *testSubTask], *redis.Client) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	storage := NewRedisStorage[*testTask, *testSubTask](client)
	return storage, client
}

// ======================== SaveTaskState ========================

func TestRedisStorage_SaveTaskState(t *testing.T) {
	storage, _ := setupRedis(t)

	err := storage.SaveTaskState(context.Background(), "task-1", CreatedState)
	if err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	// 验证状态已存入 List
	states, err := storage.client.LRange(context.Background(), "state:task_states:task-1", 0, -1).Result()
	if err != nil {
		t.Fatalf("LRange: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state entry, got %d", len(states))
	}
}

func TestRedisStorage_SaveTaskState_Multiple(t *testing.T) {
	storage, _ := setupRedis(t)

	_ = storage.SaveTaskState(context.Background(), "task-1", PendingState)
	_ = storage.SaveTaskState(context.Background(), "task-1", CreatedState)
	_ = storage.SaveTaskState(context.Background(), "task-1", SuccessState)

	states, _ := storage.client.LRange(context.Background(), "state:task_states:task-1", 0, -1).Result()
	if len(states) != 3 {
		t.Fatalf("expected 3 state entries, got %d", len(states))
	}
}

// ======================== SaveSubTasks ========================

func TestRedisStorage_SaveSubTasks(t *testing.T) {
	storage, _ := setupRedis(t)

	subTasks := []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1", State: CreatedState, Index: 0},
		{SubTaskID: "sub-2", ParentTaskID: "task-1", State: CreatedState, Index: 1},
	}

	err := storage.SaveSubTasks(context.Background(), "task-1", CreatedState, subTasks)
	if err != nil {
		t.Fatalf("SaveSubTasks: %v", err)
	}

	// 验证子任务列表已存入 Hash
	result, err := storage.client.HGet(context.Background(), "state:subtasks:task-1", "created").Result()
	if err != nil {
		t.Fatalf("HGet: %v", err)
	}
	if result == "" {
		t.Fatal("expected subtask data in hash, got empty")
	}
}

func TestRedisStorage_SaveSubTasks_Empty(t *testing.T) {
	storage, _ := setupRedis(t)

	// 空子任务列表不应报错
	err := storage.SaveSubTasks(context.Background(), "task-1", CreatedState, nil)
	if err != nil {
		t.Fatalf("SaveSubTasks with nil: %v", err)
	}

	// Hash 不应该被创建
	exists, err := storage.client.Exists(context.Background(), "state:subtasks:task-1").Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists != 0 {
		t.Fatal("expected no key created for empty subtasks")
	}
}

func TestRedisStorage_SaveSubTasks_MultipleEntries(t *testing.T) {
	storage, _ := setupRedis(t)

	subTasks1 := []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1", State: CreatedState, Index: 0},
	}
	subTasks2 := []SubTaskRecord{
		{SubTaskID: "sub-3", ParentTaskID: "task-1", State: SuccessState, Index: 2},
	}

	_ = storage.SaveSubTasks(context.Background(), "task-1", CreatedState, subTasks1)
	_ = storage.SaveSubTasks(context.Background(), "task-1", SuccessState, subTasks2)

	// 验证两个 entry 都存在
	v1, _ := storage.client.HExists(context.Background(), "state:subtasks:task-1", "created").Result()
	v2, _ := storage.client.HExists(context.Background(), "state:subtasks:task-1", "success").Result()
	if !v1 {
		t.Fatal("expected 'created' entry in hash")
	}
	if !v2 {
		t.Fatal("expected 'success' entry in hash")
	}
}

// ======================== SaveSubTaskState ========================

func TestRedisStorage_SaveSubTaskState(t *testing.T) {
	storage, _ := setupRedis(t)

	err := storage.SaveSubTaskState(context.Background(), "task-1", CreatedState, "sub-1", SuccessState)
	if err != nil {
		t.Fatalf("SaveSubTaskState: %v", err)
	}

	states, _ := storage.client.LRange(context.Background(), "state:subtask_states:sub-1", 0, -1).Result()
	if len(states) != 1 {
		t.Fatalf("expected 1 subtask state entry, got %d", len(states))
	}
}

// ======================== LoadSnapshot ========================

func TestRedisStorage_LoadSaveSnapshot(t *testing.T) {
	storage, _ := setupRedis(t)

	snapshot := ExecutionSnapshot{
		State:    CreatedState,
		SubFlows: map[State][]SubTaskRecord{CreatedState: {{SubTaskID: "sub-1"}}},
	}

	err := storage.SaveSnapshot(context.Background(), "task-1", snapshot)
	if err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	loaded, err := storage.LoadSnapshot(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.State != CreatedState {
		t.Fatalf("snapshot state mismatch: got %v, want %v", loaded.State, CreatedState)
	}
	if len(loaded.SubFlows) != 1 {
		t.Fatalf("snapshot subflows count: got %d, want 1", len(loaded.SubFlows))
	}
}

func TestRedisStorage_LoadSnapshot_NotFound(t *testing.T) {
	storage, _ := setupRedis(t)

	snapshot, err := storage.LoadSnapshot(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snapshot.State != "" {
		t.Fatalf("expected empty snapshot, got state=%v", snapshot.State)
	}
}

func TestRedisStorage_SaveSnapshot_Overwrite(t *testing.T) {
	storage, _ := setupRedis(t)

	s1 := ExecutionSnapshot{State: CreatedState}
	s2 := ExecutionSnapshot{State: SuccessState}

	_ = storage.SaveSnapshot(context.Background(), "task-1", s1)
	_ = storage.SaveSnapshot(context.Background(), "task-1", s2)

	loaded, _ := storage.LoadSnapshot(context.Background(), "task-1")
	if loaded.State != SuccessState {
		t.Fatalf("expected overwritten state, got %v", loaded.State)
	}
}

// ======================== Options ========================

func TestRedisStorage_WithPrefix(t *testing.T) {
	_, client := setupRedis(t)

	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("myapp"),
	)

	_ = storage.SaveTaskState(context.Background(), "task-1", CreatedState)

	// 验证 key 使用了自定义前缀
	exists, err := client.Exists(context.Background(), "myapp:task_states:task-1").Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists == 0 {
		t.Fatal("expected key with custom prefix 'myapp'")
	}

	// 默认前缀的 key 不应该存在
	exists, _ = client.Exists(context.Background(), "state:task_states:task-1").Result()
	if exists != 0 {
		t.Fatal("expected no key with default prefix")
	}
}

func TestRedisStorage_WithTTL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ttl := 2 * time.Second
	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("ttl_test"),
		WithRedisTTL(ttl),
	)

	subTasks := []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1", State: CreatedState, Index: 0},
	}
	_ = storage.SaveSubTasks(context.Background(), "task-1", CreatedState, subTasks)

	// 立即检查 TTL 已设置
	remaining, err := client.TTL(context.Background(), "ttl_test:subtasks:task-1").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if remaining <= 0 || remaining > ttl {
		t.Fatalf("expected TTL around %v, got %v", ttl, remaining)
	}

	// 使用 FastForward 模拟 TTL 过期
	mr.FastForward(ttl + time.Second)

	exists, _ := client.Exists(context.Background(), "ttl_test:subtasks:task-1").Result()
	if exists != 0 {
		t.Fatal("expected key to expire after TTL")
	}
}

func TestRedisStorage_NoTTL(t *testing.T) {
	_, client := setupRedis(t)

	// 使用 WithRedisTTL(0) 显式指定永不过期
	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("no_ttl"),
		WithRedisTTL(0),
	)

	subTasks := []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1", State: CreatedState, Index: 0},
	}
	_ = storage.SaveSubTasks(context.Background(), "task-1", CreatedState, subTasks)

	// TTL 应该为 -1（永不过期）
	remaining, err := client.TTL(context.Background(), "no_ttl:subtasks:task-1").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if remaining != -1 {
		t.Fatalf("expected no TTL (-1), got %v", remaining)
	}
}

func TestRedisStorage_DefaultTTL(t *testing.T) {
	_, client := setupRedis(t)

	// 不指定 TTL，使用默认值
	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("default_ttl"),
	)

	subTasks := []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1", State: CreatedState, Index: 0},
	}
	_ = storage.SaveSubTasks(context.Background(), "task-1", CreatedState, subTasks)

	// 默认 TTL 应为 7 天
	remaining, err := client.TTL(context.Background(), "default_ttl:subtasks:task-1").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	expectedTTL := 7 * 24 * time.Hour
	if remaining > expectedTTL || remaining <= 0 {
		t.Fatalf("expected default TTL ~%v, got %v", expectedTTL, remaining)
	}
}

// ======================== Integration ========================

func TestRedisStorage_FullLifecycle(t *testing.T) {
	storage, _ := setupRedis(t)
	ctx := context.Background()

	// 1. 保存任务状态轨迹
	_ = storage.SaveTaskState(ctx, "task-1", PendingState)
	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-1", SuccessState)

	// 2. 保存子任务列表
	subTasks := []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1", FlowKey: CreatedState, EntryState: CreatedState, State: SuccessState, Index: 0},
		{SubTaskID: "sub-2", ParentTaskID: "task-1", FlowKey: CreatedState, EntryState: CreatedState, State: SuccessState, Index: 1},
	}
	_ = storage.SaveSubTasks(ctx, "task-1", CreatedState, subTasks)

	// 3. 保存子任务状态
	_ = storage.SaveSubTaskState(ctx, "task-1", CreatedState, "sub-1", SuccessState)
	_ = storage.SaveSubTaskState(ctx, "task-1", CreatedState, "sub-2", SuccessState)

	// 4. 保存快照
	snapshot := ExecutionSnapshot{
		State:    SuccessState,
		SubFlows: map[State][]SubTaskRecord{CreatedState: subTasks},
	}
	_ = storage.SaveSnapshot(ctx, "task-1", snapshot)

	// 5. 加载快照并验证
	loaded, err := storage.LoadSnapshot(ctx, "task-1")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.State != SuccessState {
		t.Fatalf("snapshot state: got %v, want %v", loaded.State, SuccessState)
	}
	if len(loaded.SubFlows) != 1 {
		t.Fatalf("snapshot subflows: got %d, want 1", len(loaded.SubFlows))
	}

	// 6. 验证状态轨迹
	taskStates, _ := storage.client.LRange(ctx, "state:task_states:task-1", 0, -1).Result()
	if len(taskStates) != 3 {
		t.Fatalf("task states: got %d, want 3", len(taskStates))
	}

	// 7. 验证子任务状态轨迹
	subStates, _ := storage.client.LRange(ctx, "state:subtask_states:sub-1", 0, -1).Result()
	if len(subStates) != 1 {
		t.Fatalf("subtask states: got %d, want 1", len(subStates))
	}
}

// ======================== 二级索引 ========================

func TestRedisStorage_Indexing_StateQuery(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("idx_test"),
		WithRedisIndexing(),
	)
	ctx := context.Background()

	// 保存多个任务的不同状态
	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-1", SuccessState)
	_ = storage.SaveTaskState(ctx, "task-2", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-2", FailedState)
	_ = storage.SaveTaskState(ctx, "task-3", SuccessState)

	// 查询 success 状态的任务
	successTasks, _, err := storage.ListTasksByState(ctx, SuccessState, "", 10)
	if err != nil {
		t.Fatalf("ListTasksByState: %v", err)
	}
	if len(successTasks) != 2 {
		t.Fatalf("expected 2 success tasks, got %d: %v", len(successTasks), successTasks)
	}

	// 查询 failed 状态的任务
	failedTasks, _, err := storage.ListTasksByState(ctx, FailedState, "", 10)
	if err != nil {
		t.Fatalf("ListTasksByState: %v", err)
	}
	if len(failedTasks) != 1 {
		t.Fatalf("expected 1 failed task, got %d", len(failedTasks))
	}

	// 查询不存在的状态
	noTasks, _, err := storage.ListTasksByState(ctx, State("nonexistent"), "", 10)
	if err != nil {
		t.Fatalf("ListTasksByState: %v", err)
	}
	if len(noTasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(noTasks))
	}
}

func TestRedisStorage_Indexing_StateUpdate(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("idx_update"),
		WithRedisIndexing(),
	)
	ctx := context.Background()

	// 任务从 created → success
	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-1", SuccessState)

	// created 索引不应再包含 task-1
	createdTasks, _, _ := storage.ListTasksByState(ctx, CreatedState, "", 10)
	for _, id := range createdTasks {
		if id == "task-1" {
			t.Fatal("task-1 should not be in created index after transitioning to success")
		}
	}

	// success 索引应包含 task-1
	successTasks, _, _ := storage.ListTasksByState(ctx, SuccessState, "", 10)
	found := false
	for _, id := range successTasks {
		if id == "task-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("task-1 should be in success index")
	}
}

func TestRedisStorage_Indexing_TimeQuery(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("idx_time"),
		WithRedisIndexing(),
	)
	ctx := context.Background()

	now := time.Now()
	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-2", CreatedState)

	// 查询最近 1 分钟内的任务
	tasks, _, err := storage.ListTasksByTime(ctx, now.Unix()-60, 0, "", 10)
	if err != nil {
		t.Fatalf("ListTasksByTime: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks in last minute, got %d", len(tasks))
	}

	// 查询 1 小时前的任务（应无结果）
	tasks, _, err = storage.ListTasksByTime(ctx, 0, now.Unix()-3600, "", 10)
	if err != nil {
		t.Fatalf("ListTasksByTime: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks before 1 hour ago, got %d", len(tasks))
	}
}

// ======================== 查询 API ========================

func TestRedisStorage_GetTaskState(t *testing.T) {
	storage, _ := setupRedis(t)
	ctx := context.Background()

	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-1", SuccessState)

	state, err := storage.GetTaskState(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetTaskState: %v", err)
	}
	if state != SuccessState {
		t.Fatalf("expected state %s, got %s", SuccessState, state)
	}

	// 不存在的任务
	_, err = storage.GetTaskState(ctx, "nonexistent")
	if err != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}

// ======================== 批量操作 ========================

func TestRedisStorage_LoadSnapshots(t *testing.T) {
	storage, _ := setupRedis(t)
	ctx := context.Background()

	_ = storage.SaveSnapshot(ctx, "task-1", ExecutionSnapshot{State: SuccessState})
	_ = storage.SaveSnapshot(ctx, "task-2", ExecutionSnapshot{State: CreatedState})

	snapshots, err := storage.LoadSnapshots(ctx, []string{"task-1", "task-2", "task-3"})
	if err != nil {
		t.Fatalf("LoadSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}
	if snapshots["task-1"].State != SuccessState {
		t.Fatalf("task-1 state: got %v, want %v", snapshots["task-1"].State, SuccessState)
	}
}

func TestRedisStorage_LoadTaskStates(t *testing.T) {
	storage, _ := setupRedis(t)
	ctx := context.Background()

	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveTaskState(ctx, "task-1", SuccessState)
	_ = storage.SaveTaskState(ctx, "task-2", FailedState)

	states, err := storage.LoadTaskStates(ctx, []string{"task-1", "task-2", "task-3"})
	if err != nil {
		t.Fatalf("LoadTaskStates: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
	if states["task-1"] != SuccessState {
		t.Fatalf("task-1 state: got %v, want %v", states["task-1"], SuccessState)
	}
	if states["task-2"] != FailedState {
		t.Fatalf("task-2 state: got %v, want %v", states["task-2"], FailedState)
	}
}

// ======================== 删除 ========================

func TestRedisStorage_DeleteTask(t *testing.T) {
	storage, _ := setupRedis(t)
	ctx := context.Background()

	_ = storage.SaveTaskState(ctx, "task-1", CreatedState)
	_ = storage.SaveSnapshot(ctx, "task-1", ExecutionSnapshot{State: CreatedState})
	_ = storage.SaveSubTasks(ctx, "task-1", CreatedState, []SubTaskRecord{
		{SubTaskID: "sub-1", ParentTaskID: "task-1"},
	})

	err := storage.DeleteTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// 验证所有关联 key 已删除
	keys := []string{
		"state:task_states:task-1",
		"state:task_state:task-1",
		"state:subtasks:task-1",
		"state:snapshot:task-1",
	}
	for _, key := range keys {
		exists, _ := storage.client.Exists(ctx, key).Result()
		if exists != 0 {
			t.Fatalf("expected key %q to be deleted", key)
		}
	}
}

// ======================== 缓存层 ========================

func TestCachedTaskStorage(t *testing.T) {
	_, client := setupRedis(t)

	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("cache_test"),
	)
	cached := NewCachedTaskStorage[*testTask, *testSubTask](storage,
		WithCacheMaxEntries(100),
		WithCacheSnapshotTTL(5*time.Minute),
	)
	ctx := context.Background()

	// 首次加载：缓存未命中
	snapshot, err := cached.LoadSnapshot(ctx, "task-1")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snapshot.State != "" {
		t.Fatalf("expected empty snapshot, got state=%v", snapshot.State)
	}

	// 保存快照
	_ = cached.SaveSnapshot(ctx, "task-1", ExecutionSnapshot{State: SuccessState})

	// 再次加载：缓存命中
	snapshot, err = cached.LoadSnapshot(ctx, "task-1")
	if err != nil {
		t.Fatalf("LoadSnapshot cached: %v", err)
	}
	if snapshot.State != SuccessState {
		t.Fatalf("cached snapshot state: got %v, want %v", snapshot.State, SuccessState)
	}

	// 验证缓存指标
	metrics := cached.CacheMetrics()
	if metrics.Hits < 1 {
		t.Fatalf("expected at least 1 cache hit, got %d", metrics.Hits)
	}
	if metrics.Total < 2 {
		t.Fatalf("expected at least 2 total loads, got %d", metrics.Total)
	}
}

func TestCachedTaskStorage_InvalidateOnWrite(t *testing.T) {
	_, client := setupRedis(t)

	storage := NewRedisStorage[*testTask, *testSubTask](client,
		WithRedisPrefix("cache_inv"),
	)
	cached := NewCachedTaskStorage[*testTask, *testSubTask](storage)
	ctx := context.Background()

	_ = cached.SaveSnapshot(ctx, "task-1", ExecutionSnapshot{State: CreatedState})
	_, _ = cached.LoadSnapshot(ctx, "task-1") // warm cache

	// 写入新快照应淘汰旧缓存
	_ = cached.SaveSnapshot(ctx, "task-1", ExecutionSnapshot{State: SuccessState})

	snapshot, err := cached.LoadSnapshot(ctx, "task-1")
	if err != nil {
		t.Fatalf("LoadSnapshot after invalidation: %v", err)
	}
	if snapshot.State != SuccessState {
		t.Fatalf("expected updated state %s, got %s", SuccessState, snapshot.State)
	}
}
