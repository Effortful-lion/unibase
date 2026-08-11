package state

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/Effortful-lion/unibase/logx"
)

// SubFlowProgress 保存单段子任务流程的执行进度。
type SubFlowProgress struct {
	FlowKey           string
	EntryState        string
	GenerationDone    bool
	TotalSubTasks     int
	CompletedSubTasks int
	FailedSubTasks    int
	CurrentSubTaskIdx int
}

// TaskContext 保存一次任务执行过程中的运行时上下文与状态。
type TaskContext[D Task, S any] struct {
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	ended           atomic.Bool
	task            D
	store           TaskStorage[D, S]
	tmpStore        map[string]any
	storage         S
	subTaskLoader   SubTaskLoaderFunc[D, S]
	state           State
	fromState       State
	toState         State
	activeSubFlow   State
	subFlowOrder    []State
	subFlowTasks    map[State][]subTaskRuntime
	subTasks        []subTaskRuntime
	currentSubIndex int
	metrics         *internalMetrics
	logger          *logx.Logger
}

// subTaskRuntime 是子任务在内存中的运行时记录。
type subTaskRuntime struct {
	record  SubTaskRecord
	inst    SubTask
	lastErr error
}

// newTaskContext 创建一次任务执行期间使用的上下文对象。
func newTaskContext[D Task, S any](ctx context.Context, task D, store TaskStorage[D, S], storage S, initialState State, logger *logx.Logger) *TaskContext[D, S] {
	if initialState == "" {
		initialState = CreatedState
	}
	return &TaskContext[D, S]{
		ctx:             ctx,
		task:            task,
		store:           store,
		tmpStore:        make(map[string]any),
		storage:         storage,
		state:           initialState,
		subFlowTasks:    make(map[State][]subTaskRuntime),
		currentSubIndex: -1,
		metrics:         newInternalMetrics(),
		logger:          logger,
	}
}

// bindLifecycle 绑定本次任务执行过程中需要统一收口的资源。
func (t *TaskContext[D, S]) bindLifecycle(ctx context.Context, cancel context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ctx = ctx
	t.cancel = cancel
}

// Context 返回当前任务执行绑定的上下文。
func (t *TaskContext[D, S]) Context() context.Context { return t.ctx }

// Get 返回上下文中临时存储的值；如果不存在则返回 nil。
func (t *TaskContext[D, S]) Get(key string) any {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tmpStore[key]
}

// Set 把一个值放到上下文的临时存储里；这个存储只在本次任务执行期间有效。
func (t *TaskContext[D, S]) Set(key string, value any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tmpStore[key] = value
}

// GetTask 返回当前任务对象。
func (t *TaskContext[D, S]) GetTask() D { return t.task }

// Storage 返回当前执行绑定的业务存储句柄。
func (t *TaskContext[D, S]) Storage() S { return t.storage }

// GetState 返回当前已提交的状态。
func (t *TaskContext[D, S]) GetState() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state
}

// FromState 返回当前正在执行的转换源状态。
func (t *TaskContext[D, S]) FromState() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.fromState
}

// ToState 返回当前正在执行的转换目标状态。
func (t *TaskContext[D, S]) ToState() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.toState
}

// ActiveSubFlow 返回当前活跃子任务流程入口；若无则为空。
func (t *TaskContext[D, S]) ActiveSubFlow() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.activeSubFlow
}

// CurrentSubTask 返回当前正在执行的子任务对象。
// 如果当前只恢复出了子任务记录、但还没注册加载器，则返回 nil。
func (t *TaskContext[D, S]) CurrentSubTask() SubTask {
	record := t.CurrentSubTaskRef()
	if record.SubTaskID == "" {
		return nil
	}
	item, err := t.LoadSubTask(record)
	if err != nil {
		return nil
	}
	return item
}

// CurrentSubTaskIndex 返回当前正在执行子任务的索引位置。
func (t *TaskContext[D, S]) CurrentSubTaskIndex() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentSubIndex
}

// CurrentSubTaskRef 返回当前子任务的运行记录。
func (t *TaskContext[D, S]) CurrentSubTaskRef() SubTaskRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.currentSubIndex < 0 || t.currentSubIndex >= len(t.subTasks) {
		return SubTaskRecord{}
	}
	return buildSubTaskRecord(t.subTasks[t.currentSubIndex], t.activeSubFlow, t.currentSubIndex)
}

// SubTasks 按稳定顺序返回当前已知的全部子任务对象。
func (t *TaskContext[D, S]) SubTasks() []SubTask {
	records := t.SubTaskRefs()
	items := make([]SubTask, 0, len(records))
	for _, record := range records {
		item, err := t.LoadSubTask(record)
		if err == nil && item != nil {
			items = append(items, item)
		}
	}
	return items
}

