package gpool

import (
	"context"
	"runtime/debug"
	"sync"
)

// WorkFunc 任务函数，接收上下文，返回 (结果, 错误)。
type WorkFunc func(ctx context.Context) (any, error)

// Future 异步任务结果句柄。
type Future struct {
	once sync.Once
	ch   chan struct{} // 关闭表示结果已就绪
	val  any
	err  error
}

// newFuture 创建一个未完成的 Future。
func newFuture() *Future {
	return &Future{ch: make(chan struct{})}
}

// set 写入结果，只执行一次。
func (f *Future) set(val any, err error) {
	f.once.Do(func() {
		f.val = val
		f.err = err
		close(f.ch)
	})
}

// setError 写入错误结果，只执行一次。
func (f *Future) setError(err error) {
	f.set(nil, err)
}

// Get 等待任务完成并返回结果。
//
// ctx 控制本次等待的超时/取消，与任务本身的上下文独立。
// 即使 ctx 超时或取消，后台任务仍会继续执行直到完成。
// 结果一旦就绪，后续调用 Get 会立即返回该结果，不再受 ctx 影响。
func (f *Future) Get(ctx context.Context) (any, error) {
	// 第一阶段：非阻塞检查，优先返回已完成的结果，避免 select 竞态。
	select {
	case <-f.ch:
		return f.val, f.err
	default:
	}

	// 第二阶段：等待完成或 ctx 取消。
	select {
	case <-f.ch:
		return f.val, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Done 返回一个在任务完成时关闭的 channel。
// 可用于 select 多路复用，或配合 reflect.Select 做超时控制。
func (f *Future) Done() <-chan struct{} {
	return f.ch
}

// Ready 返回结果是否已就绪（非阻塞）。
func (f *Future) Ready() bool {
	select {
	case <-f.ch:
		return true
	default:
		return false
	}
}

// run 执行任务函数，结果写回 Future。
// panic 会被 recover 并转为 error。
// 如果 taskCtx 已取消，立即返回 context 的错误（context.Canceled / context.DeadlineExceeded）。
func (f *Future) run(taskCtx context.Context, fn WorkFunc) {
	defer func() {
		if r := recover(); r != nil {
			f.setError(&panicError{
				value: r,
				stack: debug.Stack(),
			})
		}
	}()

	if err := taskCtx.Err(); err != nil {
		f.setError(err)
		return
	}

	val, err := fn(taskCtx)
	f.set(val, err)
}

// panicError 表示任务执行过程中发生了 panic。
type panicError struct {
	value any
	stack []byte
}

func (e *panicError) Error() string {
	return "gpool: task panic"
}

// Stack 返回 panic 时的调用栈。
func (e *panicError) Stack() []byte {
	return e.stack
}

// Value 返回 panic 的原始值。
func (e *panicError) Value() any {
	return e.value
}
