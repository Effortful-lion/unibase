# tools/breaker

基于 Time-Based Sliding Window 的熔断器，防止下游故障级联传播。

## 滑动窗口原理

将时间窗口拆分为 M 个固定时长的小桶（Bucket）。随着时间推移，过期桶被重置，指针向前滚动。决策时只统计仍在窗口内的桶。

```
 window = 10s, bucket = 1s  →  10 个桶

 ┌──────┐ ┌──────┐           ┌──────┐
 │  0   │ │  1   │  ...      │  9   │  ← ring buffer
 │t=1s  │ │t=2s  │           │t=10s │
 └───┬──┘ └───┬──┘           └───┬──┘
       head ─────────────────────┘
```

每 1 秒当前桶过期，head 前移，旧桶数据被丢弃。统计时只累加 `startTime` 在窗口内的桶。

## 状态机

```
 ┌──────────┐  failure rate ≥ threshold   ┌─────────┐
 │  Closed   │ ──────────────────────────> │  Open   │
 │ (监控失败率)│                            │ (拒绝请求)│
 └──────────┘                            └────┬────┘
        ▲                                     │ timeout
        │                                     ▼
        │                               ┌─────────────┐
        │  consecutive successes        │ Half-Open   │
        └────────────────────────────── │ (探测定额)  │
                                        └─────────────┘
```

- **Closed**：滑动窗口统计失败率，超过阈值 + 最少请求数 → 打开
- **Open**：拒绝全部请求，等待超时 → 半开
- **HalfOpen**：允许 `probeLimit` 个并发探测定请求；全部成功 → 关闭，任意失败 → 重新打开

## 快速开始

```go
import "github.com/Effortful-lion/unibase/tools/breaker"

// 最近 10 秒内失败率 ≥ 50% 且至少有 20 次请求 → 打开
// 30 秒后自动探测定，连续 2 次成功恢复
b := breaker.NewBreaker(
    breaker.WithFailureRateThreshold(0.5),
    breaker.WithMinRequests(20),
    breaker.WithSuccessThreshold(2),
    breaker.WithTimeout(30 * time.Second),
    breaker.WithWindow(10 * time.Second),
    breaker.WithBucket(1 * time.Second),
)

// 保护下游调用
err := b.Do(ctx, func(ctx context.Context) error {
    return downstream.Call(ctx)
})

if breaker.IsCircuitOpen(err) {
    return cachedValue, nil // 降级
}
```

## 带降级的调用

```go
err := b.DoWithFallback(ctx, fetch, func(cause error) error {
    log.Warnf("downstream unavailable: %v", cause)
    return getFromCache()
})
```

## 监听状态变更

```go
b := breaker.NewBreaker(
    breaker.WithOnStateChange(func(from, to breaker.State) {
        log.Infof("breaker: %s -> %s", from, to)
    }),
)
```

## 集成示例

### HTTP 中间件

```go
func CircuitBreakerMiddleware(b *breaker.CircuitBreaker) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            err := b.Do(r.Context(), func(ctx context.Context) error {
                next.ServeHTTP(w, r.WithContext(ctx))
                return nil
            })
            if breaker.IsCircuitOpen(err) {
                w.WriteHeader(http.StatusServiceUnavailable)
                json.NewEncoder(w).Encode(map[string]string{"error": "service unavailable"})
            }
        })
    }
}
```

### gRPC 拦截器

```go
func UnaryServerInterceptor(b *breaker.CircuitBreaker) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
        var resp any
        err := b.Do(ctx, func(ctx context.Context) error {
            var err error
            resp, err = handler(ctx, req)
            return err
        })
        return resp, err
    }
}
```

### 管理端点（手动重置）

```go
func adminResetHandler(b *breaker.CircuitBreaker) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        b.Reset()
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("breaker reset"))
    }
}
```

### 可观测性（Prometheus 指标）

