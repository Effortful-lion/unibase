# state

单进程状态机引擎，适合订单、工单、票务等流水线场景。

## 核心能力

- 声明式 DSL 定义状态流转路径，主任务 + 子任务两级流程
- 快照恢复（断点续跑），进程重启后从上次位置继续
- 内置内存存储，零配置即可使用；Redis 存储支持二级索引、查询 API、缓存层、批量操作
- 可选分布式锁（lockdog），多进程部署时防止同一条任务被并发执行
- 子任务失败策略（立即停止 / 跳过继续），全局指数退避重试策略
- 缓存层（LRU + singleflight 防击穿），热任务读取零 Redis 开销
- TTL 自动过期 + 后台索引清理，防止 Redis 无限膨胀

## 执行模型（使用前必读）

state 是**单进程执行 + 锁防重**模型，**不是**分布式工作流调度器：

- **同步顺序执行**：任务由调用 `Execute` / `Resume` 的当前 goroutine 同步、顺序执行到底，库内部不启动后台调度器、不跨进程派发任务。
- **进程内防并发**：同一个 taskID 并发调用时，第二个调用阻塞到第一个完成后串行执行（每个 taskID 一把 `*sync.Mutex`）。
- **多实例防重不调度**：多进程部署时通过 `WithLocker`（lockdog）防止同一任务被多个进程并发执行，但任务仍会"随机落到某个持有锁的进程"上完整跑完，不做负载均衡调度。
- **子任务同步串行**：子任务在同一调用内同步串行执行（非 worker 池并发），数量很大时会阻塞当前 goroutine。
- **只进不退**：状态只能向前推进，失败仅记录终态，**不支持补偿 / 回滚（Saga）**。
- **恢复需手动唤醒**：快照恢复依赖调用方在合适时机重新调用 `Resume`（例如等待外部回调后），库不内置"暂停后自动唤醒"的调度能力。

## 适用场景

- 单服务内的订单 / 工单 / 审批流水线，步骤明确、可声明
- 需要断点续跑 + 子任务扇出，且子任务规模不大、可同步执行
- 多实例部署但能接受"锁防重 + 单实例执行"模型
- 内部工具 / 个人项目 / 中小业务后台

## 不适用场景（请改用专业工作流引擎）

- 需要跨服务补偿 / Saga / 事务回滚（资金、库存等强一致场景）
- 海量子任务需要并发 worker 池消费
- 长周期、含人工介入（human-in-the-loop）的任务编排，需要自动唤醒
- 对状态存储有强一致 / 不可丢失的审计要求（需自行接入数据库存储）
- 要求完整可观测告警闭环（失败自动进死信队列、超时告警等）

## 快速开始

### 订单流水线（主任务 + 子任务 + 重试 + 锁 + Redis 存储）

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/Effortful-lion/unibase/component/state"
)

// ----- 1. 定义任务实体 -----

type OrderTask struct {
    taskID  string
    items   []Item
    total   int64
}

func (t *OrderTask) ID() string { return t.taskID }

type Item struct {
    SKU   string
    Count int
}

// ----- 2. 定义子任务实体 -----

type PackItem struct {
    subTaskID string
    parentID  string
    SKU       string
}

func (t *PackItem) GetSubTaskId() string    { return t.subTaskID }
func (t *PackItem) GetParentTaskId() string { return t.parentID }

// ----- 3. 定义状态机 -----

var PaidState   = state.State("paid")
var PackedState = state.State("packed")
var DoneState   = state.State("done")

def, err := state.Define(func(main state.MainPathBuilder[*OrderTask, any]) {
    main.
        AddMain(PaidState, onPay).
        AddMain(PackedState, func(ctx *state.TaskContext[*OrderTask, any]) error {
            task := ctx.GetTask()
            for i, item := range task.items {
                if err := ctx.CreateSubTask(i, func(context.Context, any) (state.SubTask, error) {
                    return &PackItem{
                        subTaskID: fmt.Sprintf("%s-pack-%d", task.ID(), i),
                        parentID:  task.ID(),
                        SKU:      item.SKU,
                    }, nil
                }); err != nil {
                    return err
                }
            }
            return nil
        }).
        AddSub(state.State("item_packed"), onPackItem).
        AddMain(DoneState, nil)
})

