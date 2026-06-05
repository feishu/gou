package v8

import (
	"fmt"
	"sync"
	"time"

	"github.com/yaoapp/kun/log"
)

// Dispatcher is a runner dispatcher
type Dispatcher struct {
	mu          sync.Mutex
	availables  chan *Runner
	health      *Health
	total       uint
	min         uint
	max         uint
	state       dispatcherState
	stop        chan struct{}
	stopped     chan struct{}
	capacity    chan struct{}
	starting    bool
	started     bool
	startDone   chan struct{}
	startErr    error
	scaling     bool
	stoppedDone bool
	stats       DispatcherStats
}

// Health is the health check
type Health struct {
	missing uint // the missing runners
	total   uint // the total runners
}

// the global dispatcher instance
// initialize when the v8 start
var dispatcher *Dispatcher = nil

// NewDispatcher is a runner dispatcher
func NewDispatcher(min, max uint) *Dispatcher {

	// Test the min and max
	// min = 10
	// max = 200
	return &Dispatcher{
		availables: make(chan *Runner, max),
		health:     &Health{missing: 0, total: 0},
		total:      0,
		min:        min,
		max:        max,
		state:      dispatcherRunning,
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
		capacity:   make(chan struct{}, 1),
	}
}

// Stats 返回 Dispatcher 统计快照
func (dispatcher *Dispatcher) Stats() DispatcherStats {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	stats := dispatcher.stats
	stats.Active = uint64(dispatcher.total)
	stats.Idle = uint64(len(dispatcher.availables))
	if stats.Active >= stats.Idle {
		stats.Leased = stats.Active - stats.Idle
	} else {
		stats.Leased = 0
	}
	return stats
}

// Start start the v8 mannager
func (dispatcher *Dispatcher) Start() (err error) {
	dispatcher.mu.Lock()
	if dispatcher.state != dispatcherRunning {
		err := dispatcher.stateErrorLocked()
		dispatcher.mu.Unlock()
		return err
	}
	if dispatcher.started {
		dispatcher.mu.Unlock()
		return nil
	}
	if dispatcher.starting {
		done := dispatcher.startDone
		dispatcher.mu.Unlock()
		<-done

		dispatcher.mu.Lock()
		defer dispatcher.mu.Unlock()
		if dispatcher.startErr != nil {
			return dispatcher.startErr
		}
		if dispatcher.state != dispatcherRunning {
			return dispatcher.stateErrorLocked()
		}
		return nil
	}

	dispatcher.starting = true
	dispatcher.startErr = nil
	dispatcher.startDone = make(chan struct{})
	done := dispatcher.startDone
	dispatcher.mu.Unlock()

	defer func() {
		dispatcher.mu.Lock()
		dispatcher.startErr = err
		dispatcher.starting = false
		close(done)
		dispatcher.mu.Unlock()
	}()

	for i := uint(0); i < dispatcher.min; i++ {
		if err := dispatcher.create(); err != nil {
			dispatcher.Stop()
			return err
		}
	}

	dispatcher.mu.Lock()
	if dispatcher.state == dispatcherRunning && !dispatcher.scaling {
		dispatcher.started = true
		dispatcher.scaling = true
		go dispatcher.scalingLoop()
	}
	if dispatcher.state != dispatcherRunning {
		err = dispatcher.stateErrorLocked()
		dispatcher.mu.Unlock()
		return err
	}
	total := dispatcher.total
	dispatcher.mu.Unlock()

	log.Trace("[dispatcher] the dispatcher is started. runners %d", total)
	return nil
}

// Stop stop the v8 mannager
func (dispatcher *Dispatcher) Stop() {
	dispatcher.mu.Lock()
	switch dispatcher.state {
	case dispatcherClosed:
		dispatcher.mu.Unlock()
		return
	case dispatcherRunning:
		dispatcher.state = dispatcherClosing
		close(dispatcher.stop)
	}
	waitScaling := dispatcher.scaling
	stopped := dispatcher.stopped
	dispatcher.mu.Unlock()

	if waitScaling {
		<-stopped
	}

	for _, runner := range dispatcher.drainIdleRunners() {
		runner.Destroy(nil)
		runner.waitDestroyed(time.Second)
	}

	dispatcher.mu.Lock()
	dispatcher.state = dispatcherClosed
	dispatcher.mu.Unlock()
}

func (dispatcher *Dispatcher) create() error {
	return dispatcher.createWithTimeout(0)
}

