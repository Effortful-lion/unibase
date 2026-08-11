# poolx

基于 `sync.Pool` 的泛型对象池，提供类型安全、代际感知和背压控制。

## 演进方向

初期聚焦类型安全和代际感知，后续视使用场景扩展 metrics 能力。

## 核心能力

- 泛型参数 T，Get 返回具体类型，零类型转换
- 继承 sync.Pool 的 best-effort 语义，不承诺对象存活
- 代际感知：ResetGeneration 使旧对象过期，下次 Get 时自动触发 Init 重新初始化
- 背压控制：可选 Limit 限制同时在外的对象数量
- 生命周期钩子：Reset 在 Put 前调用，用于清理对象状态

## 快速开始

```go
package main

import "github.com/Effortful-lion/unibase/component/poolx"

func main() {
    pool := poolx.NewPool(poolx.Config[[]byte]{
        New: func() []byte { return make([]byte, 0, 4096) },
        Reset: func(b []byte) { b = b[:0] },
    })

    buf := pool.Get()
    defer pool.Put(buf)

    buf = append(buf, "hello"...)
}
```

## 代际感知示例

```go
type Request struct {
    body       []byte
    generation atomic.Int64
}

func (r *Request) PoolGeneration() int64      { return r.generation.Load() }
func (r *Request) SetPoolGeneration(g int64)  { r.generation.Store(g) }
func (r *Request) Reset()                      { r.body = r.body[:0] }

pool := poolx.NewPool(poolx.Config[*Request]{
    New:   func() *Request { return &Request{} },
    Reset: func(r *Request) { r.Reset() },
    Init: func(r *Request, gen int64) {
        r.body = make([]byte, 0, 1024)
    },
    Limit: 100,
})

req := pool.Get()
defer pool.Put(req)

// 业务认为所有对象可能过期时，递增代际
pool.ResetGeneration()
```

## API

| 方法 | 说明 |
|------|------|
| `NewPool(cfg Config[T]) *Pool[T]` | 创建对象池，New 为必填项 |
| `Get() T` | 从池中取出对象，Limit 满时阻塞 |
| `TryGet() (T, bool)` | 非阻塞尝试取对象，背压满时返回 false |
| `Put(v T)` | 归还对象，Put 前调用 Reset（如果配置了） |
| `Generation() int64` | 返回当前代际编号 |
| `ResetGeneration()` | 递增代际，使旧对象标记为过期 |

## Config 字段

| 字段 | 说明 |
|------|------|
| `New func() T` | 必填：创建新实例 |
| `Reset func(T)` | 可选：Put 前调用，重置对象状态 |
| `Init func(T, int64)` | 可选：对象代际过期时调用，必须是幂等的 |
| `Limit int64` | 可选：背压上限，非正数表示无限制 |

## 注意事项

- Init 必须是幂等的，可能被多次调用
- Init 调用成功后对象必须处于完全可用状态，SetPoolGeneration 会无条件标记为最新代际
- sync.Pool 不保证对象存活，GC 执行时池内对象可能被整体清空
- 对于 Pool[*T] 类型，传入 nil 指针会按普通对象处理，Reset 函数应能处理 nil

## 文件结构

```
component/poolx/
├── doc.go          # 包文档
├── pool.go         # Pool 结构体 + Get/Put/Generation
├── pool_test.go    # 测试
├── go.mod          # 模块定义
└── README.md       # 本文档
```

## 依赖

- 标准库 `sync`、`sync/atomic`，无外部依赖
