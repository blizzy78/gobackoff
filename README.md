[![GoDoc](https://pkg.go.dev/badge/github.com/blizzy78/gobackoff)](https://pkg.go.dev/github.com/blizzy78/gobackoff)


gobackoff
=========

A Go package that provides a backoff algorithm for retrying operations.

```go
import "github.com/blizzy78/gobackoff"
```


Code example
------------

```go
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
```


License
-------

This package is licensed under the MIT license.