func (dispatcher *Dispatcher) createWithTimeout(timeout time.Duration) error {
	dispatcher.mu.Lock()
	if dispatcher.state != dispatcherRunning {
		err := dispatcher.stateErrorLocked()
		dispatcher.mu.Unlock()
		return err
	}
	if dispatcher.total >= dispatcher.max {
		err := fmt.Errorf("[dispatcher] the runner is max. availables:%d, total:%d, %s", len(dispatcher.availables), dispatcher.total, dispatcher.health)
		dispatcher.mu.Unlock()
		log.Error(err.Error())
		return err
	}
	dispatcher.total++
	dispatcher.mu.Unlock()

	runner := NewRunner(true, dispatcher)
	ready := make(chan error, 1)

	go runner.Start(ready)
	select {
	case err := <-ready:
		if err != nil {
			dispatcher.runnerDestroyed(false)
			return err
		}

	case <-dispatcher.stop:
		go dispatcher.destroyCreatedRunner(runner, ready)
		return dispatcher.stateError()

	case <-dispatcher.createTimeout(timeout):
		go dispatcher.destroyCreatedRunner(runner, ready)
		return fmt.Errorf("[dispatcher] create timeout %v", timeout)
	}

	if !dispatcher.release(runner, true) {
		runner.waitDestroyed(time.Second)
		if !dispatcher.isRunning() {
			return dispatcher.stateError()
		}
		return fmt.Errorf("[dispatcher] runner release failed")
	}

	dispatcher.mu.Lock()
	dispatcher.stats.Created++
	total := dispatcher.total
	available := len(dispatcher.availables)
	health := dispatcher.health.String()
	dispatcher.mu.Unlock()
	log.Trace("[dispatcher] [%s] runner create. availables:%d, total:%d, %s", runner.id, available, total, health)
	return nil
}

func (dispatcher *Dispatcher) createTimeout(timeout time.Duration) <-chan time.Time {
	if timeout <= 0 {
		return nil
	}

	return time.After(timeout)
}

func (dispatcher *Dispatcher) destroyCreatedRunner(runner *Runner, ready <-chan error) {
	if err := <-ready; err != nil {
		dispatcher.runnerDestroyed(false)
		return
	}

	if runner != nil {
		runner.Destroy(nil)
		runner.waitDestroyed(time.Second)
	}
}

func dispatcherSelectTimeout(timeout time.Duration) error {
	return fmt.Errorf("[dispatcher] select timeout %v", timeout)
}

// Select select a free v8 runner
func (dispatcher *Dispatcher) Select(timeout time.Duration) (*Runner, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	recheck := time.NewTicker(10 * time.Millisecond)
	defer recheck.Stop()
	deadline := time.Now().Add(timeout)

	if err := dispatcher.recordSelect(); err != nil {
		return nil, err
	}

	for {
		if time.Until(deadline) <= 0 {
			dispatcher.timeoutCount()
			return nil, dispatcherSelectTimeout(timeout)
		}

		runner, err := dispatcher.tryTakeAvailable()
		if err != nil {
			return nil, err
		}
		if runner != nil {
			if err := dispatcher.claimRunner(runner); err != nil {
				runner.Destroy(nil)
				return nil, err
			}
			dispatcher.logSelectedRunner(runner)
			return runner, nil
		}

		if !dispatcher.isRunning() {
			return nil, dispatcher.stateError()
		}

		if dispatcher.canCreate() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				dispatcher.timeoutCount()
				return nil, dispatcherSelectTimeout(timeout)
			}
			dispatcher.missingCount()
			if err := dispatcher.createWithTimeout(remaining); err != nil {
				if !dispatcher.isRunning() {
					return nil, dispatcher.stateError()
				}
				if time.Until(deadline) <= 0 {
					dispatcher.timeoutCount()
					return nil, dispatcherSelectTimeout(timeout)
				}
			}
			continue
		}

		select {
		case runner := <-dispatcher.availables:
			if err := dispatcher.claimRunner(runner); err != nil {
				if runner != nil {
					runner.Destroy(nil)
				}
				return nil, err
			}
			dispatcher.logSelectedRunner(runner)
			return runner, nil

		case <-timer.C:
			dispatcher.timeoutCount()
			return nil, dispatcherSelectTimeout(timeout)

		case <-recheck.C:
			continue

		case <-dispatcher.capacity:
			continue

		case <-dispatcher.stop:
			return nil, dispatcher.stateError()
		}
	}

}

// Scaling scale the v8 runners, check every 10 seconds
// Release the v8 runners if the free runners are more than max size
// Create a new v8 runner if the free runners are less than min size
func (dispatcher *Dispatcher) Scaling() {
	dispatcher.mu.Lock()
	if dispatcher.scaling || dispatcher.state != dispatcherRunning {
		dispatcher.mu.Unlock()
		return
	}
	dispatcher.scaling = true
	dispatcher.mu.Unlock()

	dispatcher.scalingLoop()
}

func (dispatcher *Dispatcher) scalingLoop() {

	log.Info("[dispatcher] the dispatcher is scaling. min %d, max %d", dispatcher.min, dispatcher.max)
	defer dispatcher.finishScaling()

	// check the free runners every 30 seconds
	// @todo: release the free runners if the free runners are more than min size
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-dispatcher.stop:
			return

		case <-ticker.C:
			dispatcher.scaleOnce()
		}
	}
}

