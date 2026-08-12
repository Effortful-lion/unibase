// Package breaker implements a circuit breaker backed by a time-based sliding
// window.  It protects downstream services from cascading failures by
// monitoring the failure rate of wrapped calls and transitioning through
// three states — Closed, Open, HalfOpen.
//
// Quick start:
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
package breaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

// State represents the current state of the circuit breaker.
type State int

const (
	// StateClosed means the circuit is closed — requests flow normally and
	// the failure rate is monitored via the sliding window.
	StateClosed State = iota
	// StateOpen means the circuit is open — requests are rejected until the
	// configured timeout elapses.
	StateOpen
	// StateHalfOpen means the circuit is half-open — a limited number of
	// probe requests are allowed through to test whether the downstream has
	// recovered.
	StateHalfOpen
)

var stateNames = map[State]string{
	StateClosed:   "closed",
	StateOpen:     "open",
	StateHalfOpen: "half-open",
}

func (s State) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return fmt.Sprintf("State(%d)", s)
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// ErrCircuitOpen is returned by [CircuitBreaker.Do] when the circuit is open.
var ErrCircuitOpen = errors.New("breaker: circuit is open")

// ErrTooManyRequests is returned by [CircuitBreaker.Do] when the half-open
// probe limit has been reached.
var ErrTooManyRequests = errors.New("breaker: too many requests (half-open probe limit)")

// ---------------------------------------------------------------------------
// Counters
// ---------------------------------------------------------------------------

// Counters holds a snapshot of the current sliding-window statistics.
type Counters struct {
	Requests  uint32
	Successes uint32
	Failures  uint32
}

// FailureRate returns the failure rate within the window (0.0 – 1.0).
// Returns 0 if the window has no data.  Use [Counters.Empty] to distinguish
// "no data yet" from "zero failure rate".
func (c Counters) FailureRate() float64 {
	if c.Requests == 0 {
		return 0
	}
	return float64(c.Failures) / float64(c.Requests)
}

// Empty reports whether the window contains no request data.
func (c Counters) Empty() bool {
	return c.Requests == 0
}

// ---------------------------------------------------------------------------
// Rolling Window (internal)
// ---------------------------------------------------------------------------

// bucket holds success/failure counts for a single time slice.
type bucket struct {
	successCount uint32
	failureCount uint32
	startTime    time.Time
}

// rollingWindow implements a time-based sliding window over fixed-duration
// buckets stored in a ring buffer.
//
// Layout (example: window=10s, bucket=1s, 10 buckets):
//
//	┌──────┐ ┌──────┐           ┌──────┐
//	│  0   │ │  1   │  ...      │  9   │  ← ring buffer
//	│t=1s  │ │t=2s  │           │t=10s │
//	└───┬──┘ └───┬──┘           └───┬──┘
//	      head ─────────────────────┘
//
// As time advances, expired buckets are reset in-place and the head pointer
// advances.  Snapshot sums all buckets whose startTime is within windowDur
// of now.
type rollingWindow struct {
	mu          sync.RWMutex
	buckets     []bucket
	bucketCount int
	bucketDur   time.Duration
	windowDur   time.Duration
	head        int
}

func newRollingWindow(windowDur, bucketDur time.Duration) *rollingWindow {
	bucketCount := int(windowDur / bucketDur)
	if bucketCount < 1 {
		bucketCount = 1
		bucketDur = windowDur
	}
	now := time.Now()
	buckets := make([]bucket, bucketCount)
	for i := range buckets {
		buckets[i] = bucket{startTime: now}
	}
	return &rollingWindow{
		buckets:     buckets,
		bucketCount: bucketCount,
		bucketDur:   bucketDur,
		windowDur:   windowDur,
		head:        0,
	}
}

// record logs a request result in the current bucket, rotating expired
// buckets forward as needed.
func (w *rollingWindow) record(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()

	// When the current bucket has expired, advance head to the next position
	// and reset THAT bucket (which held the oldest data, now outside the
	// window).  Advancing first — instead of resetting the current head —
	// preserves data in the other buckets that is still inside the window.
	for now.Sub(w.buckets[w.head].startTime) >= w.bucketDur {
		w.head = (w.head + 1) % w.bucketCount
		w.buckets[w.head] = bucket{startTime: now}
	}

	if success {
		w.buckets[w.head].successCount++
	} else {
		w.buckets[w.head].failureCount++
	}
}

