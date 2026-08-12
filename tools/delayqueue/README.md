# tools/delayqueue

基于 Redis ZSet 的延迟队列，支持消息延迟投递和批量拉取。

## 设计

```
消息投递时间(纳秒) → ZSet Score
消息内容           → ZSet Member (id|payload)

Poll 时：
  ZRANGEBYSCORE key 0 <now> → 获取到期消息
  ZREM key member           → 确认后移除
```

## 快速开始

```go
import (
    "context"
    "time"

    "github.com/Effortful-lion/unibase/tools/delayqueue"
    "github.com/redis/go-redis/v9"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
q := delayqueue.New(rdb, "order:delay:queue")

// 投递消息（30 秒后可被取出）
id, err := q.Add(context.Background(), "order-123", 30*time.Second)

// 投递消息（指定绝对时间）
id, err := q.AddAt(context.Background(), "order-456", time.Now().Add(5*time.Minute))

// 消费循环
for {
    msgs, err := q.Poll(ctx, 10, time.Second) // 最多 10 条，阻塞 1s
    for _, m := range msgs {
        fmt.Println("处理:", m)
        q.Ack(ctx, m)
    }
}
```

## API

| 方法 | 说明 |
|------|------|
| `Add(ctx, msg, delay)` | 延迟 delay 后投递 msg，返回消息 ID |
| `AddAt(ctx, msg, t)` | 在绝对时间 t 投递 msg |
| `Poll(ctx, batch, timeout)` | 拉取最多 batch 条到期消息，timeout 为阻塞超时 |
| `Ack(ctx, member)` | 确认消息已处理，从队列移除 |
| `Len(ctx)` | 返回队列中未处理的消息数 |
| `Remove(ctx)` | 清空队列（测试用） |

## 消息格式

Poll 返回的 member 格式为 `id|payload`，其中：
- `id`: 投递时生成的消息标识
- `payload`: 原始消息内容

## 注意事项

- Poll 与 Ack 不保证原子性，消费端应做幂等处理
- 多副本部署时，多个消费者可能同时拉取到同一条消息（需配合分布式锁或业务去重）
- Redis ZSet 的 score 使用 UnixNano（纳秒精度），时间范围足够

## 依赖

- `github.com/redis/go-redis/v9`
