package gpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 10)

	if p.Capacity() != 10 {
		t.Errorf("Capacity: got %d, want 10", p.Capacity())
	}
	if p.Running() != 0 {
		t.Errorf("Running: got %d, want 0", p.Running())
	}
}

func TestNewPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for capacity <= 0")
		}
	}()
	New(context.Background(), 0)
}

// TestSubmitAndGet 验证基本提交-获取流程。
func TestSubmitAndGet(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	expected := "result"
	f, err := p.Submit(ctx, func(ctx context.Context) (any, error) {
		return expected, nil
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	val, err := f.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != expected {
		t.Errorf("Get: got %v, want %v", val, expected)
	}
}

// TestSubmitError 验证任务错误正确传播。
func TestSubmitError(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	expectedErr := errors.New("task failed")
	f, err := p.Submit(ctx, func(ctx context.Context) (any, error) {
		return nil, expectedErr
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	_, err = f.Get(ctx)
	if !errors.Is(err, expectedErr) {
		t.Errorf("Get error: got %v, want %v", err, expectedErr)
	}
}

// TestSubmitBlocksWhenFull 验证池满时 Submit 阻塞，直到有空位或 ctx 超时。
func TestSubmitBlocksWhenFull(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 2)
	defer p.Close()

	// 占满池子：两个任务阻塞直到 ctx 取消
	blockCh := make(chan struct{})
	for i := 0; i < 2; i++ {
		_, _ = p.Submit(ctx, func(ctx context.Context) (any, error) {
			<-blockCh
			return nil, nil
		})
	}

	// 第 3 个 Submit 应该阻塞直到超时
	ctx3, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err := p.Submit(ctx3, func(ctx context.Context) (any, error) {
		return nil, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded when pool full, got %v", err)
	}

	// 释放阻塞任务
	close(blockCh)
	p.Wait()
}

// TestConcurrencyControl 验证并发数不超过容量。
func TestConcurrencyControl(t *testing.T) {
	ctx := context.Background()
	capacity := 5
	p := New(ctx, capacity)
	defer p.Close()

	var peak int32
	var running int32

	// 用 goroutine 提交任务，避免主 goroutine 被阻塞
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				f, err := p.TrySubmit(ctx, func(ctx context.Context) (any, error) {
					atomic.AddInt32(&running, 1)
					cur := atomic.LoadInt32(&running)
					if cur > atomic.LoadInt32(&peak) {
						atomic.StoreInt32(&peak, cur)
					}
					time.Sleep(10 * time.Millisecond)
					atomic.AddInt32(&running, -1)
					return nil, nil
				})
				if err == nil {
					_ = f
					return
				}
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	p.Wait()

	if peak > int32(capacity) {
		t.Errorf("max concurrent: got %d, want <= %d", peak, capacity)
	}
}

// TestTrySubmitFull 验证非阻塞提交在池满时返回错误。
func TestTrySubmitFull(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 2)
	defer p.Close()

	// 占满池子
	var futures []*Future
	for i := 0; i < 2; i++ {
		f, err := p.Submit(ctx, func(ctx context.Context) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, nil
		})
		if err != nil {
			t.Fatalf("Submit %d: %v", i, err)
		}
		futures = append(futures, f)
	}

	// TrySubmit 应该失败
	_, err := p.TrySubmit(ctx, func(ctx context.Context) (any, error) {
		return nil, nil
	})
	if !errors.Is(err, ErrPoolFull) {
		t.Errorf("TrySubmit: got error %v, want ErrPoolFull", err)
	}

	// 等前面的完成
	for _, f := range futures {
		f.Get(ctx)
	}
}

// TestSubmitAll 验证批量提交。
func TestSubmitAll(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 10)
	defer p.Close()

	var count int32
	futures, err := p.SubmitAll(ctx,
		func(ctx context.Context) (any, error) { atomic.AddInt32(&count, 1); return "a", nil },
		func(ctx context.Context) (any, error) { atomic.AddInt32(&count, 2); return "b", nil },
		func(ctx context.Context) (any, error) { atomic.AddInt32(&count, 3); return "c", nil },
	)
	if err != nil {
		t.Fatalf("SubmitAll: %v", err)
	}
	if len(futures) != 3 {
		t.Fatalf("SubmitAll: got %d futures, want 3", len(futures))
	}

	p.Wait()
	if count != 6 { // 1 + 2 + 3
		t.Errorf("count: got %d, want 6", count)
	}
}

// TestSubmitAllPartialFailure 验证批量提交部分失败时返回已提交的 Future。
// SubmitAll 逐个 Submit，满时阻塞等待，所以部分失败只发生在池关闭时。
func TestSubmitAllPartialFailure(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 1)
	defer p.Close()

	// 先占满池子
	blocking, _ := p.Submit(ctx, func(ctx context.Context) (any, error) {
		time.Sleep(200 * time.Millisecond)
		return nil, nil
	})

	// 关闭池子后再 SubmitAll
	p.Close()

	futures, err := p.SubmitAll(ctx,
		func(ctx context.Context) (any, error) { return "x", nil },
	)
	if err == nil {
		t.Error("expected error for closed pool")
	}
	if len(futures) != 0 {
		t.Errorf("expected 0 futures on first failure, got %d", len(futures))
	}

	blocking.Get(ctx)
}

// TestCloseReturnsAccurateCount 验证 Close 返回当时正在运行的任务数。
func TestCloseReturnsAccurateCount(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)

	// 提交 3 个长时间任务
	var futures []*Future
	for i := 0; i < 3; i++ {
		f, _ := p.Submit(ctx, func(ctx context.Context) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, nil
		})
		futures = append(futures, f)
	}

	// 短暂等待确保任务已启动
	time.Sleep(20 * time.Millisecond)

	running := p.Close()
	if running != 3 {
		t.Errorf("Close running: got %d, want 3", running)
	}

	// 等待全部完成
	for _, f := range futures {
		f.Get(ctx)
	}
}

