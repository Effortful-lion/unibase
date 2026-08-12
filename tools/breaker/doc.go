// Package breaker implements a circuit breaker backed by a time-based
// sliding window.
//
// The breaker monitors the failure rate of wrapped calls and transitions
// through three states — Closed, Open, HalfOpen — to protect downstream
// services from cascading failures.
//
// # Quick start
//
//	b := breaker.NewBreaker(
//	    breaker.WithFailureRateThreshold(0.5),
//	    breaker.WithMinRequests(20),
//	    breaker.WithSuccessThreshold(2),
//	    breaker.WithTimeout(30*time.Second),
//	    breaker.WithWindow(10*time.Second),
//	    breaker.WithBucket(1*time.Second),
//	)
//
//	err := b.Do(ctx, func(ctx context.Context) error {
//	    return downstream.Call(ctx)
//	})
//	if breaker.IsCircuitOpen(err) {
//	    // degrade gracefully
//	}
//
// # State machine
//
//	Closed   → Open   : window failure rate ≥ threshold AND total ≥ minRequests
//	Open     → HalfOpen: timeout since opening has elapsed
//	HalfOpen → Closed : consecutive probe successes ≥ successThreshold
//	HalfOpen → Open   : any single probe failure
//
// In HalfOpen the breaker allows at most successThreshold concurrent probe
// requests; additional callers receive [ErrTooManyRequests].
package breaker
