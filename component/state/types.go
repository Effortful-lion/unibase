package state

import (
	"context"
	"time"
)

// State 表示主任务或子任务流转过程中使用的状态值。
type State string

// 内置任务状态。
const (
	PendingState  State = "pending"  // 已持久化，待调度
	CreatedState  State = "created"  // 已被状态机获取，待执行
	SuccessState  State = "success"  // 执行成功
	FailedState   State = "failed"   // 执行失败
	CanceledState State = "canceled" // 任务已取消
)

// IsFinalState 判断一个状态是否为主任务流程或子任务流程的终态。
func IsFinalState(s State) bool {
	switch s {
	case SuccessState, FailedState, CanceledState:
		return true
	default:
		return false
	}
}

// Task 是任务实体接口，唯一要求是实现 ID() 返回唯一标识。
type Task interface {
	ID() string
}

// SubTask 定义子任务执行所需的最小约定。
type SubTask interface {
	GetSubTaskId() string
	GetParentTaskId() string
}

// SubTaskRecord 表示框架内部对子任务运行现场保存的统一记录。
// 这份记录既用于恢复执行，也用于页面展示和统计，不依赖具体业务子任务结构。
type SubTaskRecord struct {
	SubTaskID            string `json:"sub_task_id" bson:"sub_task_id"`
	ParentTaskID         string `json:"parent_task_id" bson:"parent_task_id"`
	FlowKey              State  `json:"flow_key,omitempty" bson:"flow_key,omitempty"`
	EntryState           State  `json:"entry_state,omitempty" bson:"entry_state,omitempty"`
	State                State  `json:"state" bson:"state"`
	Index                int    `json:"index" bson:"index"`
	Attempts             int    `json:"attempts,omitempty" bson:"attempts,omitempty"`
	ProcessCount         int64  `json:"process_count" bson:"process_count"`
	LastProcessTimeStamp int64  `json:"last_process_ts,omitempty" bson:"last_process_ts,omitempty"`
	StartTimeStamp       int64  `json:"start_ts" bson:"start_ts"`
	FinishTimeStamp      int64  `json:"finish_ts" bson:"finish_ts"`
	ErrorMsg             string `json:"error_msg,omitempty" bson:"error_msg,omitempty"`
}

// GetSubTaskId 返回子任务 ID。
func (r SubTaskRecord) GetSubTaskId() string { return r.SubTaskID }

// GetParentTaskId 返回父任务 ID。
func (r SubTaskRecord) GetParentTaskId() string { return r.ParentTaskID }

// TaskLoaderFunc 按 taskID 和业务存储句柄加载完整主任务对象。
type TaskLoaderFunc[D Task, S any] func(ctx context.Context, storage S, taskID string) (D, error)

// SubTaskLoaderFunc 按当前主任务和子任务记录加载完整子任务对象。
type SubTaskLoaderFunc[D Task, S any] func(ctx context.Context, task D, record SubTaskRecord, storage S) (SubTask, error)

// TaskLoader 把主任务和子任务的恢复逻辑统一收口到一个配置项。
//
// LoadTask 用于按 taskID 回查完整主任务。
// LoadSubTask 用于按主任务和子任务记录恢复完整子任务。
type TaskLoader[D Task, S any] struct {
	LoadTask    TaskLoaderFunc[D, S]
	LoadSubTask SubTaskLoaderFunc[D, S]
}

// TransitionCallback 表示一次状态转换执行时的回调。
type TransitionCallback[D Task, S any] func(*TaskContext[D, S]) error

// TransitionCallbackMiddleware 用于包装状态转换回调。
type TransitionCallbackMiddleware[D Task, S any] func(TransitionCallback[D, S]) TransitionCallback[D, S]

// PathBuilder 用于按链式写法定义主任务和子任务的流转路径。
//
// 约定如下：
//   - AddMain(...) 追加主任务状态
//   - AddSub(...) 表示当前主状态下面挂一段子任务流程；第一次调用时，会把前一个 AddMain(...)
//     的回调当成子任务生成逻辑
//   - 子任务流程结束条件不是单独的方法，而是后面重新出现 AddMain(...)
//   - 主任务正常路径必须显式以 AddMain(SuccessState, ...) 结束
type PathBuilder[D Task, S any] interface {
	AddMain(to State, cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S]
	AddSub(to State, cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S]
	AddCancel(cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S]
	AddFail(cb TransitionCallback[D, S], mws ...TransitionCallbackMiddleware[D, S]) PathBuilder[D, S]
}

type MainPathBuilder[D Task, S any] = PathBuilder[D, S]
type SubPathBuilder[D Task, S any] = PathBuilder[D, S]

// TaskStorage 负责持久化主任务和子任务的状态变化。
type TaskStorage[D Task, S any] interface {
	SaveTaskState(ctx context.Context, taskID string, state State) error
	SaveSubTasks(ctx context.Context, taskID string, entry State, subTasks []SubTaskRecord) error
	SaveSubTaskState(ctx context.Context, taskID string, entry State, subTaskID string, state State) error
	SaveSnapshot(ctx context.Context, taskID string, snapshot ExecutionSnapshot) error
	LoadSnapshot(ctx context.Context, taskID string) (ExecutionSnapshot, error)
	LoadSnapshots(ctx context.Context, taskIDs []string) (map[string]ExecutionSnapshot, error)
	LoadTaskStates(ctx context.Context, taskIDs []string) (map[string]State, error)
	DeleteTask(ctx context.Context, taskID string) error
}

// ExecutionResult 表示 Execute/Resume 返回的最终执行结果。
type ExecutionResult[D Task] struct {
	Task       D
	FinalState State
	SubTasks   []SubTaskRecord
}

// ExecutionSnapshot 保存一个任务恢复执行所需的现场记录。
type ExecutionSnapshot struct {
	State    State
	SubFlows map[State][]SubTaskRecord
}

// DefinitionView 表示提供给管理器和注册表使用的简化只读拓扑结果。
type DefinitionView struct {
	MainPath []State
	SubPaths map[State][]State
}

// TaskSummaryMeta 保存仅靠已保存记录无法直接推导出的任务摘要补充字段。
type TaskSummaryMeta struct {
	CreatedAt    time.Time
	FinishedAt   time.Time
	ProcessCount int64
	LastError    string
}

// TaskSummaryMetadataLoader 从存储中加载主任务摘要所需的补充元数据。
type TaskSummaryMetadataLoader interface {
	LoadTaskSummaryMeta(ctx context.Context, taskID string) (TaskSummaryMeta, error)
}

// SubTaskSummaryMeta 保存从存储加载的子任务摘要补充字段。
type SubTaskSummaryMeta struct {
	ProcessCount    int64
	ErrorMsg        string
	StartTimeStamp  int64
	FinishTimeStamp int64
}

// SubTaskSummaryMetadataLoader 为一个父任务加载子任务摘要补充元数据。
type SubTaskSummaryMetadataLoader interface {
	LoadSubTaskSummaryMeta(ctx context.Context, taskID string) (map[string]SubTaskSummaryMeta, error)
}
