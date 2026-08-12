# tools/rediskeyevent

Redis 键空间事件（Keyspace Notification）订阅，监听键过期事件。

## 设计

Redis 启用 `notify-keyspace-events Ex` 后，当有键过期时会通过 Pub/Sub 频道广播事件。本包自动启用该配置，并提供前缀匹配过滤。

## 快速开始

```go
import (
    "context"
    "fmt"

    "github.com/Effortful-lion/unibase/tools/rediskeyevent"
    "github.com/redis/go-redis/v9"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

// 初始化验证码并设置过期（3 分钟）
rdb.Set(ctx, "code:user@example.com", "123456", 3*time.Minute)

// 订阅验证码键的过期事件
sub, err := rediskeyevent.Subscribe(rdb, 0, "code:*", func(key string) {
    fmt.Println("验证码已过期:", key)
    // 可在此处执行清理逻辑
})
defer sub.Close()
```

## API

### Subscribe

```go
func Subscribe(client redis.UniversalClient, db int, prefix string, h Handler) (*Subscriber, error)
```

| 参数 | 说明 |
|------|------|
| client | Redis 客户端 |
| db | 监听的数据库编号（0-15） |
| prefix | 键名前缀模式（如 `"code:*"`） |
| h | 键过期时的回调函数 |

### MustSubscribe

```go
func MustSubscribe(client redis.UniversalClient, db int, prefix string, h Handler) *Subscriber
```

同 Subscribe，但 panic 于错误（适合初始化时调用）。

### Close

```go
func (s *Subscriber) Close() error
```

停止监听。

## 前置条件

Redis 需启用键空间通知。本包在 Subscribe 时会自动执行：

```sql
CONFIG SET notify-keyspace-events Ex
```

> **注意**：`CONFIG SET` 是运行时配置，Redis 重启后失效。建议在 redis.conf 中持久化：
>
> ```
> notify-keyspace-events Ex
> ```

## 使用场景

- 验证码 / 短信验证码过期后自动清理
- 订单支付超时自动取消
- Session 过期事件通知
- 缓存预热触发（键删除时重新加载）

## 注意事项

- 仅监听 `expired` 事件（键自然过期），不监听 `del`（手动删除）
- 事件是异步的，可能有秒级延迟
- 回调函数应快速返回，避免阻塞 Pub/Sub 连接

## 依赖

- `github.com/redis/go-redis/v9`