// SubTaskRefs 按稳定顺序返回当前已知的全部子任务运行记录。
func (t *TaskContext[D, S]) SubTaskRefs() []SubTaskRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()

	items := make([]SubTaskRecord, 0)
	seen := make(map[State]struct{}, len(t.subFlowOrder))
	for _, entry := range t.subFlowOrder {
		seen[entry] = struct{}{}
		for idx, subTask := range t.subTasksForLocked(entry) {
			items = append(items, buildSubTaskRecord(subTask, entry, idx))
		}
	}
	if t.activeSubFlow != "" {
		if _, ok := seen[t.activeSubFlow]; !ok {
			for idx, subTask := range t.subTasks {
				items = append(items, buildSubTaskRecord(subTask, t.activeSubFlow, idx))
			}
		}
	}
	return items
}

// LoadSubTask 先尝试从当前运行时缓存中读取子任务对象；缓存没有时，再走业务注册的加载器。
func (t *TaskContext[D, S]) LoadSubTask(record SubTaskRecord) (SubTask, error) {
	if t == nil {
		return nil, ErrSubTaskNotLoaded
	}

	t.mu.RLock()
	if cached := t.findSubTaskLocked(record); cached != nil {
		t.mu.RUnlock()
		return cached, nil
	}
	loader := t.subTaskLoader
	task := t.task
	ctx := t.ctx
	storage := t.storage
	t.mu.RUnlock()

	if loader == nil {
		return nil, ErrSubTaskLoaderNotRegistered
	}
	loaded, err := loader(ctx, task, record, storage)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, ErrSubTaskNotLoaded
	}

	t.cacheLoadedSubTask(record, loaded)
	return loaded, nil
}

// CreateSubTask 通过构造闭包创建一个子任务，并把它插入到当前活跃子任务流程。
func (t *TaskContext[D, S]) CreateSubTask(index int, build func(context.Context, S) (SubTask, error)) error {
	if build == nil {
		return ErrSubTaskNotLoaded
	}
	subTask, err := build(t.ctx, t.storage)
	if err != nil {
		return err
	}
	if subTask == nil {
		return ErrSubTaskNotLoaded
	}
	record := SubTaskRecord{
		SubTaskID:    subTask.GetSubTaskId(),
		ParentTaskID: subTask.GetParentTaskId(),
		State:        CreatedState,
	}
	return t.createSubTaskRuntime(record, subTask, index)
}

// CreateSubTaskRecord 将一条子任务记录追加或插入到当前活跃子任务流程，并持久化该列表。
func (t *TaskContext[D, S]) CreateSubTaskRecord(record SubTaskRecord, index int) error {
	return t.createSubTaskRuntime(record, nil, index)
}

func (t *TaskContext[D, S]) createSubTaskRuntime(record SubTaskRecord, inst SubTask, index int) error {
	t.mu.Lock()
	record = normalizeSubTaskRecord(record, t.activeSubFlow, index)
	runtimeTask := subTaskRuntime{record: record, inst: inst}
	if index < 0 || index >= len(t.subTasks) {
		t.subTasks = append(t.subTasks, runtimeTask)
		t.reindexSubTasksLocked(t.activeSubFlow)
		t.syncActiveSubTasksLocked()
		t.mu.Unlock()
		return t.persistSubTasks()
	}

	t.subTasks = append(t.subTasks, subTaskRuntime{})
	copy(t.subTasks[index+1:], t.subTasks[index:])
	t.subTasks[index] = runtimeTask
	t.reindexSubTasksLocked(t.activeSubFlow)
	t.syncActiveSubTasksLocked()
	t.mu.Unlock()
	return t.persistSubTasks()
}

// setTransition 记录当前正在执行的状态切换。
func (t *TaskContext[D, S]) setTransition(from, to State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fromState = from
	t.toState = to
}

// setState 更新当前已经成功提交的任务状态。
func (t *TaskContext[D, S]) setState(next State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = next
}

// setActiveSubFlow 切换当前活跃的子任务阶段。
func (t *TaskContext[D, S]) setActiveSubFlow(entry State) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeSubFlow == entry {
		return
	}
	t.syncActiveSubTasksLocked()
	t.activeSubFlow = entry
	t.ensureSubFlowOrderLocked(entry)
	t.subTasks = t.subFlowTasks[entry]
	t.reindexSubTasksLocked(entry)
	t.currentSubIndex = -1
}

// setCurrentSubTask 更新当前正在执行的子任务位置。
func (t *TaskContext[D, S]) setCurrentSubTask(idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentSubIndex = idx
}