func (dispatcher *Dispatcher) finishScaling() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.scaling = false
	if !dispatcher.stoppedDone {
		close(dispatcher.stopped)
		dispatcher.stoppedDone = true
	}
}

func (dispatcher *Dispatcher) missingCount() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.health.missing = dispatcher.health.missing + 1
}

func (dispatcher *Dispatcher) timeoutCount() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.stats.Timeouts++
}

func (dispatcher *Dispatcher) healthEvictionCount() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.stats.HealthEvictions++
}

func (dispatcher *Dispatcher) release(runner *Runner, reusable bool) bool {
	if runner == nil {
		return false
	}

	dispatcher.mu.Lock()
	if dispatcher.state == dispatcherRunning && reusable {
		select {
		case dispatcher.availables <- runner:
			available := len(dispatcher.availables)
			total := dispatcher.total
			health := dispatcher.health.String()
			dispatcher.mu.Unlock()
			log.Trace("[dispatcher] [%s] runner online. availables:%d, total:%d, %s", runner.id, available, total, health)
			return true
		default:
		}
	}
	dispatcher.mu.Unlock()

	runner.Destroy(nil)
	return false
}

func (dispatcher *Dispatcher) runnerDestroyed(countStats bool) {
	dispatcher.mu.Lock()
	if dispatcher.total > 0 {
		dispatcher.total--
	}
	if countStats {
		dispatcher.stats.Destroyed++
	}
	dispatcher.mu.Unlock()
	dispatcher.notifyCapacityChanged()
}

func (dispatcher *Dispatcher) notifyCapacityChanged() {
	select {
	case dispatcher.capacity <- struct{}{}:
	default:
	}
}

func (dispatcher *Dispatcher) canCreate() bool {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	return dispatcher.state == dispatcherRunning && dispatcher.total < dispatcher.max
}

func (dispatcher *Dispatcher) recordSelect() error {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.state == dispatcherRunning {
		dispatcher.health.total++
		return nil
	}
	return dispatcher.stateErrorLocked()
}

func (dispatcher *Dispatcher) isRunning() bool {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	return dispatcher.state == dispatcherRunning
}

func (dispatcher *Dispatcher) tryTakeAvailable() (*Runner, error) {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.state != dispatcherRunning {
		return nil, dispatcher.stateErrorLocked()
	}

	select {
	case runner := <-dispatcher.availables:
		return runner, nil
	default:
		return nil, nil
	}
}

func (dispatcher *Dispatcher) claimRunner(runner *Runner) error {
	if runner == nil {
		return fmt.Errorf("[dispatcher] selected empty runner")
	}

	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.state == dispatcherRunning {
		return nil
	}
	return dispatcher.stateErrorLocked()
}

func (dispatcher *Dispatcher) stateError() error {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	return dispatcher.stateErrorLocked()
}

func (dispatcher *Dispatcher) stateErrorLocked() error {
	switch dispatcher.state {
	case dispatcherClosing:
		return fmt.Errorf("[dispatcher] dispatcher is closing")
	case dispatcherClosed:
		return fmt.Errorf("[dispatcher] dispatcher is closed")
	default:
		return fmt.Errorf("[dispatcher] dispatcher is not running")
	}
}

func (dispatcher *Dispatcher) logSelectedRunner(runner *Runner) {
	dispatcher.mu.Lock()
	available := len(dispatcher.availables)
	dispatcher.mu.Unlock()

	log.Debug(fmt.Sprintf("--- [%s] -----------------", runner.id))
	log.Debug(fmt.Sprintf("1.  [%s] Select a free v8 runner. availables=%d", runner.id, available))
}

func (dispatcher *Dispatcher) drainIdleRunners() []*Runner {
	runners := []*Runner{}
	for {
		select {
		case runner := <-dispatcher.availables:
			if runner != nil {
				runners = append(runners, runner)
			}
		default:
			return runners
		}
	}
}

func (dispatcher *Dispatcher) scaleOnce() {
	dispatcher.mu.Lock()
	if dispatcher.state != dispatcherRunning || dispatcher.health.total == 0 {
		dispatcher.health.Reset()
		dispatcher.mu.Unlock()
		return
	}

	percent := float64(dispatcher.health.missing) / float64(dispatcher.health.total)
	missing := dispatcher.max - dispatcher.total
	dispatcher.health.Reset()
	dispatcher.mu.Unlock()

	log.Trace("[dispatcher] the health percent is %f", percent)
	if percent <= 0.2 {
		return
	}

	for i := uint(0); i < missing; i++ {
		if err := dispatcher.create(); err != nil {
			return
		}
	}
}

func (health *Health) String() string {
	return fmt.Sprintf("missing:%d, total:%d", health.missing, health.total)
}

// Reset reset the health
func (health *Health) Reset() {
	health.missing = 0
	health.total = 0
}
