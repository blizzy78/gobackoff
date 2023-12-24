package gobackoff

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// Backoff implements a backoff algorithm.
type Backoff struct {
	initialDelay time.Duration
	maxDelay     time.Duration
	multiplier   float64
	jitter       float64
	waitFunc     waitFunc

	mu            sync.Mutex
	nextBaseDelay time.Duration
	nextDelay     time.Duration
}

// Opt is a function that configures a Backoff.
type Opt func(backoff *Backoff)

// RetryableFunc is a function that can be retried.
// The current attempt number is available using AttemptFromContext.
type RetryableFunc func(ctx context.Context) error

// AbortError is returned by a RetryableFunc to abort the retry loop.
type AbortError struct {
	// Err is the error that caused the abort, if any.
	Err error
}

// MaxAttemptsError is returned by Do when the maximum number of attempts is reached.
type MaxAttemptsError struct {
	// Err is the last error returned by the RetryableFunc, if any.
	Err error
}

type waitFunc func(ctx context.Context) error

type contextKey struct{}

var (
	_ error = (*AbortError)(nil)
	_ error = (*MaxAttemptsError)(nil)
)

var attemptContextKey = contextKey{}

// New creates a new Backoff with the given options.
// The default options are: Initial delay of 500ms, maximum delay of 10s, multiplier of 1.5 and jitter of 1.0.
func New(opts ...Opt) *Backoff {
	backoff := Backoff{
		initialDelay: 500 * time.Millisecond,
		maxDelay:     10 * time.Second,
		multiplier:   1.5,
		jitter:       1.0,
	}

	backoff.waitFunc = backoff.wait

	for _, opt := range opts {
		opt(&backoff)
	}

	return &backoff
}

// WithInitialDelay configures a Backoff such that the first retry is attempted after initialDelay.
// The actual delay used for each attempt may be shorter or longer due to jitter.
func WithInitialDelay(initialDelay time.Duration) Opt {
	if initialDelay <= 0 {
		panic("initialDelay must be >0")
	}

	return func(backoff *Backoff) {
		backoff.initialDelay = initialDelay
	}
}

// WithMaxDelay configures a Backoff such that the delay between retries will be no longer than maxDelay.
func WithMaxDelay(maxDelay time.Duration) Opt {
	if maxDelay <= 0 {
		panic("maxDelay must be >0")
	}

	return func(backoff *Backoff) {
		backoff.maxDelay = maxDelay
	}
}

// WithMultiplier configures a Backoff such that the delay between retries will be multiplied by multiplier before each attempt.
// The actual delay used for each attempt may be shorter or longer due to jitter.
func WithMultiplier(multiplier float64) Opt {
	if multiplier < 1.0 {
		panic("multiplier must be >=1.0")
	}

	return func(backoff *Backoff) {
		backoff.multiplier = multiplier
	}
}

// WithJitter configures a Backoff such that the delay between retries will be jittered randomly.
// jitter must be >=0.0 and <=1.0. If jitter==0.0, jitter is turned off.
func WithJitter(jitter float64) Opt {
	if jitter < 0.0 || jitter > 1.0 {
		panic("jitter must be >=0.0 and <=1.0")
	}

	return func(backoff *Backoff) {
		backoff.jitter = jitter
	}
}

// Do executes fun repeatedly until it returns a nil error, the retry loop is aborted explicitly,
// or if the maximum number of attempts is reached.
//
// If fun returns a nil error, Do stops and returns. The Backoff is reset such that the next call
// to a RetryableFunc is not delayed.
// If fun returns an AbortError or context.Canceled, Do stops and returns the error.
// If fun returns any other error, a retry will be attempted after a delay.
// If the maximum number of attempts is reached, Do stops and returns a MaxAttemptsError.
//
// Do is safe to call concurrently. All attempts to call RetryableFuncs will share the same delay mechanism.
func (b *Backoff) Do(ctx context.Context, fun RetryableFunc, maxAttempts int) error {
	if maxAttempts <= 0 {
		panic("maxAttempts must be >0")
	}

	var lastErr error

	for attempt := 1; ; attempt++ {
		if attempt > maxAttempts {
			return &MaxAttemptsError{
				Err: lastErr,
			}
		}

		ctx := withAttempt(ctx, attempt)

		if err := b.waitFunc(ctx); err != nil {
			return err
		}

		err := fun(ctx)
		lastErr = err

		if err == nil {
			b.reset()

			return nil
		}

		b.fail()

		if _, ok := err.(*AbortError); ok { //nolint:errorlint // must be *AbortError
			return err
		}

		if errors.Is(err, context.Canceled) {
			return err
		}

		continue
	}
}

func (b *Backoff) wait(ctx context.Context) error {
	b.mu.Lock()
	nextDelay := b.nextDelay
	b.mu.Unlock()

	if nextDelay == 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			return nil
		}
	}

	timer := time.NewTimer(nextDelay)

	select {
	case <-timer.C:
		return nil

	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}

		return ctx.Err()
	}
}

func (b *Backoff) fail() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.nextBaseDelay == 0 {
		b.nextBaseDelay = b.initialDelay
	} else {
		b.nextBaseDelay = time.Duration(float64(b.nextBaseDelay) * b.multiplier)
	}

	b.nextBaseDelay = min(b.nextBaseDelay, b.maxDelay)

	b.nextDelay = b.nextBaseDelay

	if b.jitter > 0.0 {
		b.nextDelay -= b.nextBaseDelay / 2
		b.nextDelay += time.Duration(rand.Float64() * float64(b.nextBaseDelay)) //nolint:gosec // no need for cryptographically secure random number
	}

	b.nextDelay = min(b.nextDelay, b.maxDelay)
}

func (b *Backoff) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextBaseDelay = 0
	b.nextDelay = 0
}

// Error implements error.
func (e *AbortError) Error() string {
	return "retry abort: " + e.Err.Error()
}

// Error implements error.
func (e *MaxAttemptsError) Error() string {
	if e.Err == nil {
		return "max attempts reached"
	}

	return "max attempts reached: " + e.Err.Error()
}

func withAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptContextKey, attempt)
}

// AttemptFromContext returns the current attempt number from ctx (1-based).
// If ctx does not contain an attempt number, 0 is returned.
func AttemptFromContext(ctx context.Context) int {
	attempt, ok := ctx.Value(attemptContextKey).(int)
	if !ok {
		return 0
	}

	return attempt
}
