package v8

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cleanupDispatcherForTest(t *testing.T) {
	t.Helper()

	if dispatcher != nil {
		Stop()
		waitForDispatcherTotal(t, 0)
		return
	}
	Stop()
}

func destroyIdleDispatcherRunners() {
	for {
		select {
		case runner, ok := <-dispatcher.availables:
			if !ok {
				return
			}
			if runner != nil {
				runner.Destroy(nil)
			}
		default:
			return
		}
	}
}

func destroyLeasedDispatcherRunners(runners []*Runner) {
	for _, runner := range runners {
		if runner != nil {
			runner.Destroy(nil)
		}
	}
}

func waitForDispatcherTotal(t *testing.T, expected uint) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if dispatcher.Stats().Active == uint64(expected) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("expected dispatcher total %d, got %d", expected, dispatcher.Stats().Active)
}

func waitForDispatcherStats(t *testing.T, match func(DispatcherStats) bool) DispatcherStats {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stats := dispatcher.Stats()
		if match(stats) {
			return stats
		}
		time.Sleep(time.Millisecond)
	}
	stats := dispatcher.Stats()
	t.Fatalf("dispatcher stats did not reach expected state: %+v", stats)
	return stats
}

func TestDispatcherStartPrewarmsMinRunners(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 2
	option.MaxSize = 3
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	if dispatcher == nil {
		t.Fatal("dispatcher should be initialized")
	}
	if got := len(dispatcher.availables); got != 2 {
		t.Fatalf("expected 2 idle runners, got %d", got)
	}
}

func TestDispatcherStartFailureDoesNotMarkStarted(t *testing.T) {
	dispatcher := NewDispatcher(1, 0)

	if err := dispatcher.Start(); err == nil {
		t.Fatal("expected Start to fail when max size is zero")
	}
	if dispatcher.started {
		t.Fatal("dispatcher should not be marked started after prewarm failure")
	}
	if err := dispatcher.Start(); err == nil {
		t.Fatal("expected repeated Start to fail after prewarm failure")
	}
}

func TestDispatcherSelectDoesNotCreateMoreThanMax(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 2
	option.DefaultTimeout = 250
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	var wg sync.WaitGroup
	start := make(chan struct{})
	leases := make(chan *Runner, 4)
	errs := make(chan error, 4)

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			runner, err := dispatcher.Select(250 * time.Millisecond)
			if err != nil {
				errs <- err
				return
			}
			leases <- runner
		}()
	}

	close(start)
	wg.Wait()
	close(leases)
	close(errs)

	successes := []*Runner{}
	for runner := range leases {
		successes = append(successes, runner)
	}
	defer destroyLeasedDispatcherRunners(successes)

	failures := []error{}
	for err := range errs {
		failures = append(failures, err)
	}

	if got := len(successes) + len(failures); got != 4 {
		t.Fatalf("expected 4 Select results, got %d", got)
	}
	if len(successes) < int(option.MaxSize) {
		t.Fatalf("expected at least %d successful leases, got %d successes and %d errors", option.MaxSize, len(successes), len(failures))
	}
	if len(successes) > int(option.MaxSize) {
		t.Fatalf("leased %d runners, max is %d", len(successes), option.MaxSize)
	}
	if active := dispatcher.Stats().Active; active > uint64(option.MaxSize) {
		t.Fatalf("created %d runners, max is %d", active, option.MaxSize)
	}
}

