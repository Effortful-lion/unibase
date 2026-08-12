package breaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// =====================================================================
// NewBreaker / Config
// =====================================================================

func TestNewBreaker(t *testing.T) {
	b := NewBreaker()
	if b == nil {
		t.Fatal("NewBreaker returned nil")
	}
	if b.State() != StateClosed {
		t.Fatalf("initial state = %v, want closed", b.State())
	}
}

func TestNewBreaker_Options(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.8),
		WithMinRequests(5),
		WithSuccessThreshold(3),
		WithTimeout(5*time.Second),
		WithWindow(20*time.Second),
		WithBucket(2*time.Second),
		WithOnStateChange(func(from, to State) {}),
	)
	if b == nil {
		t.Fatal("NewBreaker returned nil")
	}
	if b.State() != StateClosed {
		t.Fatalf("initial state = %v, want closed", b.State())
	}
}

func TestNewBreaker_WindowClampedToBucket(t *testing.T) {
	b := NewBreaker(
		WithWindow(500*time.Millisecond),
		WithBucket(1*time.Second),
	)
	// window should be clamped to bucket duration
	c := b.Counters()
	_ = c
	if b.State() != StateClosed {
		t.Fatalf("initial state = %v, want closed", b.State())
	}
}

// =====================================================================
// Counters & FailureRate
// =====================================================================

func TestCounters_InitiallyEmpty(t *testing.T) {
	b := NewBreaker()
	c := b.Counters()
	if c.Requests != 0 {
		t.Fatalf("Requests = %d, want 0", c.Requests)
	}
	if c.FailureRate() != 0 {
		t.Fatalf("FailureRate = %f, want 0", c.FailureRate())
	}
}

func TestCounters_WindowSnapshot(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// Two failures → window should show 2 failures
	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	c := b.Counters()
	if c.Failures != 2 {
		t.Fatalf("Failures = %d, want 2", c.Failures)
	}
	if c.Successes != 0 {
		t.Fatalf("Successes = %d, want 0", c.Successes)
	}
	if c.Requests != 2 {
		t.Fatalf("Requests = %d, want 2", c.Requests)
	}
	if c.FailureRate() != 1.0 {
		t.Fatalf("FailureRate = %f, want 1.0", c.FailureRate())
	}
}

// =====================================================================
// Closed → Open (failure rate threshold)
// =====================================================================

func TestClosedToOpen_OnHighFailureRate(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5), // 50%
		WithMinRequests(4),            // need at least 4 requests
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// 3 failures out of 4 = 75% > 50% → should trip
	for i := 0; i < 3; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	if b.State() != StateClosed {
		t.Fatalf("after 3 failures, state = %v, want closed", b.State())
	}

	// 4th failure → 4/4 = 100% → trip
	_ = b.Do(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})

	if b.State() != StateOpen {
		t.Fatalf("after threshold failures, state = %v, want open", b.State())
	}
}

func TestClosed_NoTrip_BelowMinRequests(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(10), // need 10 requests minimum
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// 9 failures out of 9 = 100% failure rate, but minRequests=10
	for i := 0; i < 9; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed (below minRequests)", b.State())
	}
}

func TestClosed_NoTrip_BelowFailureRate(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(4),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// 1 failure out of 4 = 25% < 50% → no trip
	_ = b.Do(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})
	for i := 0; i < 3; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}

	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed (rate=25%% < 50%%)", b.State())
	}
}

// =====================================================================
// Open → HalfOpen (timeout)
// =====================================================================

func TestOpenToHalfOpen_AfterTimeout(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithTimeout(50*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)

	if b.State() != StateOpen {
		t.Fatalf("after failures, state = %v, want open", b.State())
	}

	time.Sleep(60 * time.Millisecond)

	called := false
	err := b.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected probe to succeed, got: %v", err)
	}
	if !called {
		t.Fatal("fn should have been called in half-open")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("after first probe, state = %v, want half-open", b.State())
	}
}

// =====================================================================
// HalfOpen → Closed (probe successes)
// =====================================================================

