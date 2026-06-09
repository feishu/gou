package v8

import "sync"

type runnerExecutionLifecycle struct {
	mu       sync.Mutex
	active   int
	retired  bool
	inactive chan struct{}
}

func newRunnerExecutionLifecycle() *runnerExecutionLifecycle {
	inactive := make(chan struct{})
	close(inactive)
	return &runnerExecutionLifecycle{inactive: inactive}
}

func (lifecycle *runnerExecutionLifecycle) acquire() func() {
	if lifecycle == nil {
		return func() {}
	}
	lifecycle.mu.Lock()
	if lifecycle.active == 0 {
		lifecycle.inactive = make(chan struct{})
	}
	lifecycle.active++
	lifecycle.mu.Unlock()

	return lifecycle.release
}

func (lifecycle *runnerExecutionLifecycle) release() {
	if lifecycle == nil {
		return
	}
	lifecycle.mu.Lock()
	if lifecycle.active > 0 {
		lifecycle.active--
		if lifecycle.active == 0 {
			close(lifecycle.inactive)
		}
	}
	lifecycle.mu.Unlock()
}

func (lifecycle *runnerExecutionLifecycle) retire() {
	if lifecycle == nil {
		return
	}
	lifecycle.mu.Lock()
	lifecycle.retired = true
	lifecycle.mu.Unlock()
}

func (lifecycle *runnerExecutionLifecycle) reusable() bool {
	if lifecycle == nil {
		return true
	}
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return !lifecycle.retired
}

func (lifecycle *runnerExecutionLifecycle) waitInactive() {
	if lifecycle == nil {
		return
	}
	lifecycle.mu.Lock()
	inactive := lifecycle.inactive
	lifecycle.mu.Unlock()
	<-inactive
}