// TestCloseSubmitRace 验证并发调用 Close 和 Submit 不会发生 data race。
func TestCloseSubmitRace(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 10)

	// 先提交一些任务
	for i := 0; i < 5; i++ {
		_, _ = p.Submit(ctx, func(ctx context.Context) (any, error) {
			time.Sleep(50 * time.Millisecond)
			return nil, nil
		})
	}

	// 并发 Close 和 Submit
	var submitErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, err := p.Submit(ctx, func(ctx context.Context) (any, error) {
				return i, nil
			})
			if err != nil {
				submitErr = err
				break
			}
		}
	}()

	p.Close()
	wg.Wait()

	// Submit 可能在 Close 后失败，这是预期行为
	if submitErr != nil && !errors.Is(submitErr, ErrPoolClosed) {
		t.Errorf("unexpected submit error: %v", submitErr)
	}

	p.Wait()
}

// TestWait 验证 Wait 阻塞直到全部完成。
func TestWait(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	for i := 0; i < 10; i++ {
		_, _ = p.Submit(ctx, func(ctx context.Context) (any, error) {
			time.Sleep(50 * time.Millisecond)
			return nil, nil
		})
	}

	done := make(chan struct{})
	go func() {
		p.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Error("Wait did not return in time")
	}
}

// TestPanicRecovery 验证 panic 被 recover 并写回 Future。
func TestPanicRecovery(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	f, err := p.Submit(ctx, func(ctx context.Context) (any, error) {
		panic(" intentional panic ")
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	_, err = f.Get(ctx)
	if err == nil {
		t.Fatal("expected error from panicked task")
	}
	var pe *panicError
	if !errors.As(err, &pe) {
		t.Errorf("expected *panicError, got %T", err)
	}
	if len(pe.Stack()) == 0 {
		t.Error("expected non-empty stack trace")
	}
}

// TestPanicErrorAccessors 验证 panicError 的 Value 和 Stack 访问器。
func TestPanicErrorAccessors(t *testing.T) {
	pe := &panicError{
		value: "test panic",
		stack: []byte("stack trace"),
	}

	if pe.Value() != "test panic" {
		t.Errorf("Value: got %v, want 'test panic'", pe.Value())
	}
	if string(pe.Stack()) != "stack trace" {
		t.Errorf("Stack: got %s, want 'stack trace'", pe.Stack())
	}
	if pe.Error() != "gpool: task panic" {
		t.Errorf("Error: got %q, want 'gpool: task panic'", pe.Error())
	}
}

// TestContextCanceled 验证任务 ctx 取消时返回 context.Canceled。
// 取消时机可能在 Submit 阶段（acquire 检测到 ctx.Done），也可能在 Get 阶段（run 检测到 ctx.Err）。
func TestContextCanceled(t *testing.T) {
	ctx := context.Background()
	p := New(context.Background(), 5)
	defer p.Close()

	taskCtx, cancel := context.WithCancel(ctx)
	cancel() // 提交前取消

	f, err := p.Submit(taskCtx, func(ctx context.Context) (any, error) {
		return nil, nil
	})

	// 可能在 Submit 阶段就因 ctx 取消而失败
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Submit error: got %v, want context.Canceled", err)
		}
		return
	}

	// 如果 Submit 成功，Get 应该检测到取消
	_, err = f.Get(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Get: got %v, want context.Canceled", err)
	}
}

// TestGetNoRaceWithCanceledCtx 验证修复 Future.Get 的 select 竞态：
// 任务已完成 + 传入已取消的 ctx，必须返回任务结果而非 ctx.Err()。
func TestGetNoRaceWithCanceledCtx(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	f, _ := p.Submit(ctx, func(ctx context.Context) (any, error) {
		return "done", nil
	})

	// 等任务完成
	f.Get(ctx)

	// 传入已取消的 ctx 再次 Get，必须返回结果
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	val, err := f.Get(canceledCtx)
	if err != nil {
		t.Errorf("Get with canceled ctx: got error %v, want nil", err)
	}
	if val != "done" {
		t.Errorf("Get with canceled ctx: got %v, want 'done'", val)
	}
}

// TestReady 验证 Future.Ready 在完成前返回 false，完成后返回 true。
func TestReady(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	f, _ := p.Submit(ctx, func(ctx context.Context) (any, error) {
		time.Sleep(50 * time.Millisecond)
		return nil, nil
	})

	if f.Ready() {
		t.Error("Future should not be ready immediately")
	}

	f.Get(ctx)
	if !f.Ready() {
		t.Error("Future should be ready after Get")
	}
}

// TestDone 验证 Future.Done 返回的 channel 在任务完成时关闭。
func TestDone(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 5)
	defer p.Close()

	f, _ := p.Submit(ctx, func(ctx context.Context) (any, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, nil
	})

	select {
	case <-f.Done():
		t.Error("Done should not be closed yet")
	default:
	}

	f.Get(ctx)

	select {
	case <-f.Done():
		// ok
	default:
		t.Error("Done should be closed after completion")
	}
}

// TestMonitorFormat 验证监控字符串格式。
func TestMonitorFormat(t *testing.T) {
	ctx := context.Background()
	p := New(ctx, 10)

	got := p.MonitorFormat()
	want := "0/10"
	if got != want {
		t.Errorf("MonitorFormat: got %q, want %q", got, want)
	}
}