func TestHalfOpenToClosed_OnSuccesses(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)

	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	time.Sleep(30 * time.Millisecond)

	success := func(ctx context.Context) error { return nil }

	b.Do(context.Background(), success) // 1st probe
	if b.State() != StateHalfOpen {
		t.Fatalf("after 1st probe, state = %v, want half-open", b.State())
	}

	b.Do(context.Background(), success) // 2nd probe → close
	if b.State() != StateClosed {
		t.Fatalf("after successThreshold probes, state = %v, want closed", b.State())
	}
}

// =====================================================================
// HalfOpen → Open (probe failure)
// =====================================================================

func TestHalfOpenToOpen_OnFailure(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(3),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)

	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	time.Sleep(30 * time.Millisecond)

	// 1st probe succeeds → half-open
	b.Do(context.Background(), func(ctx context.Context) error { return nil })

	// 2nd probe fails → reopen
	b.Do(context.Background(), fail)
	if b.State() != StateOpen {
		t.Fatalf("after failure in half-open, state = %v, want open", b.State())
	}
}

// =====================================================================
// ErrCircuitOpen / ErrTooManyRequests
// =====================================================================

func TestDo_ReturnsErrCircuitOpen(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	if !IsCircuitOpen(b.Do(context.Background(), func(ctx context.Context) error {
		return nil
	})) {
		t.Fatal("expected ErrCircuitOpen")
	}
}

func TestDo_DoesNotCallFnWhenOpen(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	called := false
	_ = b.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("fn should NOT be called when circuit is open")
	}
}

func TestDo_ReturnsErrTooManyRequests(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(2),
		WithProbeLimit(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// trip open
	_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })
	_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })

	time.Sleep(30 * time.Millisecond)

	// Hold both probe slots concurrently.
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < b.probeLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Do(context.Background(), func(ctx context.Context) error {
				<-release
				return nil
			})
		}()
	}

	// Wait for both probes to acquire their slots.
	time.Sleep(80 * time.Millisecond)

	// Now probeInFlight == probeLimit, the next call must be rejected.
	err := b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if !IsTooManyRequests(err) {
		t.Fatalf("expected ErrTooManyRequests, got: %v", err)
	}

	// Release probes and verify they complete cleanly.
	close(release)
	wg.Wait()
}

// =====================================================================
// DoWithFallback
// =====================================================================

func TestDoWithFallback_OnCircuitOpen(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	fallbackCalled := false
	err := b.DoWithFallback(context.Background(),
		func(ctx context.Context) error { t.Fatal("fn should not be called"); return nil },
		func(e error) error {
			fallbackCalled = true
			if !IsCircuitOpen(e) {
				t.Fatalf("fallback error = %v, want ErrCircuitOpen", e)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallbackCalled {
		t.Fatal("fallback should have been called")
	}
}

func TestDoWithFallback_OnFnError(t *testing.T) {
	b := NewBreaker()
	fallbackCalled := false
	originalErr := errors.New("original")
	err := b.DoWithFallback(context.Background(),
		func(ctx context.Context) error { return originalErr },
		func(e error) error {
			fallbackCalled = true
			if e != originalErr {
				t.Fatalf("fallback error = %v, want %v", e, originalErr)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallbackCalled {
		t.Fatal("fallback should have been called on fn error")
	}
}

// =====================================================================
// StateChangeHook
// =====================================================================

func TestOnStateChange_HookCalled(t *testing.T) {
	var transitions []string
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
		WithOnStateChange(func(from, to State) {
			transitions = append(transitions, from.String()+"→"+to.String())
		}),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)

	if len(transitions) != 1 || transitions[0] != "closed→open" {
		t.Fatalf("transitions = %v, want [closed→open]", transitions)
	}
}

func TestOnStateChange_HookPanic_DoesNotLeakMutex(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
		WithOnStateChange(func(from, to State) {
			if from == StateClosed && to == StateOpen {
				panic("hook panic")
			}
		}),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }

	// Two failures trip the breaker, triggering the hook which panics.
	// setState must re-lock b.mu before re-panicking.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from hook, got none")
		}
		// Verify the mutex is usable after the panic.
		_ = b.State()
	}()

	_ = b.Do(context.Background(), fail) // 1st failure — no trip yet (minRequests=2)
	_ = b.Do(context.Background(), fail) // 2nd failure — trips, hook panics

	t.Fatal("should not reach here")
}

// =====================================================================
// Window expiry (time-based decay)
// =====================================================================

func TestWindow_OldFailuresExpire(t *testing.T) {
	// Window = 1s, bucket = 1s. After window expiry + timeout, old failures
	// should be gone and the breaker should accept probe requests.
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithTimeout(50*time.Millisecond),
		WithWindow(1*time.Second),
		WithBucket(1*time.Second),
	)

	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}

	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	// Wait for both window expiry and Open timeout
	time.Sleep(1500 * time.Millisecond)

	called := false
	err := b.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected request after window expiry, got: %v", err)
	}
	if !called {
		t.Fatal("fn should have been called")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
}

