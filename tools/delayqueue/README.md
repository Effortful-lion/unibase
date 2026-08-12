# tools/delayqueue

生产级 Redis 延迟队列，基于 Redis ZSet + Lua 原子脚本 + lockdog 分布式锁实现。

## 设计

```
ZSet {key}            → 延迟队列（score = 投递时间戳，member = JSON 编码消息）
ZSet {key}:processing → Processing 处理中队列（score = 拉取时间戳）
List {key}:dlq        → 死信队列（处理超过重试上限的消息）
Lock {key}:nack:{id}  → Nack 分布式锁（lockdog，防止 Nack/Sweep 竞态）
```

## 核心能力

| 能力 | 实现 |
|------|------|
| 原子 Poll | ZPOPMINBYSCORE 原子弹出，多消费者不重复抢 |
| 原子 Nack | Lua 脚本 + lockdog 分布式锁，Nack/Sweep 并发安全 |
| 可见性超时 | Poll 后消息进入 Processing，超时未 Ack 由 Sweeper 自动重试 |
| NACK + 重试 | 消费失败按退避时间重新入队 |
| 死信队列 (DLQ) | 超过 MaxRetries 的消息移入 DLQ，供人工排查 |
| Sweeper | 后台定时扫描超时 Processing 消息，每条消息加锁处理 |
| 优雅关闭 | Stop 等待 Processing 处理完毕 |
| Consumer 模式 | Start(ctx, consumer) 一行启动，内置退避、重试、DLQ |

## 快速开始

### Consumer 模式（推荐）

```go
import (
    "context"
    "time"

    "github.com/Effortful-lion/unibase/tools/delayqueue"
    "github.com/redis/go-redis/v9"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
q := delayqueue.New(rdb, "order:delay",
    delayqueue.WithMaxRetries(3),
    delayqueue.WithVisibilityTimeout(30*time.Second),
    delayqueue.WithBackoff(func(retries int) time.Duration {
        return time.Duration(retries) * 2 * time.Minute
    }),
)

type orderHandler struct{}
func (h *orderHandler) Consume(ctx context.Context, msg *delayqueue.Message) error {
    fmt.Println("处理订单:", msg.Payload)
    return nil // nil = Ack，非 nil = Nack
}

ctx := context.Background()
q.Start(ctx, &orderHandler{})
```

### 手动模式

```go
// 投递
id, err := q.Add(ctx, "order-123", 30*time.Minute)

// 拉取
msgs, err := q.Poll(ctx, 10)
for _, raw := range msgs {
    msg, _ := delayqueue.ParseMessage(raw)

    if err := process(msg); err != nil {
        q.Nack(ctx, raw) // 失败 → 重试或 DLQ
        continue
    }
    q.Ack(ctx, raw) // 成功 → 确认
}

// 停止
q.Stop()
```

## 重试策略

默认退避：第 N 次重试延迟 N 分钟。可通过 `WithBackoff` 自定义。

## 配置选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `WithMaxRetries(n)` | 3 | 最大重试次数 |
| `WithVisibilityTimeout(d)` | 30s | Processing 超时时间 |
| `WithSweepInterval(d)` | 5s | Sweeper 扫描间隔（自动不超过 VisibilityTimeout/3） |
| `WithBackoff(fn)` | 第 N 次延迟 N 分钟 | 自定义重试退避函数 |

## 生命周期

```
Add → ZSet (score=投递时间)
  ↓ [score <= now, ZPOPMINBYSCORE 原子弹出]
Poll → ZSet:processing (score=拉取时间)
  ↓ [处理成功]
Ack → ZRem from processing ✅ 完成
  ↓ [处理失败]
Nack (Lua原子 + lockdog锁) → ZRem + ZAdd (backoff后重新入队) 🔄 重试
  ↓ [重试 >= MaxRetries]
DLQ (List) 💀 死信
  ↓ [Processing 超时未 Ack]
Sweeper (每条消息 lockdog 锁保护) → 同 Nack 逻辑 ♻️ 自动回收
```

## 依赖

- `github.com/redis/go-redis/v9`
- `github.com/Effortful-lion/unibase/component/lockdog`
- `github.com/Effortful-lion/unibase/tools/id`