// snapshot returns (successes, failures) across all buckets still inside the
// sliding window.
func (w *rollingWindow) snapshot() (successes, failures uint32) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-w.windowDur)
	for i := 0; i < w.bucketCount; i++ {
		b := &w.buckets[i]
		if b.startTime.After(cutoff) {
			successes += b.successCount
			failures += b.failureCount
		}
	}
	return
}

// reset clears all buckets and resets the head pointer.
func (w *rollingWindow) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	for i := range w.buckets {
		w.buckets[i] = bucket{startTime: now}
	}
	w.head = 0
}

// ---------------------------------------------------------------------------
// Config & Options
// ---------------------------------------------------------------------------

// Config holds all configuration for a [CircuitBreaker].
type Config struct {
	failureRateThreshold float64
	minRequests          int
	successThreshold     int // consecutive successes to close from HalfOpen
	probeLimit           int // max concurrent probes allowed in HalfOpen
	timeout              time.Duration
	windowDuration       time.Duration
	bucketDuration       time.Duration
	onStateChange        []StateChangeHook
}

// Option applies a configuration option.
type Option func(*Config)

// WithFailureRateThreshold sets the failure rate that trips the breaker open.
// Value must be in (0, 1].  When the sliding-window failure rate meets or
// exceeds this value AND at least [WithMinRequests] requests have been seen,
// the breaker opens.  Defaults to 0.5 (50%).
func WithFailureRateThreshold(rate float64) Option {
	return func(c *Config) {
		if rate > 0 && rate <= 1.0 {
			c.failureRateThreshold = rate
		}
	}
}

// WithMinRequests sets the minimum number of requests in the window before
// the breaker is allowed to trip.  This prevents tripping on a handful of
// requests.  Must be > 0.  Defaults to 10.
func WithMinRequests(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.minRequests = n
		}
	}
}

// WithSuccessThreshold sets the number of consecutive probe successes required
// to close the breaker from HalfOpen.  Must be > 0.  Defaults to 2.
func WithSuccessThreshold(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.successThreshold = n
		}
	}
}

// WithProbeLimit sets the maximum number of concurrent probe requests allowed
// in HalfOpen state.  When the limit is reached, additional requests are
// rejected with [ErrTooManyRequests].  Must be >= [WithSuccessThreshold] for
// the probe limit to be meaningful (otherwise all probes succeed before the
// limit is tested).  Defaults to the value set by [WithSuccessThreshold] (or 2).
func WithProbeLimit(n int) Option {
	return func(c *Config) {
		if n > 0 {
			c.probeLimit = n
		}
	}
}

// WithTimeout sets how long to stay in Open before transitioning to
// HalfOpen.  Must be > 0.  Defaults to 30s.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithWindow sets the sliding window duration.  Must be >= [WithBucket].
// Defaults to 10s.
func WithWindow(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.windowDuration = d
		}
	}
}

// WithBucket sets the individual bucket duration.  Smaller values give finer
// time resolution at the cost of more buckets (and slightly more memory).
// Must be > 0.  Defaults to 1s.
func WithBucket(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.bucketDuration = d
		}
	}
}

// StateChangeHook is a callback invoked after every state transition.
//   - from: previous state
//   - to:   new state
//
// IMPORTANT: hooks run outside the breaker's lock but in the caller's
// goroutine.  Calling any method on the breaker (Do, State, Counters, etc.)
// from inside a hook will deadlock because sync.RWMutex is non-recursive.
// Keep hooks fast and side-effect-free; use them only for logging/metrics.
type StateChangeHook func(from, to State)

// WithOnStateChange registers a hook that is called on every state change.
// Multiple hooks may be registered; they are called in registration order.
func WithOnStateChange(hook StateChangeHook) Option {
	return func(c *Config) {
		if hook != nil {
			c.onStateChange = append(c.onStateChange, hook)
		}
	}
}

var defaultConfig = Config{
	failureRateThreshold: 0.5,
	minRequests:          10,
	successThreshold:     2,
	probeLimit:           2, // defaults to successThreshold value
	timeout:              30 * time.Second,
	windowDuration:       10 * time.Second,
	bucketDuration:       1 * time.Second,
}

// ---------------------------------------------------------------------------
// CircuitBreaker
// ---------------------------------------------------------------------------

