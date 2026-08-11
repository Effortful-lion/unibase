package limiter

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ======================== NewLimiter ========================

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(10, 5)
	if l == nil {
		t.Fatal("NewLimiter returned nil")
	}
	if l.Rate() != 10 {
		t.Fatalf("Rate = %v, want 10", l.Rate())
	}
	if l.Burst() != 5 {
		t.Fatalf("Burst = %v, want 5", l.Burst())
	}
}

func TestNewLimiter_ZeroRate(t *testing.T) {
	// rate.Limit(0) becomes the minimum positive rate, no panic
	l := NewLimiter(0, 1)
	if l == nil {
		t.Fatal("NewLimiter(0, 1) returned nil")
	}
}

// ======================== Allow ========================

func TestAllow_InitiallyAvailable(t *testing.T) {
	l := NewLimiter(10, 5)
	// 刚创建时令牌是满的
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			t.Fatal("expected Allow to succeed when bucket is full")
		}
	}
}

func TestAllow_Exhausted(t *testing.T) {
	l := NewLimiter(10, 2)
	l.Allow() // 消耗 1
	l.Allow() // 消耗 1，桶空
	if l.Allow() {
		t.Fatal("expected Allow to fail when bucket is empty")
	}
}

// ======================== AllowN ========================

func TestAllowN_Success(t *testing.T) {
	l := NewLimiter(10, 5)
	if !l.AllowN(3) {
		t.Fatal("expected AllowN(3) to succeed")
	}
}

func TestAllowN_PartialConsumption(t *testing.T) {
	l := NewLimiter(10, 5)
	l.AllowN(3)
	// 剩余 2
	if !l.AllowN(2) {
		t.Fatal("expected AllowN(2) to succeed")
	}
	if l.AllowN(1) {
		t.Fatal("expected AllowN(1) to fail when bucket is empty")
	}
}

func TestAllowN_InvalidInput(t *testing.T) {
	l := NewLimiter(10, 5)
	if l.AllowN(0) {
		t.Fatal("expected AllowN(0) to return false")
	}
	if l.AllowN(-1) {
		t.Fatal("expected AllowN(-1) to return false")
	}
}

// ======================== Wait ========================

func TestWait_Immediate(t *testing.T) {
	l := NewLimiter(10, 5)
	start := time.Now()
	err := l.Wait(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait error: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Wait took %v, expected near-instant", elapsed)
	}
}

func TestWait_ContextCancel(t *testing.T) {
	l := NewLimiter(1, 1)
	l.Allow() // 消耗唯一令牌
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	err := l.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestWait_RefillsOverTime(t *testing.T) {
	// rate=10/s, burst=1 → 每 100ms  replenish 1 token
	l := NewLimiter(10, 1)
	l.Allow() // 消耗

	// 等待一个 replenish 周期
	time.Sleep(150 * time.Millisecond)
	start := time.Now()
	err := l.Wait(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait error after refill: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("Wait took %v after refill, expected near-instant", elapsed)
	}
}

// ======================== WaitN ========================

func TestWaitN_InvalidInput(t *testing.T) {
	l := NewLimiter(10, 5)

	err := l.WaitN(context.Background(), 0)
	if err == nil {
		t.Fatal("expected error for WaitN(0)")
	}

	err = l.WaitN(context.Background(), -1)
	if err == nil {
		t.Fatal("expected error for WaitN(-1)")
	}
}

// ======================== Reserve ========================

func TestReserve_Available(t *testing.T) {
	l := NewLimiter(10, 5)
	d := l.Reserve()
	if d != 0 {
		t.Fatalf("Reserve() = %v, want 0", d)
	}
}

func TestReserve_Exhausted(t *testing.T) {
	l := NewLimiter(10, 1)
	l.Allow() // 消耗唯一令牌
	d := l.Reserve()
	if d <= 0 {
		t.Fatalf("expected positive delay when bucket empty, got %v", d)
	}
	// 100 tokens/s → 100ms per token
	if d < 80*time.Millisecond || d > 200*time.Millisecond {
		t.Fatalf("Reserve delay = %v, expected ~100ms", d)
	}
}

// ======================== ReserveN ========================

func TestReserveN_MultipleTokens(t *testing.T) {
	l := NewLimiter(100, 10)
	// 请求 5 个令牌，桶满时应立即可用
	d := l.ReserveN(5)
	if d != 0 {
		t.Fatalf("ReserveN(5) = %v, want 0", d)
	}
}

func TestReserveN_InvalidInput(t *testing.T) {
	l := NewLimiter(10, 5)
	if l.ReserveN(0) != 0 {
		t.Fatal("ReserveN(0) should return 0")
	}
	if l.ReserveN(-1) != 0 {
		t.Fatal("ReserveN(-1) should return 0")
	}
}

// ======================== Rate / Burst ========================

func TestRateAndBurst(t *testing.T) {
	l := NewLimiter(42.5, 99)
	if l.Rate() != 42.5 {
		t.Fatalf("Rate = %v, want 42.5", l.Rate())
	}
	if l.Burst() != 99 {
		t.Fatalf("Burst = %v, want 99", l.Burst())
	}
}

// ======================== Integration: Concurrency ========================

func TestConcurrentAllow(t *testing.T) {
	l := NewLimiter(1000, 100)
	done := make(chan struct{})
	errors := make(chan int, 200)

	for i := 0; i < 200; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if !l.Allow() {
				errors <- 1
			}
		}()
	}
	for i := 0; i < 200; i++ {
		<-done
	}
	close(errors)

	failures := 0
	for range errors {
		failures++
	}
	if failures != 100 {
		t.Fatalf("concurrent Allow: %d failures, want 100 (burst=100)", failures)
	}
}

func TestConcurrentWait(t *testing.T) {
	l := NewLimiter(10, 2)
	results := make(chan error, 4)

	// 前 2 个立即可用
	for i := 0; i < 2; i++ {
		go func() {
			results <- l.Wait(context.Background())
		}()
	}

	// 再等一个 replenish 周期后，第 3 个可用
	time.Sleep(150 * time.Millisecond)
	go func() {
		results <- l.Wait(context.Background())
	}()

	for i := 0; i < 3; i++ {
		err := <-results
		if err != nil {
			t.Fatalf("concurrent Wait[%d] error: %v", i, err)
		}
	}
}