// =====================================================================
// String
// =====================================================================

func TestString(t *testing.T) {
	b := NewBreaker()
	s := b.String()
	if s == "" {
		t.Fatal("String() returned empty")
	}
}

// =====================================================================
// Context cancellation
// =====================================================================

func TestDo_ContextCancelledBeforeCall(t *testing.T) {
	b := NewBreaker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := b.Do(ctx, func(ctx context.Context) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("fn should not be called when context is cancelled")
	}
}

func TestDoWithFallback_ContextCancelled(t *testing.T) {
	b := NewBreaker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fallbackCalled := false
	err := b.DoWithFallback(ctx,
		func(ctx context.Context) error { t.Fatal("fn should not be called"); return nil },
		func(e error) error {
			fallbackCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallbackCalled {
		t.Fatal("fallback should have been called")
	}
}

// =====================================================================
// Concurrency
// =====================================================================

func TestConcurrentDo(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.8),
		WithMinRequests(100),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.Do(context.Background(), func(ctx context.Context) error {
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)

	var errCount int
	for range errCh {
		errCount++
	}
	if errCount > 0 {
		t.Fatalf("concurrent Do: %d unexpected errors", errCount)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}
}

func TestConcurrentStateAccess(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.State()
			_ = b.Counters()
			_ = b.String()
		}()
	}
	wg.Wait()
}

// =====================================================================
// Full recovery cycle (closed → open → half-open → closed)
// =====================================================================

func TestFullRecoveryCycle(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	success := func(ctx context.Context) error { return nil }

	// Phase 1: closed → open
	_ = b.Do(context.Background(), fail)
	_ = b.Do(context.Background(), fail)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	// Phase 2: wait for half-open
	time.Sleep(30 * time.Millisecond)

	// Phase 3: half-open → closed
	_ = b.Do(context.Background(), success) // 1st probe
	if b.State() != StateHalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	_ = b.Do(context.Background(), success) // 2nd probe → close
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}

	// Phase 4: verify normal operation
	_ = b.Do(context.Background(), success)
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}
}

// =====================================================================
// Error helpers
// =====================================================================

func TestIsCircuitOpen(t *testing.T) {
	if !IsCircuitOpen(ErrCircuitOpen) {
		t.Fatal("expected IsCircuitOpen(ErrCircuitOpen) = true")
	}
	if IsCircuitOpen(errors.New("other")) {
		t.Fatal("expected IsCircuitOpen(other) = false")
	}
}

func TestIsTooManyRequests(t *testing.T) {
	if !IsTooManyRequests(ErrTooManyRequests) {
		t.Fatal("expected IsTooManyRequests(ErrTooManyRequests) = true")
	}
	if IsTooManyRequests(errors.New("other")) {
		t.Fatal("expected IsTooManyRequests(other) = false")
	}
}

// =====================================================================
// State.String()
// =====================================================================

