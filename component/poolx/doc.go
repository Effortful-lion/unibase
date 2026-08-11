// Package poolx 提供基于 sync.Pool 的泛型对象池，封装类型安全、代际感知和背压控制。
//
// 核心设计：
//   - 泛型参数 T，Get 返回具体类型，零类型转换
//   - 继承 sync.Pool 的 best-effort 语义，不承诺对象存活
//   - 代际感知：通过 GenerationTracker 接口标记对象代际，ResetGeneration 使旧对象过期，
//     下次 Get 时自动触发 Init 重新初始化（Init 必须是幂等的，可能被多次调用；
//     调用成功后对象必须处于完全可用状态，SetPoolGeneration 会无条件标记为最新代际）
//   - 背压控制：可选 Limit 限制同时在外的对象数量，防止极端并发下内存膨胀
//   - 生命周期钩子：Reset 在 Put 前调用，用于清理对象状态
//
// 快速开始：
//
//	// 定义带代际标记的类型
//	type Request struct {
//	    body       []byte
//	    generation atomic.Int64
//	}
//
//	func (r *Request) PoolGeneration() int64      { return r.generation.Load() }
//	func (r *Request) SetPoolGeneration(g int64)  { r.generation.Store(g) }
//	func (r *Request) Reset()                      { r.body = r.body[:0] }
//
//	// 创建池（带背压上限 100）
//	pool := poolx.NewPool(poolx.Config[*Request]{
//	    New: func() *Request { return &Request{} },
//	    Reset: func(r *Request) { r.Reset() },
//	    Init: func(r *Request, gen int64) { r.body = make([]byte, 0, 1024) },
//	    Limit: 100,
//	})
//
//	// 使用
//	req := pool.Get()
//	defer pool.Put(req)
//
//	// 业务认为所有对象可能过期时，递增代际
//	pool.ResetGeneration()
//
// 不需要代际感知的类型，忽略 GenerationTracker 接口即可，池行为与 sync.Pool 一致。
package poolx