// CircuitBreaker implements the circuit breaker pattern backed by a
// time-based sliding window.  It is safe for concurrent use by multiple
// goroutines.
//
// State transitions:
//
//	Closed   → Open   : window failure rate ≥ threshold AND total ≥ minRequests
//	Open     → HalfOpen: timeout since opening has elapsed
//	HalfOpen → Closed : consecutive probe successes ≥ successThreshold
//	HalfOpen → Open   : any single probe failure
//
// In HalfOpen state, up to probeLimit concurrent probes are allowed; excess
// requests receive ErrTooManyRequests.
type CircuitBreaker struct {
	mu                   sync.RWMutex
	state                State
	failureRateThreshold float64
	minRequests          int
	successThreshold     int // consecutive successes to close from HalfOpen
	probeLimit           int // max concurrent probes in HalfOpen
	timeout              time.Duration
	window               *rollingWindow
	probeInFlight        int       // probes currently executing in HalfOpen
	probeSuccessCount    int       // consecutive successes in HalfOpen
	openedAt             time.Time // set when state becomes Open; zero otherwise
	stateChangeHooks     []StateChangeHook
}

// NewBreaker creates a [CircuitBreaker] with the supplied options.
//
// Example:
//
//	b := breaker.NewBreaker(
//	    breaker.WithFailureRateThreshold(0.5),  // 50% failure rate
//	    breaker.WithMinRequests(20),            // at least 20 requests
//	    breaker.WithSuccessThreshold(2),
//	    breaker.WithTimeout(30*time.Second),
//	    breaker.WithWindow(10*time.Second),
//	    breaker.WithBucket(1*time.Second),
//	)
func NewBreaker(opts ...Option) *CircuitBreaker {
	cfg := defaultConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.windowDuration < cfg.bucketDuration {
		cfg.windowDuration = cfg.bucketDuration
	}
	// probeLimit defaults to successThreshold when not explicitly set.
	if cfg.probeLimit == 0 {
		cfg.probeLimit = cfg.successThreshold
	}

	return &CircuitBreaker{
		state:                StateClosed,
		failureRateThreshold: cfg.failureRateThreshold,
		minRequests:          cfg.minRequests,
		successThreshold:     cfg.successThreshold,
		probeLimit:           cfg.probeLimit,
		timeout:              cfg.timeout,
		window:               newRollingWindow(cfg.windowDuration, cfg.bucketDuration),
		stateChangeHooks:     cfg.onStateChange,
	}
}

// ---------------------------------------------------------------------------
// Observers (RLock — safe for concurrent reads)
// ---------------------------------------------------------------------------

// State returns the current state of the circuit breaker.
func (b *CircuitBreaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Counters returns a snapshot of the current sliding-window statistics.
func (b *CircuitBreaker) Counters() Counters {
	successes, failures := b.window.snapshot()
	return Counters{
		Requests:  successes + failures,
		Successes: successes,
		Failures:  failures,
	}
}

// Reset force-transitions the breaker back to Closed and clears all window
// data, probe counters, and timestamps.  StateChangeHooks are NOT fired.
//
// Use Reset for testing or admin-triggered recovery; normal operation should
// rely on the state machine's own transitions.
func (b *CircuitBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = StateClosed
	b.openedAt = time.Time{}
	b.probeSuccessCount = 0
	b.probeInFlight = 0
	b.window.reset()
}

// String returns a human-readable summary of the breaker's current state.
func (b *CircuitBreaker) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	successes, failures := b.window.snapshot()
	total := successes + failures
	rate := 0.0
	if total > 0 {
		rate = float64(failures) / float64(total)
	}
	var age time.Duration
	if !b.openedAt.IsZero() {
		age = time.Since(b.openedAt)
	}
	return fmt.Sprintf("breaker{state=%s window=%d reqs=%d failures=%d rate=%.1f%% open_for=%s}",
		b.state, b.window.bucketCount, total, failures, rate*100, age.Truncate(time.Millisecond))
}

// ---------------------------------------------------------------------------
// Internal state machine (caller must NOT hold b.mu)
// ---------------------------------------------------------------------------

// releaseProbe decrements probeInFlight when a HalfOpen probe never
// completed (e.g. context was cancelled after ready() succeeded).
// Safe to call from any state; only acts when state is HalfOpen and
// probeInFlight > 0.
func (b *CircuitBreaker) releaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateHalfOpen && b.probeInFlight > 0 {
		b.probeInFlight--
	}
}

// Caller must hold b.mu; it is released while hooks run and reacquired
// before returning.
func (b *CircuitBreaker) setState(to State) {
	from := b.state
	if from == to {
		return
	}

	switch to {
	case StateOpen:
		b.openedAt = time.Now()
		b.probeSuccessCount = 0
		b.probeInFlight = 0
	case StateHalfOpen:
		b.probeSuccessCount = 0
		b.probeInFlight = 0
		b.window.reset()
	case StateClosed:
		b.openedAt = time.Time{}
		b.probeSuccessCount = 0
		b.probeInFlight = 0
		b.window.reset()
	}

	b.state = to

	hooks := append([]StateChangeHook(nil), b.stateChangeHooks...)
	b.mu.Unlock()

	// Ensure the lock is always reacquired, even if a hook panics.
	defer func() {
		if r := recover(); r != nil {
			b.mu.Lock()
			panic(r)
		}
	}()

	for _, h := range hooks {
		h(from, to)
	}

	// Normal path: reacquire the lock.  The defer above is a no-op because
	// recover() returns nil when no panic occurred.
	b.mu.Lock()
}

