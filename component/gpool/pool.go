package gpool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Pool 协程池。
//
// 使用信号量模式控制最大并发数，goroutine 由 Go runtime 调度，
// 不手动管理 worker 生命周期（Go 1.22+ 调度器已足够高效）。
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
type Pool struct {
	capacity int
	limiter  chan struct{}
	closed   atomic.Bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  atomic.Int64
}

// New 创建一个协程池。
//
// ctx 用于 pool 生命周期管理，通常传 context.Background()。
// capacity 是最大并发数，必须 > 0。
func New(ctx context.Context, capacity int) *Pool {
	if capacity <= 0 {
		panic("gpool: capacity must be positive")
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Pool{
		capacity: capacity,
		limiter:  make(chan struct{}, capacity),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Submit 提交一个任务，池满时阻塞直到有空位。
// 返回 *Future，通过 Future.Get(ctx) 获取结果。
// 池关闭后调用返回 ErrPoolClosed。
func (p *Pool) Submit(ctx context.Context, fn WorkFunc) (*Future, error) {
	f, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}
	p.execute(f, ctx, fn)
	return f, nil
}

// TrySubmit 非阻塞提交，池满时立即返回 (nil, ErrPoolFull)。
// 池关闭后调用返回 ErrPoolClosed。
func (p *Pool) TrySubmit(ctx context.Context, fn WorkFunc) (*Future, error) {
	f, err := p.tryAcquire(ctx)
	if err != nil {
		return nil, err
	}
	p.execute(f, ctx, fn)
	return f, nil
}

// SubmitAll 批量提交多个任务。
// 逐个阻塞提交，直到全部完成或某个因池满/池关闭失败。
// 已成功提交的任务会继续执行，需自行 Wait 等待完成。
func (p *Pool) SubmitAll(ctx context.Context, fns ...WorkFunc) ([]*Future, error) {
	futures := make([]*Future, 0, len(fns))
	for _, fn := range fns {
		f, err := p.Submit(ctx, fn)
		if err != nil {
			return futures, err
		}
		futures = append(futures, f)
	}
	return futures, nil
}

// Close 优雅关闭：拒绝新任务提交，取消池级上下文。
// 不会阻塞等待已有任务完成。如需等待全部完成，先 Close 再 Wait。
// 返回当时 Running() 的值，供日志/观测使用。
func (p *Pool) Close() int {
	p.closed.Store(true)
	p.cancel()
	return p.Running()
}

// Wait 阻塞直到所有已提交任务执行完毕。
// 通常在 Close 之后调用，也可以在运行中调用以同步等待。
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Running 返回当前正在执行的任务数（含信号量占位）。
func (p *Pool) Running() int {
	return int(p.running.Load())
}

// Capacity 返回池的最大并发容量。
func (p *Pool) Capacity() int {
	return p.capacity
}

// MonitorFormat 返回可读的监控字符串，格式 "running/capacity"。
func (p *Pool) MonitorFormat() string {
	return formatMonitor(p.Running(), p.capacity)
}

// acquire 阻塞式获取信号量。
func (p *Pool) acquire(ctx context.Context) (*Future, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	select {
	case <-p.ctx.Done():
		return nil, ErrPoolClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.limiter <- struct{}{}:
	}

	// 二次检查：防止 Close 发生在 Load 和 select 之间
	if p.closed.Load() {
		<-p.limiter
		return nil, ErrPoolClosed
	}

	p.running.Add(1)
	p.wg.Add(1)
	return newFuture(), nil
}

// tryAcquire 非阻塞获取信号量。
func (p *Pool) tryAcquire(ctx context.Context) (*Future, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	select {
	case <-p.ctx.Done():
		return nil, ErrPoolClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	select {
	case p.limiter <- struct{}{}:
	default:
		return nil, ErrPoolFull
	}

	// 二次检查：防止 Close 发生在 Load 和 select 之间
	if p.closed.Load() {
		<-p.limiter
		return nil, ErrPoolClosed
	}

	p.running.Add(1)
	p.wg.Add(1)
	return newFuture(), nil
}

// execute 在 goroutine 中执行任务。
func (p *Pool) execute(f *Future, taskCtx context.Context, fn WorkFunc) {
	go func() {
		defer p.wg.Done()
		defer func() {
			<-p.limiter
			p.running.Add(-1)
		}()
		f.run(taskCtx, fn)
	}()
}

// formatMonitor 格式化监控字符串。
func formatMonitor(running, capacity int) string {
	return fmt.Sprintf("%d/%d", running, capacity)
}
