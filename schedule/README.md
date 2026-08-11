# 定位

schedule：基于标准库的定时任务触发能力。提供三种触发类型（At / Every / After）和时间窗口约束（Between / BetweenDaily），零外部依赖。

# 核心 API

## 触发类型

| 构造函数 | 语义 | 典型场景 |
|---|---|---|
| `At(hour, minute, period...)` | 定时锚点，在指定的 wall-clock 时间点触发 | 每天 3:00 清理日志 |
| `Every(interval)` | 固定间隔，每隔固定时长触发一次 | 每 30 秒同步数据 |
| `After(delay)` | 延迟单次，等固定时长后触发一次 | 启动 10 秒后发通知 |

## 时间窗口约束

| 构造函数 | 语义 | 典型场景 |
|---|---|---|
| `Between(start, end)` | 固定日期范围闭区间 | 2025-01-01 到 2025-12-31 期间才执行 |
| `BetweenDaily(startHour, endHour)` | 每日小时范围，左闭右开 | 每天 9:00-17:59 执行，或跨天 22:00-06:00 |

## 生命周期

| 方法 | 作用 |
|---|---|
| `Run(ctx, trigger, jobFunc, constraints...) *Job` | 启动定时任务，返回 handle |
| `Job.Stop()` | 停止调度，不再安排下一轮 |
| `Job.Wait()` | 等待当前轮次完成后返回 |

## 任务函数

```go
type JobFunc func(ctx context.Context, triggerTime time.Time) error
```

- `ctx`：感知取消，控制 goroutine 生命周期
- `triggerTime`：本次实际触发时间点
- 返回值 `error`：仅用于日志记录，不影响后续调度

# 使用示例

## At：定时锚点

```go
// 每天 3:00 执行
j := schedule.Run(ctx, schedule.At(3, 0), func(ctx context.Context, t time.Time) error {
    cleanUp()
    return nil
})

// 从 3:00 开始，每 2 小时执行一次：3:00、5:00、7:00...
j := schedule.Run(ctx, schedule.At(3, 0, 2*time.Hour), doWork)
```

## Every：固定间隔

```go
// 每 30 秒执行一次
j := schedule.Run(ctx, schedule.Every(30*time.Second), func(ctx context.Context, t time.Time) error {
    syncData()
    return nil
})

// 每 5 分钟执行一次
j := schedule.Run(ctx, schedule.Every(5*time.Minute), healthCheck)
```

## After：延迟单次

```go
// 10 秒后执行一次
j := schedule.Run(ctx, schedule.After(10*time.Second), sendNotification)
j.Wait() // 等待执行完成
```

## BetweenDaily：每日小时窗口

```go
// 每天 9:00-17:59 执行，18:00 整不执行
j := schedule.Run(ctx,
    schedule.Every(30*time.Minute),
    doWork,
    schedule.BetweenDaily(9, 18),
)

// 跨天窗口：每天 22:00-23:59 和 00:00-06:00
j := schedule.Run(ctx,
    schedule.Every(30*time.Minute),
    doWork,
    schedule.BetweenDaily(22, 6),
)
```

## Between：固定日期范围

```go
// 2025 年内才执行
j := schedule.Run(ctx,
    schedule.Every(1*time.Hour),
    doWork,
    schedule.Between(
        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
        time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
    ),
)
```

## 组合：停止与等待

```go
j := schedule.Run(ctx, schedule.Every(30*time.Second), doWork)

// ... 运行中 ...

j.Stop() // 不再安排下一轮
j.Wait() // 等当前轮次跑完再继续后续清理
```

# 行为说明

- **drift correction**：At 和 Every 的周期从锚点时间算起，不随任务执行时长漂移
- **panic 保护**：任务函数 panic 会被 recover，不影响后续调度
- **error 不阻断**：任务函数返回 error 仅记录日志，不影响后续调度
- **约束跳过**：约束不满足时跳过本次，按正常周期等待下一触发时机

# 演进方向

- 无分布式能力规划
- 不引入外部依赖（如 cron parser）
- 功能保持单一，按真实需求渐进扩展
