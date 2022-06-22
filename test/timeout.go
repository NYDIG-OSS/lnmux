package test

import (
	"os"
	"runtime/pprof"
	"time"
)

const testTimeout = 30 * time.Second

// Timeout implements a test level timeout.
func Timeout() func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(testTimeout):
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)

			panic("test timeout")
		case <-done:
		}
	}()

	return func() {
		close(done)
	}
}
