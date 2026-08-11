package state

import (
	"context"
	"sync"
)

// MemoryStorage 是内存存储实现，适合测试和单进程场景。
type MemoryStorage[D Task, S any] struct {
	taskStates    map[string][]State
	subTaskLists  map[string]map[State][]SubTaskRecord
	subTaskStates map[string][]State
	snapshots     map[string]ExecutionSnapshot
	mu            sync.RWMutex
}

// NewMemoryStorage 创建内存存储。
func NewMemoryStorage[D Task, S any]() *MemoryStorage[D, S] {
	return &MemoryStorage[D, S]{
		taskStates:    make(map[string][]State),
		subTaskLists:  make(map[string]map[State][]SubTaskRecord),
		subTaskStates: make(map[string][]State),
		snapshots:     make(map[string]ExecutionSnapshot),
	}
}

// SaveTaskState 保存主任务状态。
func (m *MemoryStorage[D, S]) SaveTaskState(_ context.Context, taskID string, state State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taskStates[taskID] = append(m.taskStates[taskID], state)
	return nil
}

// SaveSubTasks 保存子任务列表。
func (m *MemoryStorage[D, S]) SaveSubTasks(_ context.Context, taskID string, entry State, subTasks []SubTaskRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subTaskLists[taskID]; !ok {
		m.subTaskLists[taskID] = make(map[State][]SubTaskRecord)
	}
	items := make([]SubTaskRecord, len(subTasks))
	copy(items, subTasks)
	m.subTaskLists[taskID][entry] = items
	return nil
}

// SaveSubTaskState 保存子任务状态。
func (m *MemoryStorage[D, S]) SaveSubTaskState(_ context.Context, _ string, _ State, subTaskID string, state State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subTaskStates[subTaskID] = append(m.subTaskStates[subTaskID], state)
	return nil
}

// SaveSnapshot 保存任务执行快照。
func (m *MemoryStorage[D, S]) SaveSnapshot(_ context.Context, taskID string, snapshot ExecutionSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots[taskID] = snapshot
	return nil
}

// LoadSnapshot 加载任务执行快照。
func (m *MemoryStorage[D, S]) LoadSnapshot(_ context.Context, taskID string) (ExecutionSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot, ok := m.snapshots[taskID]
	if !ok {
		return ExecutionSnapshot{}, nil
	}
	return snapshot, nil
}

// LoadSnapshots 批量加载快照（内存实现）。
func (m *MemoryStorage[D, S]) LoadSnapshots(_ context.Context, taskIDs []string) (map[string]ExecutionSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]ExecutionSnapshot, len(taskIDs))
	for _, taskID := range taskIDs {
		if snapshot, ok := m.snapshots[taskID]; ok {
			result[taskID] = snapshot
		}
	}
	return result, nil
}

// LoadTaskStates 批量查询任务当前状态（内存实现）。
func (m *MemoryStorage[D, S]) LoadTaskStates(_ context.Context, taskIDs []string) (map[string]State, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]State, len(taskIDs))
	for _, taskID := range taskIDs {
		states, ok := m.taskStates[taskID]
		if ok && len(states) > 0 {
			result[taskID] = states[len(states)-1]
		}
	}
	return result, nil
}

// DeleteTask 删除任务及其所有关联数据（内存实现）。
func (m *MemoryStorage[D, S]) DeleteTask(_ context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.taskStates, taskID)
	delete(m.subTaskLists, taskID)
	delete(m.snapshots, taskID)
	// 清理子任务状态轨迹
	for subTaskID := range m.subTaskStates {
		if len(subTaskID) > 0 && subTaskID[:len(taskID)] == taskID {
			delete(m.subTaskStates, subTaskID)
		}
	}
	return nil
}

// TaskStates 返回任务的状态轨迹（用于测试和展示）。
func (m *MemoryStorage[D, S]) TaskStates(taskID string) []State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]State(nil), m.taskStates[taskID]...)
}

// SubTaskLists 返回子任务列表（用于测试和展示）。
func (m *MemoryStorage[D, S]) SubTaskLists(taskID string) map[State][]SubTaskRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[State][]SubTaskRecord, len(m.subTaskLists[taskID]))
	for k, v := range m.subTaskLists[taskID] {
		items := make([]SubTaskRecord, len(v))
		copy(items, v)
		out[k] = items
	}
	return out
}

// SubTaskStates 返回子任务的状态轨迹（用于测试和展示）。
func (m *MemoryStorage[D, S]) SubTaskStates(subTaskID string) []State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]State(nil), m.subTaskStates[subTaskID]...)
}

// Ensure MemoryStorage implements TaskStorage.
var _ TaskStorage[Task, SubTask] = (*MemoryStorage[Task, SubTask])(nil)