func TestDispatcherSelectCreatesAfterDestroyedRunnerFreesCapacity(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	leased, err := dispatcher.Select(100 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if leased == nil {
		t.Fatal("expected leased runner")
	}

	type selectResult struct {
		runner *Runner
		err    error
	}
	result := make(chan selectResult, 1)
	go func() {
		runner, err := dispatcher.Select(500 * time.Millisecond)
		result <- selectResult{runner: runner, err: err}
	}()

	select {
	case res := <-result:
		if res.runner != nil {
			res.runner.Destroy(nil)
		}
		t.Fatalf("Select returned before capacity was available: %v", res.err)
	case <-time.After(20 * time.Millisecond):
	}

	leased.Destroy(nil)
	if !leased.waitDestroyed(time.Second) {
		t.Fatal("leased runner was not destroyed")
	}

	select {
	case res := <-result:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.runner == nil {
			t.Fatal("expected replacement runner")
		}
		defer destroyLeasedDispatcherRunners([]*Runner{res.runner})

	case <-time.After(time.Second):
		t.Fatal("Select did not create a replacement runner after capacity was freed")
	}
}

func TestDispatcherScaleOnceShrinksIdleRunnersToMin(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 4
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	leased := []*Runner{}
	for i := uint(0); i < option.MaxSize; i++ {
		runner, err := dispatcher.Select(500 * time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		leased = append(leased, runner)
	}

	for _, runner := range leased {
		if !dispatcher.release(runner, true) {
			t.Fatalf("failed to release runner %s", runner.id)
		}
	}

	stats := waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.Active == uint64(option.MaxSize) && stats.Idle == uint64(option.MaxSize)
	})
	if stats.Leased != 0 {
		t.Fatalf("expected no leased runners before shrink, got %d", stats.Leased)
	}

	dispatcher.scaleOnce()

	stats = waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.Active == uint64(option.MinSize) && stats.Idle == uint64(option.MinSize)
	})
	if stats.Destroyed != uint64(option.MaxSize-option.MinSize) {
		t.Fatalf("expected %d destroyed runners, got %d", option.MaxSize-option.MinSize, stats.Destroyed)
	}
}

func TestDispatcherScaleOnceWaitsForRealDestroyBeforeCounting(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 2
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	runner1, err := dispatcher.Select(500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	runner2, err := dispatcher.Select(500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	releaseExecution := runner1.execution.acquire()
	defer releaseExecution()

	if !dispatcher.release(runner1, true) {
		t.Fatalf("failed to release runner %s", runner1.id)
	}
	if !dispatcher.release(runner2, true) {
		t.Fatalf("failed to release runner %s", runner2.id)
	}

	waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.Active == 2 && stats.Idle == 2
	})

	dispatcher.scaleOnce()

	if runner1.waitDestroyed(10 * time.Millisecond) {
		t.Fatal("runner should still be blocked before execution release")
	}

	stats := dispatcher.Stats()
	if stats.Active != 2 {
		t.Fatalf("expected active runners to stay at 2 before destroy completes, got %d", stats.Active)
	}
	if stats.Idle != 1 {
		t.Fatalf("expected one idle runner before destroy completes, got %d", stats.Idle)
	}
	if stats.Destroyed != 0 {
		t.Fatalf("expected destroyed count to stay at 0 before destroy completes, got %d", stats.Destroyed)
	}

	releaseExecution()
	if !runner1.waitDestroyed(time.Second) {
		t.Fatal("runner was not destroyed after execution release")
	}

	stats = waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.Active == 1 && stats.Idle == 1 && stats.Destroyed == 1
	})
	if stats.Leased != 0 {
		t.Fatalf("expected no leased runners after destroy completes, got %d", stats.Leased)
	}
}

func TestDispatcherScaleOnceIgnoresSmallSampleWindow(t *testing.T) {
	dispatcher := NewDispatcher(1, 20)
	originalStartRunner := startRunnerForDispatcher
	startRunnerForDispatcher = func(runner *Runner, ready chan error) {
		ready <- nil
	}
	defer func() {
		startRunnerForDispatcher = originalStartRunner
		for _, runner := range dispatcher.drainIdleRunners() {
			runner.Destroy(nil)
		}
	}()

	dispatcher.mu.Lock()
	dispatcher.total = 1
	dispatcher.health.missing = 1
	dispatcher.health.total = 1
	dispatcher.mu.Unlock()

	dispatcher.scaleOnce()

	stats := dispatcher.Stats()
	if stats.Active != 1 {
		t.Fatalf("expected small sample window to keep active runners at 1, got %d", stats.Active)
	}
	if stats.Created != 0 {
		t.Fatalf("expected no runners created from small sample window, got %d", stats.Created)
	}
}

func TestDispatcherScaleOnceGrowsGradually(t *testing.T) {
	dispatcher := NewDispatcher(1, 20)
	originalStartRunner := startRunnerForDispatcher
	startRunnerForDispatcher = func(runner *Runner, ready chan error) {
		ready <- nil
	}
	defer func() {
		startRunnerForDispatcher = originalStartRunner
		for _, runner := range dispatcher.drainIdleRunners() {
			runner.Destroy(nil)
		}
	}()

	dispatcher.mu.Lock()
	dispatcher.total = 1
	dispatcher.health.missing = 10
	dispatcher.health.total = 20
	dispatcher.mu.Unlock()

	dispatcher.scaleOnce()

	stats := dispatcher.Stats()
	if stats.Active != 2 {
		t.Fatalf("expected one gradual scale-up runner, got %d active runners", stats.Active)
	}
	if stats.Created != 1 {
		t.Fatalf("expected one runner created by gradual scale-up, got %d", stats.Created)
	}
}