// loadSubTasks 用一组已保存的子任务记录覆盖当前活跃阶段的内存列表。
func (t *TaskContext[D, S]) loadSubTasks(items []SubTaskRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.subTasks = cloneSnapshotSubTasks(items)
	t.reindexSubTasksLocked(t.activeSubFlow)
	t.syncActiveSubTasksLocked()
}

// preloadSnapshot 预加载整条任务的已保存现场。
func (t *TaskContext[D, S]) preloadSnapshot(snapshot ExecutionSnapshot, mainPath []State) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(snapshot.SubFlows) == 0 {
		return
	}

	seen := make(map[State]struct{}, len(snapshot.SubFlows))
	for _, state := range mainPath {
		items, ok := snapshot.SubFlows[state]
		if !ok {
			continue
		}
		t.ensureSubFlowOrderLocked(state)
		t.subFlowTasks[state] = cloneSnapshotSubTasks(items)
		t.reindexSubTasksLocked(state)
		seen[state] = struct{}{}
	}

	leftover := make([]string, 0, len(snapshot.SubFlows)-len(seen))
	for entry := range snapshot.SubFlows {
		if _, ok := seen[entry]; ok {
			continue
		}
		leftover = append(leftover, string(entry))
	}
	sort.Strings(leftover)
	for _, entry := range leftover {
		state := State(entry)
		t.ensureSubFlowOrderLocked(state)
		t.subFlowTasks[state] = cloneSnapshotSubTasks(snapshot.SubFlows[state])
		t.reindexSubTasksLocked(state)
	}
}

// persistSubTasks 持久化当前子任务列表。
func (t *TaskContext[D, S]) persistSubTasks() error {
	t.mu.RLock()
	if t.store == nil || t.activeSubFlow == "" {
		t.mu.RUnlock()
		return nil
	}
	items := cloneRuntimeSubTasks(t.activeSubFlow, t.subTasks)
	store := t.store
	ctx := t.ctx
	taskID := t.task.ID()
	activeSubFlow := t.activeSubFlow
	t.mu.RUnlock()
	return store.SaveSubTasks(ctx, taskID, activeSubFlow, items)
}

// ensureSubFlowOrderLocked 在持锁状态下补充子任务阶段顺序。
func (t *TaskContext[D, S]) ensureSubFlowOrderLocked(entry State) {
	if entry == "" {
		return
	}
	for _, item := range t.subFlowOrder {
		if item == entry {
			return
		}
	}
	t.subFlowOrder = append(t.subFlowOrder, entry)
}

// syncActiveSubTasksLocked 在持锁状态下同步当前活跃阶段的数据。
func (t *TaskContext[D, S]) syncActiveSubTasksLocked() {
	if t.activeSubFlow == "" {
		return
	}
	t.subFlowTasks[t.activeSubFlow] = t.subTasks
}

func (t *TaskContext[D, S]) activeSubTaskCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.subTasks)
}

func (t *TaskContext[D, S]) subTaskRuntimeAt(idx int) (subTaskRuntime, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if idx < 0 || idx >= len(t.subTasks) {
		return subTaskRuntime{}, false
	}
	return t.subTasks[idx], true
}

func (t *TaskContext[D, S]) incrementSubTaskAttempts(idx int) (subTaskRuntime, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if idx < 0 || idx >= len(t.subTasks) {
		return subTaskRuntime{}, false
	}
	t.subTasks[idx].record.Attempts++
	t.syncActiveSubTasksLocked()
	return t.subTasks[idx], true
}

func (t *TaskContext[D, S]) setSubTaskState(idx int, state State, lastErr error) (subTaskRuntime, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if idx < 0 || idx >= len(t.subTasks) {
		return subTaskRuntime{}, false
	}
	t.subTasks[idx].record.State = state
	t.subTasks[idx].lastErr = lastErr
	t.syncActiveSubTasksLocked()
	return t.subTasks[idx], true
}

func (t *TaskContext[D, S]) markSubTaskFailedIfPending(idx int, reason error) (subTaskRuntime, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if idx < 0 || idx >= len(t.subTasks) {
		return subTaskRuntime{}, false
	}
	if IsFinalState(t.subTasks[idx].record.State) {
		return subTaskRuntime{}, false
	}
	t.subTasks[idx].record.State = FailedState
	t.subTasks[idx].lastErr = reason
	t.syncActiveSubTasksLocked()
	return t.subTasks[idx], true
}

func (t *TaskContext[D, S]) activeSubTaskSnapshot() (State, []subTaskRuntime, int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.activeSubFlow, append([]subTaskRuntime(nil), t.subTasks...), t.currentSubIndex
}

