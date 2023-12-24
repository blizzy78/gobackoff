package gobackoff

import (
	"context"
	"fmt"
	"io"
)

func Example() {
	const maxAttempts = 5

	backoff := New( /* ... options ... */ )

	_ = backoff.Do(context.Background(), func(ctx context.Context) error {
		attempt := AttemptFromContext(ctx)
		if attempt == 1 {
			// simulate error
			return io.EOF
		}

		fmt.Printf("success after %d attempts\n", attempt)

		return nil
	}, maxAttempts)

	// Output: success after 2 attempts
}
