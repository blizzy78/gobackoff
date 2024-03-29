package gobackoff

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/matryer/is"
)

func TestBackoff(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	attempts := 0

	err := backoff.Do(context.Background(), func(_ context.Context) error {
		attempts++

		if attempts < 5 {
			return errors.New("test") //nolint:goerr113 // dynamic error is okay here
		}

		return nil
	}, 10)

	is.NoErr(err)
	is.Equal(attempts, 5)
}

func TestBackoff_Timeout(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	attempts := 0

	err := backoff.Do(context.Background(), func(ctx context.Context) error {
		attempts++

		longRunning := func(ctx context.Context) error {
			timer := time.NewTimer(100 * time.Millisecond)

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

		ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()

		return longRunning(ctx)
	}, 5)

	maxErr, ok := err.(*MaxAttemptsError) //nolint:errorlint // must be *MaxAttemptsError
	is.True(ok)
	is.True(errors.Is(maxErr.Err, context.DeadlineExceeded))

	is.Equal(attempts, 5)
}

func TestBackoff_Cancel(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	attempts := 0

	err := backoff.Do(context.Background(), func(ctx context.Context) error {
		attempts++

		longRunning := func(_ context.Context) error {
			return context.Canceled
		}

		return longRunning(ctx)
	}, 5)

	is.True(errors.Is(err, context.Canceled))

	is.Equal(attempts, 1)
}

func TestBackoff_MaxAttempts(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	testErr := errors.New("test") //nolint:goerr113 // dynamic error is okay here

	err := backoff.Do(context.Background(), func(_ context.Context) error {
		return testErr
	}, 10)

	maxErr, ok := err.(*MaxAttemptsError) //nolint:errorlint // must be *MaxAttemptsError
	is.True(ok)
	is.Equal(maxErr.Err, testErr)
}

func TestBackoff_Abort(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	testErr := errors.New("test") //nolint:goerr113 // dynamic error is okay here

	err := backoff.Do(context.Background(), func(_ context.Context) error {
		return &AbortError{
			Err: testErr,
		}
	}, 10)

	abortErr, ok := err.(*AbortError) //nolint:errorlint // must be *AbortError
	is.True(ok)
	is.Equal(abortErr.Err, testErr)
}

func TestBackoff_Delay(t *testing.T) {
	const (
		initialDelay = 500 * time.Millisecond
		maxDelay     = 10 * time.Second
		multiplier   = 1.5
		jitter       = 1.0
	)

	is := is.New(t)

	backoff := New(WithInitialDelay(initialDelay), WithMaxDelay(maxDelay), WithMultiplier(multiplier), WithJitter(jitter))
	backoff.waitFunc = noWait

	baseDelay := time.Duration(0)
	attempts := 0
	failures := 0

	_ = backoff.Do(context.Background(), func(_ context.Context) error {
		attempts++

		if failures == 0 {
			is.Equal(backoff.nextDelay, time.Duration(0))
		} else {
			if baseDelay == 0 {
				baseDelay = initialDelay
			} else {
				baseDelay = time.Duration(float64(baseDelay) * multiplier)
			}

			baseDelay = min(baseDelay, maxDelay)

			wantMinDelay := baseDelay - baseDelay/2
			wantMinDelay = min(wantMinDelay, maxDelay)

			wantMaxDelay := baseDelay + baseDelay/2
			wantMaxDelay = min(wantMaxDelay, maxDelay)

			is.True(backoff.nextDelay >= wantMinDelay)
			is.True(backoff.nextDelay <= wantMaxDelay)
		}

		failures++

		return errors.New("test") //nolint:goerr113 // dynamic error is okay here
	}, 100)
}

func TestBackoff_Reset(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	attempts := 0

	_ = backoff.Do(context.Background(), func(_ context.Context) error {
		attempts++

		if attempts < 5 {
			return errors.New("test") //nolint:goerr113 // dynamic error is okay here
		}

		return nil
	}, 10)

	is.Equal(backoff.nextDelay, time.Duration(0))
}

func TestAttemptFromContext(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.waitFunc = noWait

	testErr := errors.New("test") //nolint:goerr113 // dynamic error is okay here

	attempt := 0

	_ = backoff.Do(context.Background(), func(ctx context.Context) error {
		attempt++

		is.Equal(AttemptFromContext(ctx), attempt)

		return testErr
	}, 10)
}

func TestWait(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.nextDelay = 20 * time.Millisecond

	start := time.Now()

	_ = backoff.wait(context.Background())

	is.True(time.Since(start) >= 20*time.Millisecond)
}

func TestWait_Reset(t *testing.T) {
	is := is.New(t)

	backoff := New()

	start := time.Now()

	_ = backoff.wait(context.Background())

	is.True(time.Since(start) < 10*time.Millisecond)
}

func TestWait_Timeout(t *testing.T) {
	is := is.New(t)

	backoff := New()
	backoff.nextDelay = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()

	err := backoff.wait(ctx)

	is.True(errors.Is(err, context.DeadlineExceeded))
	is.True(time.Since(start) < 20*time.Millisecond)
}

func TestWait_CancelBefore(t *testing.T) {
	is := is.New(t)

	backoff := New()

	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	start := time.Now()

	err := backoff.wait(ctx)

	is.True(errors.Is(err, context.Canceled))
	is.True(time.Since(start) < 10*time.Millisecond)
}

func noWait(_ context.Context) error {
	return nil
}