```go
breaker.WithOnStateChange(func(from, to breaker.State) {
    stateChangeCounter.WithLabelValues(from.String(), to.String()).Inc()
})

// 定期采集窗口统计
func observeBreaker(b *breaker.CircuitBreaker) {
    c := b.Counters()
    if c.Empty() {
        return // 窗口无数据，不上报
    }
    requestTotal.Add(float64(c.Requests))
    failureTotal.Add(float64(c.Failures))
}
```

## API

### 构造函数

| 函数 | 说明 | 默认值 |
|------|------|--------|
| `NewBreaker(opts ...Option)` | 创建熔断器 | — |
| `WithFailureRateThreshold(rate)` | 失败率阈值 (0,1] | `0.5` |
| `WithMinRequests(n)` | 最少请求数（防误判） | `10` |
| `WithSuccessThreshold(n)` | 半开连续成功次数 | `2` |
| `WithProbeLimit(n)` | HalfOpen 最大并发探测定 | 等于 `successThreshold` |
| `WithTimeout(d)` | Open 等待时间 | `30s` |
| `WithWindow(d)` | 滑动窗口时长 | `10s` |
| `WithBucket(d)` | 桶粒度 | `1s` |
| `WithOnStateChange(hook)` | 状态变更回调 | — |

### 方法

| 方法 | 说明 |
|------|------|
| `Do(ctx, fn)` | 执行 fn，熔断时返回 `ErrCircuitOpen` |
| `DoWithFallback(ctx, fn, fallback)` | 熔断时调用 fallback |
| `State()` | 当前状态 |
| `Counters()` | 窗口快照（`Requests`/`Successes`/`Failures`/`FailureRate()`/`Empty()`） |
| `Reset()` | 强制重置为 Closed，清除所有数据 |
| `String()` | 可读状态摘要 |

### 错误判断

| 函数 | 说明 |
|------|------|
| `IsCircuitOpen(err)` | 判断是否为熔断错误 |
| `IsTooManyRequests(err)` | 判断是否为半开探测定额已满 |

## 配置示例

### 敏感场景（快速打开）

```go
breaker.NewBreaker(
    breaker.WithFailureRateThreshold(0.3),  // 30% 失败即打开
    breaker.WithMinRequests(5),              // 最少 5 次请求
    breaker.WithTimeout(10*time.Second),
    breaker.WithWindow(5*time.Second),
)
```

### 宽松场景（避免误判）

```go
breaker.NewBreaker(
    breaker.WithFailureRateThreshold(0.7),  // 70% 才打开
    breaker.WithMinRequests(50),             // 需要足够样本
    breaker.WithSuccessThreshold(3),         // 需要 3 次成功探测定
    breaker.WithTimeout(60*time.Second),
    breaker.WithWindow(30*time.Second),
)
```

## 与 Count-Based 方案的对比

| 维度 | Count-Based (Ring Buffer) | Time-Based (本实现) |
|------|---------------------------|---------------------|
| 时间精度 | 无，依赖请求密度 | 精确到 bucket 粒度 |
| 低流量表现 | 差，老数据过期慢 | 好，过期桶自动丢弃 |
| 调参直观性 | 差，N 需映射到时间 | 好，window + bucket 直接配置 |
| 实现复杂度 | ~50 行 | ~150 行 |
| 工业界采用 | 较少 | Hystrix、Resilience4j、Sentinel |

## 文件结构

```
tools/breaker/
├── go.mod
├── breaker.go
├── breaker_test.go
└── README.md
```

## 依赖

- 无外部依赖（仅使用标准库）

## 注意事项

- 每个下游服务建议使用独立的 `CircuitBreaker` 实例
- `StateChangeHook` 在锁外同步调用，保持钩子逻辑轻量；**禁止在 hook 内调用 breaker 的任何方法**（会导致死锁）
- 线程安全：所有方法均可被多个 goroutine 并发调用
- `WithWindow` 必须 ≥ `WithBucket`，否则自动钳制
- `Reset()` 用于管理端点或测试，不会触发 `StateChangeHook`
- `Counters.Empty()` 可区分"窗口无数据"和"零失败率"
- `WithProbeLimit` 独立控制 HalfOpen 并发探测定，与 `WithSuccessThreshold` 解耦
