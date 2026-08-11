# lockdog

基于 Redis 的分布式锁组件，支持看门狗自动续期、安全释放和毫秒级 TTL 精度。

## 核心概念

| 概念 | 说明 |
|------|------|
| `Locker` | 分布式锁的获取入口，通过 `Lock` 方法尝试加锁 |
| `Lock` | 已持有的锁实例，通过 `Unlock` 释放 |
| `Token` | 每次加锁随机生成的唯一标识，用于安全释放和续期的所有权校验 |
| `Watchdog` | 后台 goroutine，在锁过期前自动续期，避免临界区执行时间超过 TTL |
| `Lua 脚本` | 释放和续期均通过 Lua 原子操作，防止误删/篡改他人持有的锁 |

## 快速开始

### 1. 创建 Locker 实例

```go
import lockdog "github.com/Effortful-lion/unibase/component/lockdog"

client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
locker := lockdog.New(client,
    lockdog.WithOwner("order-service"),
    lockdog.WithTTL(15*time.Second),
    lockdog.WithWatchdogInterval(5*time.Second),
)
```

### 2. 加锁与释放

```go
ctx := context.Background()
key := "order:create:1001"

lock, err := locker.Lock(ctx, key)
if err != nil {
    // lock not acquired — 锁已被他人持有
    return err
}
defer lock.Unlock(ctx) // 必须释放，推荐 defer

// 临界区：执行需要互斥的业务逻辑
processOrder(key)
```

### 3. 监听锁丢失信号

```go
lock, err := locker.Lock(ctx, key)
if err != nil {
    return err
}
defer lock.Unlock(ctx)

for {
    select {
    case <-lock.Lost():
        // 锁被其他进程抢占，立即停止临界区操作
        return lockdog.ErrLockLost
    case result := <-doWork():
        process(result)
    }
}
```

### 4. 使用自定义错误回调

```go
locker := lockdog.New(client,
    lockdog.WithOnRenewError(func(key, owner string, err error) {
        logx.Errorf("[lockdog] renew failed: key=%s owner=%s err=%v", key, owner, err)
    }),
)
```

## 选项

| 选项 | 说明 | 默认值 |
|------|------|--------|
| `WithTTL(d)` | 锁的过期时间。建议大于 `WatchdogInterval` 的 2 倍 | `15s` |
| `WithWatchdogInterval(d)` | 看门狗续期间隔。传 `0` 禁用看门狗 | `5s` |
| `WithOwner(s)` | 锁持有者标识，用于日志和调试 | `"unknown"` |
| `WithOnRenewError(fn)` | 看门狗续期失败时的回调 | `fmt.Printf` |

## 错误处理

| 错误 | 场景 |
|------|------|
| `ErrLockNotAcquired` | 锁已被他人持有，获取失败 |
| `ErrLockNotHeld` | 当前实例不持有该锁，无法释放（重复释放或 token 不匹配） |
| `ErrLockLost` | 锁在持有期间被其他进程抢占（TTL 过期后未及时续期），`Unlock` 时返回 |

## 特性

- **SET NX + 随机 Token**：加锁时生成 16 字节随机 token 作为 value，防止跨持有者误删
- **Lua 原子释放**：Unlock 通过 `unlockLua` 脚本仅当 token 匹配时才删除 key
- **Lua 原子续期**：Watchdog 通过 `renewLua` 脚本仅当 token 匹配时才续期，防止锁过期后被他人获取后又被旧持有者覆盖
- **毫秒级精度**：续期使用 `PEXPIRE`，亚秒级 TTL 不会被截断为 0
- **锁丢失信号**：`Lost()` channel 在看门狗检测到锁被他人抢占时关闭，调用方可通过 `select` 提前终止临界区
- **可注入错误回调**：`WithOnRenewError` 允许调用方自定义续期失败时的日志输出
- **panic 恢复**：看门狗 goroutine 内部有 `recover()`，防止 panic 导致锁静默过期

## 注意事项

- TTL 必须大于 `WatchdogInterval`，建议至少 2 倍以上，防止网络抖动时续期来不及触发
- `Lock` 传入的 `ctx` 决定了看门狗的存活时间。如果 ctx 超时或被取消，看门狗会停止续期，锁在 TTL 到期后自动过期。**确保 ctx 覆盖整个临界区**，或使用 `context.Background()`
- 必须调用 `Unlock` 释放锁，推荐 `defer lock.Unlock(ctx)`
- 禁用看门狗时（`WatchdogInterval(0)`），锁在 TTL 到期后自动过期，适合短临界区
- `WithOnRenewError(nil)` 表示忽略续期错误（不输出日志），默认行为是 `fmt.Printf`
- 集成测试需要本地 Redis 服务，测试会自动 skip 如果 Redis 不可用

## 文件结构

```
lockdog/
├── lock.go           # 核心锁逻辑（加锁/解锁/看门狗）
├── options.go        # 配置选项
├── errors.go         # 错误类型
├── lock_test.go      # 集成测试
└── *_test.go         # 单元测试
```