// ready checks whether a request is allowed.  Returns ErrCircuitOpen or
// ErrTooManyRequests when the request should be rejected.
// b.mu must NOT be held by the caller.
func (b *CircuitBreaker) ready() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	switch b.state {
	case StateClosed:
		successes, failures := b.window.snapshot()
		total := int(successes + failures)
		if total >= b.minRequests {
			failureRate := float64(failures) / float64(total)
			if failureRate >= b.failureRateThreshold {
				b.setState(StateOpen)
				return ErrCircuitOpen
			}
		}
		return nil

	case StateOpen:
		if now.Sub(b.openedAt) >= b.timeout {
			b.setState(StateHalfOpen)
			// The transition above does not fall through to the HalfOpen
			// case, so acquire the probe slot explicitly here.
			b.probeInFlight++
			return nil
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		if b.probeInFlight >= b.probeLimit {
			return ErrTooManyRequests
		}
		b.probeInFlight++
		return nil

	default:
		return nil
	}
}

// markSuccess records a successful request and advances the state machine.
// b.mu must NOT be held by the caller.
func (b *CircuitBreaker) markSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.window.record(true)

	if b.state == StateHalfOpen {
		b.probeInFlight--
		b.probeSuccessCount++
		if b.probeSuccessCount >= b.successThreshold {
			b.setState(StateClosed)
		}
	}
}

// markFailure records a failed request and advances the state machine.
// If the window failure rate now exceeds the threshold, trips immediately.
// b.mu must NOT be held by the caller.
func (b *CircuitBreaker) markFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.window.record(false)

	if b.state == StateHalfOpen {
		b.setState(StateOpen)
		return
	}

	// In Closed state, check if the failure rate now exceeds the threshold
	// so we can trip immediately rather than waiting for the next request.
	if b.state == StateClosed {
		successes, failures := b.window.snapshot()
		total := int(successes + failures)
		if total >= b.minRequests {
			failureRate := float64(failures) / float64(total)
			if failureRate >= b.failureRateThreshold {
				b.setState(StateOpen)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// Do executes fn protected by the circuit breaker.
//
// Call flow:
//  1. Check ctx cancellation
//  2. ready() — allow or reject based on state and window stats
//  3. Call fn
//  4. Record result (success or failure), advancing the state machine
//
// When the circuit is open, Do returns [ErrCircuitOpen] without calling fn.
// When the half-open probe limit is reached, Do returns [ErrTooManyRequests].
func (b *CircuitBreaker) Do(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := b.ready(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		b.releaseProbe()
		return ctx.Err()
	}
	err := fn(ctx)
	if err != nil {
		b.markFailure()
		return err
	}
	b.markSuccess()
	return nil
}

// DoWithFallback is like [CircuitBreaker.Do] but invokes fallback(err) when
// the circuit is open, the half-open limit is reached, fn returns an error,
// or the context is cancelled.
//
// The fallback receives the original error (or [ErrCircuitOpen],
// [ErrTooManyRequests], or the context error).  The value returned by fallback
// is propagated to the caller.  Fallback calls do NOT affect breaker counters.
func (b *CircuitBreaker) DoWithFallback(ctx context.Context, fn func(context.Context) error, fallback func(error) error) error {
	if ctx.Err() != nil {
		return fallback(ctx.Err())
	}
	if err := b.ready(); err != nil {
		return fallback(err)
	}
	if ctx.Err() != nil {
		b.releaseProbe()
		return fallback(ctx.Err())
	}
	err := fn(ctx)
	if err != nil {
		b.markFailure()
		return fallback(err)
	}
	b.markSuccess()
	return nil
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------

// IsCircuitOpen reports whether err is [ErrCircuitOpen] or was wrapped by
// [errors.Is].
func IsCircuitOpen(err error) bool {
	return errors.Is(err, ErrCircuitOpen)
}

// IsTooManyRequests reports whether err is [ErrTooManyRequests] or was wrapped
// by [errors.Is].
func IsTooManyRequests(err error) bool {
	return errors.Is(err, ErrTooManyRequests)
}
