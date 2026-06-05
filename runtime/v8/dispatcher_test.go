package v8

import (
	"sync"
	"testing"
	"time"
)

func cleanupDispatcherForTest(t *testing.T) {
	t.Helper()

	if dispatcher != nil {
		destroyIdleDispatcherRunners()
		waitForDispatcherTotal(t, 0)
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