func TestDispatcherScaleOnceGrowsByCurrentPoolRatio(t *testing.T) {
	dispatcher := NewDispatcher(10, 100)
	originalStartRunner := startRunnerForDispatcher
	startRunnerForDispatcher = func(runner *Runner, ready chan error) {
		ready <- nil
	}
	defer func() {
		startRunnerForDispatcher = originalStartRunner
		for _, runner := range dispatcher.drainIdleRunners() {
			runner.Destroy(nil)
		}
	}()

	dispatcher.mu.Lock()
	dispatcher.total = 50
	dispatcher.health.missing = 25
	dispatcher.health.total = 50
	dispatcher.mu.Unlock()

	dispatcher.scaleOnce()

	stats := dispatcher.Stats()
	if stats.Active != 60 {
		t.Fatalf("expected scale-up by 20 percent of current pool, got %d active runners", stats.Active)
	}
	if stats.Created != 10 {
		t.Fatalf("expected 10 runners created by ratio scale-up, got %d", stats.Created)
	}
}

func TestDispatcherSelectBacksOffAfterCreateFailure(t *testing.T) {
	dispatcher := NewDispatcher(0, 100)
	originalStartRunner := startRunnerForDispatcher
	var attempts int64
	startRunnerForDispatcher = func(runner *Runner, ready chan error) {
		atomic.AddInt64(&attempts, 1)
		ready <- errors.New("synthetic runner start failure")
	}
	defer func() { startRunnerForDispatcher = originalStartRunner }()

	runner, err := dispatcher.Select(35 * time.Millisecond)
	if runner != nil {
		runner.Destroy(nil)
		t.Fatalf("expected no runner after synthetic start failures, got %s", runner.id)
	}
	if err == nil {
		t.Fatal("expected Select to return an error")
	}

	if got := atomic.LoadInt64(&attempts); got > 5 {
		t.Fatalf("create attempts = %d, want at most 5 within 35ms timeout", got)
	}
}

func TestDispatcherSelectTimeoutIncludesStats(t *testing.T) {
	dispatcher := NewDispatcher(0, 1)
	dispatcher.mu.Lock()
	dispatcher.total = 1
	dispatcher.mu.Unlock()

	runner, err := dispatcher.Select(5 * time.Millisecond)
	if runner != nil {
		runner.Destroy(nil)
		t.Fatalf("expected no runner, got %s", runner.id)
	}
	if err == nil {
		t.Fatal("expected Select to timeout")
	}

	message := err.Error()
	for _, want := range []string{"select timeout", "active:1", "idle:0", "leased:1", "max:1"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected timeout error to contain %q, got %q", want, message)
		}
	}
}

func TestDispatcherStopRejectsNewSelect(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 20
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	Stop()

	runner, err := dispatcher.Select(20 * time.Millisecond)
	if runner != nil {
		runner.Destroy(nil)
		waitForDispatcherTotal(t, 0)
		t.Fatalf("expected no runner after Stop, got %s", runner.id)
	}
	if err == nil {
		t.Fatal("expected Select to fail after Stop")
	}
}

func TestRunnerResetEvictsUnhealthyRunner(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	originalHealthChecker := runnerHealthChecker
	runnerHealthChecker = func(*Runner) bool { return false }
	defer func() { runnerHealthChecker = originalHealthChecker }()

	runner, err := dispatcher.Select(100 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	oldID := runner.id

	runner.Reset()
	if !runner.waitDestroyed(time.Second) {
		t.Fatal("unhealthy runner was not destroyed")
	}

	stats := waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.HealthEvictions == 1 && stats.Destroyed == 1 && stats.Active == 0
	})
	if stats.Idle != 0 {
		t.Fatalf("expected no idle runner after eviction, got %d", stats.Idle)
	}

	runnerHealthChecker = originalHealthChecker
	replacement, err := dispatcher.Select(500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer destroyLeasedDispatcherRunners([]*Runner{replacement})

	if replacement.id == oldID {
		t.Fatal("expected unhealthy runner to be replaced")
	}
	if active := dispatcher.Stats().Active; active > uint64(option.MaxSize) {
		t.Fatalf("created %d runners, max is %d", active, option.MaxSize)
	}
}