// ----- 4. 创建管理器并执行 -----

func runOrder(ctx context.Context, order *OrderTask) (*state.ExecutionResult[*OrderTask], error) {
    // Redis 存储（带二级索引 + 默认 7 天 TTL）
    storage := state.NewRedisStorage[*OrderTask, *PackItem](redisClient,
        state.WithRedisPrefix("myapp"),
        state.WithRedisTTL(7*24*time.Hour),
        state.WithRedisIndexing(),  // 启用二级索引，支持按状态/时间查询
    )

    // 可选：加本地缓存层（LRU + singleflight）
    cached := state.NewCachedTaskStorage[*OrderTask, *PackItem](storage,
        state.WithCacheMaxEntries(10000),
        state.WithCacheSnapshotTTL(30*time.Second),
        state.WithCacheMetrics(),   // 启用命中率统计
    )

    mgr, err := state.NewManager(ctx, cached, nil, def,
        state.WithDescription("订单流水线"),
        state.WithSubTaskFailStrategy(state.SubTaskFailContinue),
        state.WithRetryPolicy(state.RetryPolicy{
            MaxAttempts:     3,
            InitialInterval: 500 * time.Millisecond,
            BackoffRate:     2.0,     // 指数退避
            MaxInterval:     5 * time.Second,
        }),
        state.WithLocker(state.NewLocker(func() lockdog.Locker {
            return lockdog.New(redisClient)
        })),
        state.WithTaskLoader(state.TaskLoader[*OrderTask, any]{
            LoadSubTask: func(_ context.Context, task *OrderTask, record state.SubTaskRecord, _ any) (state.SubTask, error) {
                return &PackItem{
                    subTaskID: record.SubTaskID,
                    parentID:  record.ParentTaskID,
                    SKU:      fmt.Sprintf("sku-%s", record.SubTaskID),
                }, nil
            },
        }),
    )
    if err != nil {
        return nil, err
    }
    return mgr.Execute(ctx, order)
}
```

## 完整 API 参考

### 一、常量与状态值

| 名称 | 类型 | 值 | 说明 |
|---|---|---|---|
| `PendingState` | `State` | `"pending"` | 已持久化，待调度 |
| `CreatedState` | `State` | `"created"` | 已被状态机获取，待执行 |
| `SuccessState` | `State` | `"success"` | 执行成功 |
| `FailedState` | `State` | `"failed"` | 执行失败 |
| `CanceledState` | `State` | `"canceled"` | 任务已取消 |
| `SubTaskFailStop` | `SubTaskFailStrategy` | `0` | 子任务失败立即停止 |
| `SubTaskFailContinue` | `SubTaskFailStrategy` | `1` | 子任务失败后跳过继续 |

### 二、核心类型

| 类型 | 定义位置 | 说明 |
|---|---|---|
| `State` | `types.go` | `string` 类型，表示状态值 |
| `Task` | `types.go` | 任务实体接口，只需实现 `ID() string` |
| `SubTask` | `types.go` | 子任务接口：`GetSubTaskId() string` + `GetParentTaskId() string` |
| `SubTaskRecord` | `types.go` | 框架内部子任务记录：FlowKey / EntryState / State / Index / Attempts / ProcessCount / LastProcessTimeStamp / StartTimeStamp / FinishTimeStamp / ErrorMsg |
| `ExecutionResult[D]` | `types.go` | 执行结果：Task / FinalState / SubTasks |
| `ExecutionSnapshot` | `types.go` | 断点恢复快照：State + SubFlows map |
| `DefinitionView` | `types.go` | 只读拓扑：MainPath []State + SubPaths map[State][]State |
| `Metrics` | `metrics.go` | 执行指标：ProcessCount / ElapsedNanos / ActiveTasks |
| `CacheMetrics` | `cached_storage.go` | 缓存命中统计：Hits / Total / Ratio |
| `TaskSummary` | `storage_redis.go` | 任务摘要：TaskID / State / CreatedAt |

### 三、定义层（definition.go + compile.go）

#### 函数

| 签名 | 说明 |
|---|---|
| `NewDefinition[D, S]() *Definition[D, S]` | 创建空定义，从隐式 `created` 状态开始 |
| `Define[D, S](build func(PathBuilder[D, S])) (*Definition[D, S], error)` | 一次性构建并校验定义 |
| `IsFinalState(s State) bool` | 判断状态是否为终态（success / failed / canceled） |

#### `Definition[D, S]` 方法

| 方法 | 签名 | 说明 |
|---|---|---|
| `Main` | `() PathBuilder[D, S]` | 返回主任务流程末尾的构建器 |
| `Use` | `Use(mws ...TransitionCallbackMiddleware[D, S])` | 注册全局中间件 |
| `Validate` | `() error` | 校验定义结构合法性 |
| `View` | `() DefinitionView` | 返回只读拓扑结果 |

#### `PathBuilder[D, S]` 接口方法

| 方法 | 说明 |
|---|---|
| `AddMain(to State, cb, mws...) PathBuilder` | 追加主任务状态转换 |
| `AddSub(to State, cb, mws...) PathBuilder` | 在当前主状态下挂子任务流程 |
| `AddCancel(cb, mws...) PathBuilder` | 登记取消收尾回调 |
| `AddFail(cb, mws...) PathBuilder` | 登记失败收尾回调 |

> **DSL 约定**：
> - `AddMain` 必须显式以 `SuccessState` 结束
> - `AddSub` 首次调用时自动"偷走"前一个 `AddMain` 的回调作为子任务生成器
> - 后续再次调用 `AddMain` 时关闭子任务流程，回到主任务路径

#### 中间件类型

| 类型 | 签名 | 说明 |
|---|---|---|
| `TransitionCallback` | `func(*TaskContext[D, S]) error` | 状态转换回调 |
| `TransitionCallbackMiddleware` | `func(TransitionCallback) TransitionCallback` | 包装回调的中间件 |

### 四、执行层（manager.go）

#### `Manager[D, S]` 方法

| 方法 | 签名 | 说明 |
|---|---|---|
| `Execute` | `(ctx context.Context, task D) (*ExecutionResult[D], error)` | 从初始状态开始执行 |
| `Resume` | `(ctx context.Context, task D) (*ExecutionResult[D], error)` | 从快照恢复执行 |
| `DefinitionView` | `() DefinitionView` | 返回编译后状态图的只读副本 |
| `IsSubTaskMode` | `() bool` | 判断定义中是否存在子任务流程 |
| `Metrics` | `() Metrics` | 返回当前执行指标 |

#### 并发安全

- 同一条 taskID 并发调用 `Execute` / `Resume` 时，第二个调用会阻塞到第一个完成后串行执行
- 进程内通过 `sync.Map`（每个 taskID 一把 `*sync.Mutex`）保证

### 五、存储层（storage.go）

#### `TaskStorage[D, S]` 接口

| 方法 | 签名 | 说明 |
|---|---|---|
| `SaveTaskState` | `(ctx, taskID string, state State) error` | 保存主任务状态 |
| `SaveSubTasks` | `(ctx, taskID string, entry State, subTasks []SubTaskRecord) error` | 保存子任务列表 |
| `SaveSubTaskState` | `(ctx, taskID string, entry State, subTaskID string, state State) error` | 保存单个子任务状态 |
| `SaveSnapshot` | `(ctx, taskID string, snapshot ExecutionSnapshot) error` | 保存执行快照 |
| `LoadSnapshot` | `(ctx, taskID string) (ExecutionSnapshot, error)` | 加载执行快照 |
| `LoadSnapshots` | `(ctx, taskIDs []string) (map[string]ExecutionSnapshot, error)` | 批量加载快照 |
| `LoadTaskStates` | `(ctx, taskIDs []string) (map[string]State, error)` | 批量查询任务状态 |
| `DeleteTask` | `(ctx, taskID string) error` | 删除任务及其所有关联数据 |

#### `MemoryStorage[D, S]` 结构体

| 方法 | 说明 |
|---|---|
| `NewMemoryStorage[D, S]() *MemoryStorage[D, S]` | 创建内存存储 |
| `SaveTaskState(ctx, taskID, state) error` | 实现 TaskStorage |
| `SaveSubTasks(ctx, taskID, entry, subTasks) error` | 实现 TaskStorage |
| `SaveSubTaskState(ctx, taskID, entry, subTaskID, state) error` | 实现 TaskStorage |
| `SaveSnapshot(ctx, taskID, snapshot) error` | 手动保存快照 |
| `LoadSnapshot(ctx, taskID) (ExecutionSnapshot, error)` | 加载快照 |
| `LoadSnapshots(ctx, taskIDs) (map[string]ExecutionSnapshot, error)` | 批量加载快照 |
| `LoadTaskStates(ctx, taskIDs) (map[string]State, error)` | 批量查询状态 |
| `DeleteTask(ctx, taskID) error` | 删除任务 |
| `TaskStates(taskID) []State` | 查询任务状态轨迹（测试/展示用） |
| `SubTaskLists(taskID) map[State][]SubTaskRecord` | 查询子任务列表 |
| `SubTaskStates(subTaskID) []State` | 查询子任务状态轨迹 |

#### `RedisStorage[D, S]` 结构体

| 选项 | 说明 |
|---|---|
| `WithRedisPrefix(prefix)` | 设置 key 前缀，默认 `"state"` |
| `WithRedisTTL(ttl)` | 设置 TTL，传 0 表示永不过期，默认 7 天 |
| `WithRedisIndexing()` | 启用二级索引（Sorted Set），支持按状态/时间查询 |

| 方法 | 签名 | 说明 |
|---|---|---|
| `NewRedisStorage` | `(client, opts...) *RedisStorage[D, S]` | client 可以是 `*redis.Client` 或 `*redis.ClusterClient` |
| `GetTaskState` | `(ctx, taskID) (State, error)` | 查询任务当前状态 |
| `ListTasksByState` | `(ctx, state, cursor, limit) ([]string, string, error)` | 按状态分页查询任务 ID |
| `ListTasksByTime` | `(ctx, start, end, cursor, limit) ([]string, string, error)` | 按创建时间范围查询任务 ID |
| `DeleteTask` | `(ctx, taskID) error` | 删除任务及所有关联数据 |
| `SaveTaskState` | — | 写状态轨迹 + 更新二级索引 |
| `SaveSubTasks` | — | 保存子任务列表 |
| `SaveSubTaskState` | — | 保存子任务状态 |
| `SaveSnapshot` | — | 保存快照 |
| `LoadSnapshot` | — | 加载快照 |
| `LoadSnapshots` | — | 批量加载快照（Pipeline） |
| `LoadTaskStates` | — | 批量查询状态（Pipeline） |

> **索引说明**：启用 `WithRedisIndexing()` 后，`SaveTaskState` 会自动维护两个 Sorted Set：
> - `{prefix}:idx:state:{state}` — 按状态索引，score 为创建时间
> - `{prefix}:idx:created` — 按创建时间索引

#### `CachedTaskStorage[D, S]` 结构体（缓存层）

| 选项 | 说明 |
|---|---|
| `WithCacheMaxEntries(n)` | 本地缓存最大条目数，默认 1000 |
| `WithCacheSnapshotTTL(ttl)` | 快照在本地缓存的过期时间，默认 30 秒 |
| `WithCacheMetrics()` | 启用缓存命中率统计 |

| 方法 | 说明 |
|---|---|
| `NewCachedTaskStorage(store, opts...)` | 包装任意 `TaskStorage`，增加本地 LRU 缓存 |
| `LoadSnapshot` | 先查本地缓存，miss 时 singleflight 合并请求后回写 |
| `SaveSnapshot` | 写入存储 + 同步更新本地缓存 |
| `CacheMetrics` | 返回命中率统计 |

#### `TaskCleaner[D, S]` 结构体（TTL 清理）

| 选项 | 说明 |
|---|---|
| `WithCleanupInterval(interval)` | 自动清理间隔，默认 1 小时 |
| `WithCleanupLogger(logger)` | 自定义日志器 |

| 方法 | 说明 |
|---|---|
| `NewTaskCleaner(store, prefix, indexEnabled, opts...)` | 创建清理器 |
| `Start(ctx)` | 启动后台清理 goroutine |
| `Stop()` | 停止清理 |
| `CleanupExpired(ctx)` | 手动触发一次过期索引清理 |
| `CleanupTasksBefore(ctx, before)` | 清理指定时间点之前的已完成任务 |

### 六、TaskContext 运行时（context.go）

#### `TaskContext[D, S]` 方法

| 方法 | 返回值 | 说明 |
|---|---|---|
| `Context()` | `context.Context` | 任务执行上下文 |
| `Get(key string)` | `any` | 读取临时存储 |
| `Set(key string, value any)` | — | 写入临时存储 |
| `GetTask()` | `D` | 当前任务对象 |
| `Storage()` | `S` | 业务存储句柄 |
| `GetState()` | `State` | 当前已提交状态 |
| `FromState()` | `State` | 正在执行的源状态 |
| `ToState()` | `State` | 正在执行的目标状态 |
| `ActiveSubFlow()` | `State` | 当前活跃子任务流程入口 |
| `CurrentSubTask()` | `SubTask` | 当前正在执行的子任务对象 |
| `CurrentSubTaskIndex()` | `int` | 当前子任务索引 |
| `CurrentSubTaskRef()` | `SubTaskRecord` | 当前子任务运行记录 |
| `SubTasks()` | `[]SubTask` | 全部子任务对象 |
| `SubTaskRefs()` | `[]SubTaskRecord` | 全部子任务运行记录 |
| `LoadSubTask(record)` | `(SubTask, error)` | 按记录加载子任务对象 |
| `CreateSubTask(index, build)` | `error` | 创建子任务并插入当前活跃流程 |
| `CreateSubTaskRecord(record, index)` | `error` | 插入子任务记录 |

### 七、选项与配置（options.go）

#### `Option` 函数列表

| 选项 | 参数 | 说明 |
|---|---|---|
| `WithDescription` | `string` | 管理器描述信息 |
| `WithTaskLoader` | `any`（传入 `TaskLoader[D, S]`） | 断点恢复时的任务加载器 |
| `WithSubTaskFailStrategy` | `SubTaskFailStrategy` | 子任务失败策略，默认 `SubTaskFailStop` |
| `WithRetryPolicy` | `RetryPolicy` | 全局重试策略（指数退避） |
| `WithLocker` | `Locker` | 任务锁，多进程部署时使用 |
| `WithLogger` | `*logx.Logger` | 自定义日志器 |

#### `RetryPolicy` 结构体

| 字段 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `MaxAttempts` | `int` | `1` | 最大重试次数 |
| `InitialInterval` | `time.Duration` | `1s` | 首次重试间隔 |
| `MaxInterval` | `time.Duration` | `1s` | 最大重试间隔 |
| `BackoffRate` | `float64` | `0` | 退避倍数（`interval = InitialInterval * BackoffRate^(attempt-1)`） |

#### `Locker` 接口

| 方法 | 签名 | 说明 |
|---|---|---|
| `Lock` | `(ctx context.Context, taskID string) (taskLock, error)` | 获取任务锁 |

#### `NewLocker` 函数

```go
func NewLocker(lockerFactory func() lockdog.Locker, opts ...lockdog.Option) Locker
```

- `lockerFactory`：返回 lockdog.Locker 实例的工厂函数
- `opts`：透传给 lockdog 的选项（如 `lockdog.WithTTL`、`lockdog.WithOwner`）
- 单进程部署不需要，省略 `WithLocker` 即可

### 八、错误（errors.go）

#### 哨兵错误

| 错误变量 | 类型 | 说明 |
|---|---|---|
| `ErrInvalidTransition` | `*StateError` | 无效状态转换 |
| `ErrTaskWaiting` | `*StateError` | 任务等待中 |
| `ErrSubTasksFailed` | `*StateError` | 子任务执行失败 |
| `ErrTaskNotFound` | `*StateError` | 任务未找到 |
| `ErrTaskAlreadyRunning` | `*StateError` | 任务已在执行中 |
| `ErrSubTaskLoaderNotRegistered` | `*StateError` | 子任务加载器未注册 |
| `ErrSubTaskNotLoaded` | `*StateError` | 子任务未加载 |

#### 便捷判断函数

| 函数 | 说明 |
|---|---|
| `IsInvalidTransition(err) bool` | 判断是否为无效状态转换错误 |
| `IsTaskWaiting(err) bool` | 判断是否为任务等待错误 |
| `IsSubTasksFailed(err) bool` | 判断是否为子任务失败错误 |
| `IsTaskNotFound(err) bool` | 判断是否为任务未找到错误 |
| `IsTaskAlreadyRunning(err) bool` | 判断是否为任务已在执行中错误 |
| `IsSubTaskNotLoaded(err) bool` | 判断是否为子任务未加载错误 |

#### 错误类型

| 类型 | 说明 |
|---|---|
| `DefinitionError` | 定义校验失败：Kind / Entry / From / To / Msg |
| `DefinitionErrors` | 多条定义错误聚合 |
| `StateError` | 运行期错误，实现 `error` 接口 |

### 九、辅助类型（types.go）

| 类型 | 说明 |
|---|---|
| `TaskLoaderFunc[D, S]` | `func(ctx, storage, taskID) (D, error)` — 按 taskID 加载主任务 |
| `SubTaskLoaderFunc[D, S]` | `func(ctx, task, record, storage) (SubTask, error)` — 加载子任务对象 |
| `TaskLoader[D, S]` | 结构体：`LoadTask` + `LoadSubTask`，统一收口恢复逻辑 |

### 十、Redis 存储专用（storage_redis.go）

#### `RedisStorageOption` 函数

| 选项 | 说明 |
|---|---|
| `WithRedisPrefix(prefix)` | key 前缀，默认 `"state"` |
| `WithRedisTTL(ttl)` | 默认 TTL，传 0 表示永不过期，默认 7 天 |
| `WithRedisIndexing()` | 启用二级索引 |

#### `CachedStorageOption` 函数

| 选项 | 说明 |
|---|---|
| `WithCacheMaxEntries(n)` | 本地 LRU 最大条目数，默认 1000 |
| `WithCacheSnapshotTTL(ttl)` | 快照本地缓存过期时间，默认 30 秒 |
| `WithCacheMetrics()` | 启用命中率统计 |

#### `CleanerOption` 函数

| 选项 | 说明 |
|---|---|
| `WithCleanupInterval(interval)` | 自动清理间隔，默认 1 小时 |
| `WithCleanupLogger(logger)` | 自定义日志器 |

## 设计决策

| 决策 | 原因 |
|---|---|
| `Task` 接口仅需 `ID()` | 单进程不内置调度器，`GetStartAtTs` 无意义 |
| 重试由 `RetryPolicy` 控制 | 布尔开关 `GetSupportRetry` 粒度太粗，策略模式更灵活 |
| 锁可选且默认关闭 | 单进程部署零开销；多进程共享 Redis 时通过 `WithLocker` 启用 |
| 内置 `MemoryStorage` | 零配置即用；需要持久化时实现 `TaskStorage` 接口对接任意存储 |
| JSON/BSON 标签保留旧名 | `SubTaskRecord` 序列化兼容存量数据 |
| 二级索引默认关闭 | 无索引场景零额外开销；需要查询能力时通过 `WithRedisIndexing()` 启用 |
| 默认 TTL 7 天 | 防止 Redis key 无限膨胀；可通过 `WithRedisTTL(0)` 永不过期 |
| 缓存层可插拔 | `CachedTaskStorage` 包装任意 `TaskStorage`，不侵入核心逻辑 |

## 示例

| 示例 | 路径 | 说明 |
|---|---|---|
| 线性主流程 + AddFail + 重试 | `example/linear` | 订单支付→发货，发货总是失败，触发 AddFail 收尾回调 |
| 主流程 + 子流程 + AddFail + 重试 | `example/subtask` | 订单支付→打包（子任务）→发货，每个商品一个打包子任务 |
| 断点续执行（Resume） | `example/resume` | 转账流程，模拟进程崩溃后从快照恢复继续执行 |

## 依赖

| 依赖 | 用途 | 必选 |
|---|---|---|
| `github.com/Effortful-lion/unibase/logx` | 结构化日志 | 是 |
| `github.com/Effortful-lion/unibase/component/lockdog` | 分布式锁 | 否 |
| `github.com/redis/go-redis/v9` | Redis 客户端（持久化存储） | 否 |
| `github.com/hashicorp/golang-lru/v2` | 本地 LRU 缓存（缓存层） | 否 |
| `golang.org/x/sync/singleflight` | 缓存击穿防护 | 否 |
| `github.com/alicebob/miniredis/v2` | 测试用 Redis | 仅测试 |
