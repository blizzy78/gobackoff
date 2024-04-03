package gobackoff_test

import (
	"context"
	"fmt"
	"io"

	"github.com/blizzy78/gobackoff"
)

func Example() {
	const maxAttempts = 5

	backoff := gobackoff.New( /* ... options ... */ )

	_ = backoff.Do(context.Background(), func(ctx context.Context) error {
		attempt := gobackoff.AttemptFromContext(ctx)
		if attempt == 1 {
			// simulate error
			return io.EOF
		}

		fmt.Printf("success after %d attempts\n", attempt)

		return nil
	}, maxAttempts)

	// Output: success after 2 attempts
}
