package v8

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDispatcherSelectContextCancelled(t *testing.T) {
	option := runtimeOption
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 500

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	// Pre-cancelled context should return immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner, err := dispatcher.SelectContext(ctx, 100*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if runner != nil {
		runner.Destroy(nil)
		t.Fatal("expected nil runner on cancelled context")
	}

	// Normal select works
	runner, err = dispatcher.SelectContext(context.Background(), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}

	// Now pool is exhausted (MaxSize=1, leased=1)
	// Waiting with a 20ms deadline context should return deadline exceeded well before 500ms
	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelTimeout()

	start := time.Now()
	waitingRunner, err := dispatcher.SelectContext(ctxTimeout, 500*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context deadline error, got: %v", err)
	}
	if waitingRunner != nil {
		waitingRunner.Destroy(nil)
		t.Fatal("expected nil runner on cancelled wait")
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("SelectContext took too long to cancel: %v (expected ~20ms)", elapsed)
	}

	// Clean up leased runner
	runner.Destroy(nil)
}