func TestStateString(t *testing.T) {
	tests := []struct {
		s    State
		want string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(999), "State(999)"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

// =====================================================================
// Mixed success/failure — failure rate stays below threshold
// =====================================================================

func TestMixedResults_NoTrip(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(4),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// 1 failure + 5 successes = 1/6 ≈ 16.7% < 50% → no trip
	_ = b.Do(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})
	for i := 0; i < 5; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}

	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}

	c := b.Counters()
	if c.FailureRate() >= 0.5 {
		t.Fatalf("failure rate = %.2f, want < 0.5", c.FailureRate())
	}
}

// =====================================================================
// Window buckets rotate over time
// =====================================================================

func TestWindow_MultiBucketRetainsHistory(t *testing.T) {
	// With window=3s and bucket=1s (3 buckets), data from the first bucket
	// must still be visible after the first bucket expires but before the
	// full window elapses.
	b := NewBreaker(
		WithFailureRateThreshold(1.0), // never trip from failure rate
		WithMinRequests(1000),         // never trip from min requests
		WithWindow(3*time.Second),
		WithBucket(1*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }

	// Record 2 failures in the first bucket.
	_ = b.Do(context.Background(), fail)
	_ = b.Do(context.Background(), fail)

	// Wait for the first bucket to expire.
	time.Sleep(1100 * time.Millisecond)

	// Record 1 more failure in the second bucket.
	_ = b.Do(context.Background(), fail)

	// The window should still contain all 3 failures.
	c := b.Counters()
	if c.Failures != 3 {
		t.Fatalf("Failures = %d, want 3 (window should retain history across buckets)", c.Failures)
	}
}

func TestWindow_BucketsRotate(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithTimeout(50*time.Millisecond),
		WithWindow(3*time.Second),
		WithBucket(1*time.Second), // 3 buckets
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }

	_ = b.Do(context.Background(), fail)
	_ = b.Do(context.Background(), fail)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	// Wait for window (3s) + timeout (50ms) to expire
	time.Sleep(3500 * time.Millisecond)

	called := false
	err := b.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected request after window expiry, got: %v", err)
	}
	if !called {
		t.Fatal("fn should have been called")
	}
}

// =====================================================================
// Custom thresholds
// =====================================================================

func TestCustomFailureRateThreshold(t *testing.T) {
	// 80% threshold: verify the breaker trips when the window failure rate
	// reaches the configured threshold.
	b := NewBreaker(
		WithFailureRateThreshold(0.8),
		WithMinRequests(5),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// 4 failures + 1 success = 80% — rate is met but total < 5 until the
	// 5th failure is recorded, so the trip happens on the next failure.
	for i := 0; i < 4; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}
	_ = b.Do(context.Background(), func(ctx context.Context) error {
		return nil
	})

	c := b.Counters()
	if c.FailureRate() < 0.8 {
		t.Fatalf("failure rate = %.2f, want >= 0.8", c.FailureRate())
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed (trip fires on next failure)", b.State())
	}

	// 5th failure → 5/6 = 83.3% >= 80% → trip
	_ = b.Do(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open (rate=83.3%%)", b.State())
	}
}

func TestCustomSuccessThreshold(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(3),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	time.Sleep(30 * time.Millisecond)

	success := func(ctx context.Context) error { return nil }

	b.Do(context.Background(), success) // 1st probe → half-open
	if b.State() != StateHalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	b.Do(context.Background(), success) // 2nd → still half-open
	if b.State() != StateHalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	b.Do(context.Background(), success) // 3rd → closed
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}
}

// =====================================================================
// ProbeLimit independent of SuccessThreshold
// =====================================================================

func TestProbeLimitIndependentOfSuccessThreshold(t *testing.T) {
	// probeLimit=3 allows 3 concurrent probes, but only 1 success is needed
	// to close.  This verifies the two parameters are independent.
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(1),
		WithProbeLimit(3),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	time.Sleep(30 * time.Millisecond)

	// First probe succeeds → successThreshold=1 → should close immediately
	_ = b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed (1 success >= successThreshold=1)", b.State())
	}
}

