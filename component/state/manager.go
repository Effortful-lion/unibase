package state

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Effortful-lion/unibase/logx"
)

// activeTask 记录当前进程里正在执行的一条任务。
type activeTask[D Task, S any] struct {
	taskCtx *TaskContext[D, S]
	cancel  context.CancelFunc
}

// Manager 是 state 对外唯一的状态机管理器。
type Manager[D Task, S any] struct {
	ctx           context.Context
	store         TaskStorage[D, S]
	loadTask      func(context.Context, string) (D, error)
	storage       S
	subTaskLoader SubTaskLoaderFunc[D, S]
	compiled      *compiledDefinition[D, S]
	options       options
	activeMu      sync.Mutex
	active        map[string]activeTask[D, S]
	taskMu        sync.Map // map[string]*sync.Mutex，每个 taskID 一把进程内锁
	process       atomic.Int64
	elapsed       atomic.Int64
	logger        *logx.Logger
}

// NewManager 为给定定义完成编译并创建管理器。
func NewManager[D Task, S any](ctx context.Context, store TaskStorage[D, S], storage S, def *Definition[D, S], opts ...Option) (*Manager[D, S], error) {
	if def == nil {
		return nil, &DefinitionError{Msg: "definition is nil"}
	}
	compiled, err := compileDefinition(def)
	if err != nil {
		return nil, err
	}
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(&cfg)
	}
	manager := &Manager[D, S]{
		ctx:      ctx,
		store:    store,
		storage:  storage,
		compiled: compiled,
		options:  cfg,
		active:   make(map[string]activeTask[D, S]),
		logger:   cfg.logger,
	}
	if loader, ok := resolveTaskLoader[D, S](cfg); ok {
		if loader.LoadTask != nil {
			manager.loadTask = func(ctx context.Context, taskID string) (D, error) {
				return loader.LoadTask(ctx, storage, taskID)
			}
		}
		manager.subTaskLoader = loader.LoadSubTask
	}
	return manager, nil
}

func resolveTaskLoader[D Task, S any](cfg options) (TaskLoader[D, S], bool) {
	if cfg.taskLoader == nil {
		var zero TaskLoader[D, S]
		return zero, false
	}
	loader, ok := cfg.taskLoader.(TaskLoader[D, S])
	if !ok {
		var zero TaskLoader[D, S]
		return zero, false
	}
	return loader, true
}

// DefinitionView 返回编译后状态图的只读副本。
func (m *Manager[D, S]) DefinitionView() DefinitionView {
	if m == nil || m.compiled == nil {
		return DefinitionView{}
	}
	return cloneDefinitionView(m.compiled.view)
}

// IsSubTaskMode 判断当前定义里是否真的存在子任务流程。
func (m *Manager[D, S]) IsSubTaskMode() bool {
	return m != nil && m.compiled != nil && len(m.compiled.view.SubPaths) > 0
}

// Metrics 汇总当前管理器的内存执行指标。
func (m *Manager[D, S]) Metrics() Metrics {
	currentTasks := 0
	if m != nil {
		m.activeMu.Lock()
		currentTasks = len(m.active)
		m.activeMu.Unlock()
	}
	return Metrics{
		ProcessCount: m.process.Load(),
		ElapsedNanos: m.elapsed.Load(),
		ActiveTasks:  currentTasks,
	}
}

