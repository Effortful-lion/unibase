# tools/limiter

基于令牌桶算法的限速器，提供非阻塞放行、阻塞等待和预留三种流量控制模式。

## 快速开始

```go
import "github.com/Effortful-lion/unibase/tools/limiter"

// 每秒 100 个令牌，突发上限 10
l := limiter.NewLimiter(100, 10)

// 非阻塞：有令牌就放行
if l.Allow() {
    processRequest()
}

// 非阻塞：批量放行
if l.AllowN(5) {
    processBatch(5)
}

// 阻塞等待：等不到就超时
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()
if err := l.Wait(ctx); err != nil {
    return err
}
processRequest()
```

## API

| 方法 | 说明 |
|------|------|
| `NewLimiter(r float64, burst int)` | 创建限速器，r 为每秒令牌数，burst 为桶容量 |
| `Allow()` | 非阻塞尝试获取 1 个令牌，返回是否成功 |
| `AllowN(n int)` | 非阻塞尝试获取 n 个令牌 |
| `Wait(ctx)` | 阻塞等待直到获取 1 个令牌，ctx 控制超时/取消 |
| `WaitN(ctx, n)` | 阻塞等待直到获取 n 个令牌 |
| `Reserve()` | 预留 1 个令牌，返回需要等待的时长（0 = 立即可用） |
| `ReserveN(n)` | 预留 n 个令牌，返回等待时长 |
| `Rate()` | 返回每秒令牌生成速率 |
| `Burst()` | 返回令牌桶容量 |

## 使用场景

### 接口限流（非阻塞）

```go
if !l.Allow() {
    w.WriteHeader(http.StatusTooManyRequests)
    return
}
handleRequest(w, r)
```

### 下游限速（阻塞等待）

```go
// 调用第三方 API 前限速
if err := l.Wait(ctx); err != nil {
    return err
}
resp, err := client.Do(req)
```

### 批量并发限速

```go
// 批量并发请求，每组需 3 个令牌
if err := l.WaitN(ctx, 3); err != nil {
    return err
}
results, _ := pool.SubmitAll(ctx, fetch1, fetch2, fetch3)
```

### 异步调度（预留）

```go
// 提前计算需要 sleep 多久，用于定时任务调度
delay := l.ReserveN(10)
time.AfterFunc(delay, func() {
    batchProcess(10)
})
```

## 特性

- **线程安全**：多 goroutine 可并发调用
- **基于 `golang.org/x/time/rate`**：经过生产验证的令牌桶实现
- **三种模式覆盖**：`Allow` 适合"过就过，不过就丢"；`Wait` 适合"必须拿到"；`Reserve` 适合提前算好等待时间
- **零业务依赖**：仅依赖 `golang.org/x/time`

## 注意事项

- `Allow` 和 `AllowN` 是"用完即丢"模式，拒绝的请求不会排队，需要调用方自行决定是否丢弃、降级或排队
- `Wait` 和 `WaitN` 会阻塞当前 goroutine，适合后台任务或对延迟不敏感的场景，不适合 HTTP handler 等需要快速响应的路径
- `Reserve` 返回的等待时间可能非常短（微秒级），`time.AfterFunc` 的最小精度受系统调度影响
- 令牌桶的 `burst` 参数决定了最大突发量，设置过大会导致限流失效，建议不超过 `rate` 的 2~5 倍

## 文件结构

```
tools/limiter/
├── go.mod
├── limiter.go
└── limiter_test.go
```

## 依赖

- `golang.org/x/time`
