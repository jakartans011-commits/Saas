package observability

import (
	"sync"
	"testing"
)

// TestCaptureError_NoDataRaceOnReusedBuffer simulates the exact failure
// mode described in the DATA RACE FIX comment in captureMessage: fiber/
// fasthttp reuses a pooled []byte buffer for the NEXT request the instant
// the current handler returns. If CaptureError's goroutine reads that
// buffer's string view after it's been overwritten, `go test -race`
// flags it.
func TestCaptureError_NoDataRaceOnReusedBuffer(t *testing.T) {
	InitWithDSN("https://public@example.invalid/1")
	defer InitWithDSN("")

	var wg sync.WaitGroup
	iterations := 200

	for i := 0; i < iterations; i++ {
		buf := []byte("/api/v1/route")

		path := string(buf)
		tags := map[string]string{"route": path}

		wg.Add(1)
		go func() {
			defer wg.Done()
			CaptureError(errUhOh, tags)
		}()

		for j := range buf {
			buf[j] = 'X'
		}
	}

	wg.Wait()
}

var errUhOh = &staticError{"uh oh"}

type staticError struct{ msg string }

func (e *staticError) Error() string { return e.msg }