// Execute 从初始状态开始执行，不依赖已保存的执行记录。
func (m *Manager[D, S]) Execute(ctx context.Context, task D) (*ExecutionResult[D], error) {
	if m == nil || m.compiled == nil {
		return nil, &DefinitionError{Msg: "manager is not initialized"}
	}
	startTime := time.Now()
	m.process.Add(1)
	defer func() {
		m.elapsed.Add(time.Since(startTime).Nanoseconds())
	}()

	taskID := task.ID()

	// 获取任务级进程内锁，防止同一条任务并发执行
	muAny, _ := m.taskMu.LoadOrStore(taskID, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer func() {
		mu.Unlock()
		m.taskMu.Delete(taskID)
	}()

	// 获取分布式锁（多实例部署场景）
	if m.options.enableLock && m.options.locker != nil {
		lockCtx, lockCancel := context.WithTimeout(ctx, m.options.lockTimeout)
		defer lockCancel()
		taskLock, err := m.options.locker.Lock(lockCtx, taskID)
		if err != nil {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		defer func() {
			_ = taskLock.Unlock(context.Background())
		}()
	}

	// 检查任务是否已在执行中
	m.activeMu.Lock()
	if _, exists := m.active[taskID]; exists {
		m.activeMu.Unlock()
		return nil, ErrTaskAlreadyRunning
	}
	m.activeMu.Unlock()

	// 保存初始状态
	if m.store != nil {
		if err := m.store.SaveTaskState(ctx, taskID, CreatedState); err != nil {
			return nil, fmt.Errorf("save initial state: %w", err)
		}
	}

	// 创建任务上下文
	taskCtx := newTaskContext(ctx, task, m.store, m.storage, CreatedState, m.logger)
	execCtx, cancel := context.WithCancel(ctx)
	taskCtx.bindLifecycle(execCtx, cancel)

	// 注册到活跃任务
	m.activeMu.Lock()
	m.active[taskID] = activeTask[D, S]{taskCtx: taskCtx, cancel: cancel}
	m.activeMu.Unlock()

	// 确保任务结束后清理
	defer func() {
		cancel()
		_ = taskCtx.taskEnd()
		m.activeMu.Lock()
		delete(m.active, taskID)
		m.activeMu.Unlock()
	}()

	// 执行状态机
	finalState, execErr := m.executeTransitions(ctx, taskCtx)

	if execErr != nil {
		if finalState == "" {
			finalState = FailedState
		}
		if m.store != nil {
			_ = m.store.SaveTaskState(ctx, taskID, finalState)
		}
	}

	result := &ExecutionResult[D]{
		Task:       task,
		FinalState: finalState,
		SubTasks:   taskCtx.SubTaskRefs(),
	}
	return result, execErr
}

// Resume 从快照恢复执行。
func (m *Manager[D, S]) Resume(ctx context.Context, task D) (*ExecutionResult[D], error) {
	if m == nil || m.compiled == nil {
		return nil, &DefinitionError{Msg: "manager is not initialized"}
	}
	startTime := time.Now()
	m.process.Add(1)
	defer func() {
		m.elapsed.Add(time.Since(startTime).Nanoseconds())
	}()

	taskID := task.ID()

	// 获取任务级进程内锁
	muAny, _ := m.taskMu.LoadOrStore(taskID, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer func() {
		mu.Unlock()
		m.taskMu.Delete(taskID)
	}()

	// 获取分布式锁（多实例部署场景）
	if m.options.enableLock && m.options.locker != nil {
		lockCtx, lockCancel := context.WithTimeout(ctx, m.options.lockTimeout)
		defer lockCancel()
		taskLock, err := m.options.locker.Lock(lockCtx, taskID)
		if err != nil {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		defer func() {
			_ = taskLock.Unlock(context.Background())
		}()
	}

	// 检查是否已在执行
	m.activeMu.Lock()
	if _, exists := m.active[taskID]; exists {
		m.activeMu.Unlock()
		return nil, ErrTaskAlreadyRunning
	}
	m.activeMu.Unlock()

	// 加载快照
	var snapshot ExecutionSnapshot
	if m.store != nil {
		var err error
		snapshot, err = m.store.LoadSnapshot(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("load snapshot: %w", err)
		}
	}

	// 如果快照为空，回退到全量 Execute
	if snapshot.State == "" {
		return m.Execute(ctx, task)
	}

	// 创建上下文并预加载快照
	taskCtx := newTaskContext(ctx, task, m.store, m.storage, snapshot.State, m.logger)
	taskCtx.preloadSnapshot(snapshot, m.compiled.mainPath)
	execCtx, cancel := context.WithCancel(ctx)
	taskCtx.bindLifecycle(execCtx, cancel)

	// 注册到活跃任务
	m.activeMu.Lock()
	m.active[taskID] = activeTask[D, S]{taskCtx: taskCtx, cancel: cancel}
	m.activeMu.Unlock()

	defer func() {
		cancel()
		_ = taskCtx.taskEnd()
		m.activeMu.Lock()
		delete(m.active, taskID)
		m.activeMu.Unlock()
	}()

	finalState, err := m.executeTransitions(ctx, taskCtx)

	if err != nil {
		if finalState == "" {
			finalState = FailedState
		}
		if m.store != nil {
			_ = m.store.SaveTaskState(ctx, taskID, finalState)
		}
	}

	result := &ExecutionResult[D]{
		Task:       task,
		FinalState: finalState,
		SubTasks:   taskCtx.SubTaskRefs(),
	}
	return result, err
}

// executeTransitions 执行状态机驱动的主循环。
func (m *Manager[D, S]) executeTransitions(ctx context.Context, taskCtx *TaskContext[D, S]) (State, error) {
	compiled := m.compiled
	taskID := taskCtx.GetTask().ID()
	logger := m.logger

	// 找到当前状态在 mainPath 中的位置
	startIdx := -1
	for i, s := range compiled.mainPath {
		if s == taskCtx.GetState() {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return "", &StateError{message: fmt.Sprintf("unknown state: %s", taskCtx.GetState())}
	}
	if startIdx >= len(compiled.mainPath)-1 {
		return taskCtx.GetState(), nil
	}

	// 逐个执行状态转换
	for i := startIdx; i < len(compiled.mainPath)-1; i++ {
		fromState := compiled.mainPath[i]
		toState := compiled.mainPath[i+1]

		// 检查取消
		if taskCtx.ended.Load() {
			return taskCtx.GetState(), ErrTaskWaiting
		}

		// 查找转换回调
		transitions, ok := compiled.mainTransitions[fromState]
		if !ok {
			continue
		}
		callback, ok := transitions[toState]
		if !ok {
			continue
		}

		// 如果有子任务流程挂在这个主状态上，先执行子任务
		if subFlow, hasSub := compiled.subFlows[fromState]; hasSub {
			finalState, err := m.executeSubFlow(ctx, taskCtx, subFlow)
			if err != nil {
				return finalState, err
			}
			if IsFinalState(finalState) {
				return finalState, nil
			}
			// 子流程执行完成后立即保存快照，确保子任务数据在崩溃时可恢复
			if m.store != nil {
				snapshot := taskCtx.snapshot()
				if err := m.store.SaveSnapshot(ctx, taskID, snapshot); err != nil {
					logger.Error("save snapshot after subflow failed",
						logx.Fields{"task_id": taskID, "err": err})
				}
			}
		}

		// 执行主任务回调（带重试）
		maxAttempts := 1
		if m.options.retryPolicy != nil && m.options.retryPolicy.MaxAttempts > 1 {
			maxAttempts = m.options.retryPolicy.MaxAttempts
		}

		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				interval := calculateBackoff(m.options.retryPolicy, attempt, 1*time.Second)
				select {
				case <-ctx.Done():
					return taskCtx.GetState(), ctx.Err()
				case <-time.After(interval):
				}
			}

			taskCtx.setTransition(fromState, toState)
			lastErr = callback(taskCtx)
			if lastErr == nil {
				break
			}
		}

		if lastErr != nil {
			// 执行失败，尝试失败回调
			if failCb, ok := compiled.mainFail[fromState]; ok {
				_ = failCb(taskCtx)
			}
			if cancelCb, ok := compiled.mainCancel[fromState]; ok {
				_ = cancelCb(taskCtx)
			}
			return FailedState, lastErr
		}

		// 提交状态
		taskCtx.setState(toState)
		if m.store != nil {
			if err := m.store.SaveTaskState(ctx, taskID, toState); err != nil {
				logger.Error("save task state failed",
					logx.Fields{"task_id": taskID, "state": string(toState), "err": err})
			}
		}

		// 保存快照（子任务数据已包含在快照的 SubFlows 中）
		if m.store != nil {
			snapshot := taskCtx.snapshot()
			if err := m.store.SaveSnapshot(ctx, taskID, snapshot); err != nil {
				logger.Error("save snapshot failed",
					logx.Fields{"task_id": taskID, "err": err})
			}
		}
	}

	return taskCtx.GetState(), nil
}

// executeSubFlow 执行挂载在当前主状态上的子任务流程。
func (m *Manager[D, S]) executeSubFlow(ctx context.Context, taskCtx *TaskContext[D, S], subFlow *compiledSubFlow[D, S]) (State, error) {
	if subFlow == nil {
		return taskCtx.GetState(), nil
	}

	taskID := taskCtx.GetTask().ID()

	// 1. 激活子任务阶段（保证 persistSubTasks / snapshot 正常工作）
	taskCtx.setActiveSubFlow(subFlow.entry)

	// 2. 执行生成器回调（创建子任务）
	if subFlow.generator != nil {
		if err := subFlow.generator(taskCtx); err != nil {
			return FailedState, err
		}
	}

	// 检查是否已有子任务记录（恢复场景）
	existingSubTasks := taskCtx.SubTaskRefs()
	if len(existingSubTasks) > 0 {
		taskCtx.loadSubTasks(existingSubTasks)
	}

	totalSubTasks := taskCtx.activeSubTaskCount()
	if totalSubTasks == 0 {
		return taskCtx.GetState(), nil
	}

	// 2. 逐个执行子任务
	subFailStrategy := m.options.subTaskFailStrategy
	for idx := 0; idx < totalSubTasks; idx++ {
		if taskCtx.ended.Load() {
			return taskCtx.GetState(), ErrTaskWaiting
		}

		taskCtx.setCurrentSubTask(idx)
		subTaskCtx, _ := taskCtx.subTaskRuntimeAt(idx)
		subRecord := subTaskCtx.record

		// 如果子任务已完成，跳过
		if IsFinalState(subRecord.State) {
			continue
		}

		// 查找子任务转换回调（所有子任务共用同一段子路径的第一个业务状态回调）
		var callback TransitionCallback[D, S]
		if trans, ok := subFlow.transitions[CreatedState]; ok {
			for _, state := range subFlow.path {
				if state == CreatedState {
					continue
				}
				if cb, ok := trans[state]; ok {
					callback = cb
					break
				}
			}
		}

		if callback == nil {
			// 没有回调，直接标记成功
			taskCtx.setSubTaskState(idx, SuccessState, nil)
			if m.store != nil {
				_ = m.store.SaveSubTaskState(ctx, taskID, subFlow.entry, subRecord.SubTaskID, SuccessState)
			}
			continue
		}

		// 记录子任务转换（从 CreatedState 到子路径第一个业务状态）
		targetState := subFlow.path[0]
		if targetState == CreatedState && len(subFlow.path) > 1 {
			targetState = subFlow.path[1]
		}
		taskCtx.setTransition(CreatedState, targetState)

		// 重试逻辑（感知上下文取消）
		maxAttempts := 1
		if m.options.retryPolicy != nil && m.options.retryPolicy.MaxAttempts > 1 {
			maxAttempts = m.options.retryPolicy.MaxAttempts
		}

		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				taskCtx.incrementSubTaskAttempts(idx)
				interval := calculateBackoff(m.options.retryPolicy, attempt, 1*time.Second)
				select {
				case <-ctx.Done():
					return taskCtx.GetState(), ctx.Err()
				case <-time.After(interval):
				}
			}

			lastErr = callback(taskCtx)
			if lastErr == nil {
				break
			}
		}

		if lastErr != nil {
			taskCtx.markSubTaskFailedIfPending(idx, lastErr)
			if m.store != nil {
				_ = m.store.SaveSubTaskState(ctx, taskID, subFlow.entry, subRecord.SubTaskID, FailedState)
			}
			// 失败回调按子流程入口状态查找
			if failCb, ok := subFlow.fail[subFlow.entry]; ok && failCb != nil {
				_ = failCb(taskCtx)
			}
			if subFailStrategy == SubTaskFailStop {
				return FailedState, lastErr
			}
			continue
		}

		// 子任务成功
		taskCtx.setSubTaskState(idx, SuccessState, nil)
		if m.store != nil {
			_ = m.store.SaveSubTaskState(ctx, taskID, subFlow.entry, subRecord.SubTaskID, SuccessState)
		}
	}

	// 3. 检查子任务整体结果
	subTasks := taskCtx.SubTaskRefs()
	failedCount := 0
	for _, sub := range subTasks {
		if sub.State == FailedState {
			failedCount++
		}
	}
	if failedCount > 0 {
		if subFailStrategy == SubTaskFailStop {
			return FailedState, ErrSubTasksFailed
		}
		// SubTaskFailContinue：不阻断主流程，由后续主路径转换决定终态
		return taskCtx.GetState(), nil
	}

	return taskCtx.GetState(), nil
}

func cloneDefinitionView(view DefinitionView) DefinitionView {
	out := DefinitionView{
		MainPath: append([]State(nil), view.MainPath...),
		SubPaths: make(map[State][]State, len(view.SubPaths)),
	}
	for k, v := range view.SubPaths {
		out.SubPaths[k] = append([]State(nil), v...)
	}
	return out
}

// calculateBackoff 根据重试策略和当前重试次数计算退避间隔。
// attempt 从 1 开始（1 表示第一次重试）。
func calculateBackoff(policy *RetryPolicy, attempt int, defaultInterval time.Duration) time.Duration {
	if policy == nil || attempt <= 0 {
		return defaultInterval
	}
	if policy.InitialInterval <= 0 {
		return defaultInterval
	}
	if policy.BackoffRate <= 0 {
		return policy.InitialInterval
	}
	interval := float64(policy.InitialInterval) * math.Pow(policy.BackoffRate, float64(attempt-1))
	if policy.MaxInterval > 0 && interval > float64(policy.MaxInterval) {
		interval = float64(policy.MaxInterval)
	}
	if interval <= 0 {
		return defaultInterval
	}
	return time.Duration(interval)
}