// subTasksForLocked 在持锁状态下读取某个子任务阶段的子任务列表。
func (t *TaskContext[D, S]) subTasksForLocked(entry State) []subTaskRuntime {
	if entry == "" {
		return nil
	}
	if entry == t.activeSubFlow {
		return t.subTasks
	}
	return t.subFlowTasks[entry]
}

func (t *TaskContext[D, S]) findSubTaskLocked(record SubTaskRecord) SubTask {
	if record.SubTaskID == "" {
		return nil
	}
	for _, subTask := range t.subTasks {
		if subTask.record.SubTaskID == record.SubTaskID && subTask.inst != nil {
			return subTask.inst
		}
	}
	for _, entry := range t.subFlowOrder {
		if entry == t.activeSubFlow {
			continue
		}
		for _, subTask := range t.subFlowTasks[entry] {
			if subTask.record.SubTaskID == record.SubTaskID && subTask.inst != nil {
				return subTask.inst
			}
		}
	}
	return nil
}

func (t *TaskContext[D, S]) cacheLoadedSubTask(record SubTaskRecord, inst SubTask) {
	if inst == nil || record.SubTaskID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for idx := range t.subTasks {
		if t.subTasks[idx].record.SubTaskID == record.SubTaskID {
			t.subTasks[idx].inst = inst
			return
		}
	}
	for _, entry := range t.subFlowOrder {
		if entry == t.activeSubFlow {
			continue
		}
		items := t.subFlowTasks[entry]
		for idx := range items {
			if items[idx].record.SubTaskID == record.SubTaskID {
				items[idx].inst = inst
				t.subFlowTasks[entry] = items
				return
			}
		}
	}
}

func normalizeSubTaskRecord(record SubTaskRecord, entry State, idx int) SubTaskRecord {
	if entry != "" {
		record.FlowKey = entry
		record.EntryState = entry
	}
	record.Index = idx
	if record.State == "" {
		record.State = CreatedState
	}
	return record
}

func buildSubTaskRecord(item subTaskRuntime, entry State, idx int) SubTaskRecord {
	record := item.record
	if item.inst != nil {
		record.SubTaskID = item.inst.GetSubTaskId()
		record.ParentTaskID = item.inst.GetParentTaskId()
	}
	record = normalizeSubTaskRecord(record, entry, idx)
	return record
}

// cloneSnapshotSubTasks 把已保存的子任务记录转成内存中的运行时结构。
func cloneSnapshotSubTasks(items []SubTaskRecord) []subTaskRuntime {
	out := make([]subTaskRuntime, 0, len(items))
	for _, item := range items {
		out = append(out, subTaskRuntime{record: item})
	}
	return out
}

// snapshot 生成当前任务的执行现场。
func (t *TaskContext[D, S]) snapshot() ExecutionSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := ExecutionSnapshot{
		State:    t.state,
		SubFlows: make(map[State][]SubTaskRecord, len(t.subFlowTasks)),
	}
	for _, entry := range t.subFlowOrder {
		items := t.subFlowTasks[entry]
		if entry == t.activeSubFlow {
			items = t.subTasks
		}
		out.SubFlows[entry] = cloneRuntimeSubTasks(entry, items)
	}
	if t.activeSubFlow != "" {
		if _, ok := out.SubFlows[t.activeSubFlow]; !ok {
			out.SubFlows[t.activeSubFlow] = cloneRuntimeSubTasks(t.activeSubFlow, t.subTasks)
		}
	}
	return out
}

// cloneRuntimeSubTasks 把内存里的子任务运行记录转成可保存的结构。
func cloneRuntimeSubTasks(entry State, items []subTaskRuntime) []SubTaskRecord {
	out := make([]SubTaskRecord, 0, len(items))
	for idx, item := range items {
		out = append(out, buildSubTaskRecord(item, entry, idx))
	}
	return out
}

func (t *TaskContext[D, S]) reindexSubTasksLocked(entry State) {
	for idx := range t.subTasks {
		t.subTasks[idx].record = normalizeSubTaskRecord(t.subTasks[idx].record, entry, idx)
	}
}

// taskEnd 用来结束一次任务执行，并保证收尾逻辑只执行一次。
func (t *TaskContext[D, S]) taskEnd() error {
	if t == nil || !t.ended.CompareAndSwap(false, true) {
		return nil
	}
	t.mu.RLock()
	cancel := t.cancel
	t.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// internalMetrics 内部指标，通过 atomic 更新。
type internalMetrics struct {
	processCount atomic.Int64
	elapsedNanos atomic.Int64
}

// newInternalMetrics 创建内部指标实例。
func newInternalMetrics() *internalMetrics {
	return &internalMetrics{}
}
