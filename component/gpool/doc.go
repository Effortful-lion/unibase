// Package gpool 提供轻量级并发控制协程池。
//
// 核心能力：
//   - 信号量限流：Submit 和 TrySubmit 控制最大并发数
//   - Future 模式：每个任务返回 *Future，通过 Get(ctx) 获取结果
//   - 批量提交：SubmitAll 并发提交多个任务
//   - 优雅关闭：Close 拒绝新任务并等待已提交任务完成
//   - 可观测：Running / Capacity / MonitorFormat 监控池状态
//   - Panic 保护：任务 panic 不传播，写回 Future.Err
//
// 示例：
//
//	pool := gpool.New(ctx, 20)
//	defer pool.Close()
//
//	f, _ := pool.Submit(ctx, func(ctx context.Context) (any, error) {
//	    return fetchData(ctx)
//	})
//	result, err := f.Get(ctx)
//
//	// 批量提交
//	futures, _ := pool.SubmitAll(ctx, fn1, fn2, fn3)
//	for _, f := range futures {
//	    res, _ := f.Get(ctx)
//	}
package gpool