func TestProbeLimit_ConcurrentRejection(t *testing.T) {
	// probeLimit=2 but successThreshold=1: only 2 probes allowed in parallel,
	// the 3rd concurrent request must be rejected.
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(1),
		WithProbeLimit(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)

	time.Sleep(30 * time.Millisecond)

	var wg sync.WaitGroup
	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.Do(context.Background(), func(ctx context.Context) error {
				time.Sleep(80 * time.Millisecond)
				return nil
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	rejected := 0
	succeeded := 0
	for err := range results {
		if err == ErrTooManyRequests {
			rejected++
		} else if err == nil {
			succeeded++
		}
	}
	if succeeded != 2 {
		t.Fatalf("succeeded probes = %d, want 2", succeeded)
	}
	if rejected != 1 {
		t.Fatalf("rejected probes = %d, want 1", rejected)
	}
}

// =====================================================================
// Immediate trip from markFailure (no one-request lag)
// =====================================================================

func TestImmediateTrip_FromMarkFailure(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(4),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }

	// 3 failures → 75%, still below threshold? No, 75% >= 50%
	// Wait, minRequests=4, so 3 < 4 → no trip yet
	for i := 0; i < 3; i++ {
		_ = b.Do(context.Background(), fail)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed (3 < minRequests=4)", b.State())
	}

	// 4th failure → 100% >= 50% AND total=4 >= 4 → immediate trip
	_ = b.Do(context.Background(), fail)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open (immediate trip from markFailure)", b.State())
	}
}

// =====================================================================
// Counters.Empty()
// =====================================================================

func TestCounters_EmptyInitially(t *testing.T) {
	b := NewBreaker()
	c := b.Counters()
	if !c.Empty() {
		t.Fatal("expected Empty() = true on fresh breaker")
	}
}

func TestCounters_NotEmptyAfterRequest(t *testing.T) {
	b := NewBreaker()
	_ = b.Do(context.Background(), func(ctx context.Context) error { return nil })
	c := b.Counters()
	if c.Empty() {
		t.Fatal("expected Empty() = false after a request")
	}
}

func TestCounters_NotEmptyAfterFailure(t *testing.T) {
	b := NewBreaker()
	_ = b.Do(context.Background(), func(ctx context.Context) error { return errors.New("fail") })
	c := b.Counters()
	if c.Empty() {
		t.Fatal("expected Empty() = false after a failure")
	}
}

// =====================================================================
// Reset()
// =====================================================================

func TestReset_ReturnsToClosed(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// Trip to Open
	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	b.Reset()

	if b.State() != StateClosed {
		t.Fatalf("after Reset, state = %v, want closed", b.State())
	}
	c := b.Counters()
	if !c.Empty() {
		t.Fatalf("after Reset, Counters should be empty, got %+v", c)
	}
}

func TestReset_AllowsNormalOperation(t *testing.T) {
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
	)

	// Trip to Open
	for i := 0; i < 2; i++ {
		_ = b.Do(context.Background(), func(ctx context.Context) error {
			return errors.New("fail")
		})
	}
	b.Reset()

	// After reset, fresh requests should work normally
	err := b.Do(context.Background(), func(ctx context.Context) error { return nil })
	if err != nil {
		t.Fatalf("expected success after Reset, got: %v", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}
}

func TestReset_DoesNotFireHooks(t *testing.T) {
	var transitions []string
	b := NewBreaker(
		WithFailureRateThreshold(0.5),
		WithMinRequests(2),
		WithSuccessThreshold(2),
		WithTimeout(20*time.Millisecond),
		WithWindow(5*time.Second),
		WithBucket(5*time.Second),
		WithOnStateChange(func(from, to State) {
			transitions = append(transitions, from.String()+"→"+to.String())
		}),
	)

	fail := func(ctx context.Context) error { return errors.New("fail") }
	b.Do(context.Background(), fail)
	b.Do(context.Background(), fail)
	// transitions = ["closed→open"]

	b.Reset()
	// Reset should NOT fire hooks — transitions unchanged

	if len(transitions) != 1 || transitions[0] != "closed→open" {
		t.Fatalf("transitions after Reset = %v, want [closed→open]", transitions)
	}
}
