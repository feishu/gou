package v8

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yaoapp/gou/runtime/v8/bridge"
	"github.com/yaoapp/gou/runtime/v8/objects/console"
	"github.com/yaoapp/kun/log"
	"rogchap.com/v8go"
)

// ID is the runner id
type ID string

func (id ID) String() string {
	return strings.Split(string(id), "-")[0]
}

// Runner is the v8 runner
type Runner struct {
	mu          sync.Mutex
	id          ID
	iso         *v8go.Isolate
	ctx         *v8go.Context
	tmpl        *v8go.ObjectTemplate
	inspector   *v8go.Inspector
	debugTarget *debugTarget
	status      uint8
	closed      bool
	dispatcher  *Dispatcher
	signal      chan uint8
	destroyed   chan struct{}
	chResp      chan interface{}
	keepalive   bool
	script      *Script
	method      string
	sid         string
	args        []interface{}
	global      map[string]interface{}
	invocation  runnerInvocation
	caches      map[string]*v8go.Object
}

var runnerHealthChecker = func(runner *Runner) bool {
	return runner.health()
}

const (
	// RunnerStatusInit is the runner status init
	RunnerStatusInit uint8 = iota

	// RunnerStatusRunning is the runner status running
	RunnerStatusRunning

	// RunnerStatusCleaning is the runner status cleaning
	RunnerStatusCleaning

	// RunnerStatusReady is the runner status ready
	RunnerStatusReady

	// RunnerStatusDestroy is the runner status destroy
	RunnerStatusDestroy

	// RunnerCommandDestroy is the runner command destroy
	RunnerCommandDestroy

	// RunnerCommandReset is the runner command reset
	RunnerCommandReset

	// RunnerCommandExec is the runner command exec
	RunnerCommandExec

	// RunnerCommandStatus is the runner command status
	RunnerCommandStatus
)

// NewRunner create a new v8 runner
func NewRunner(keepalive bool, owner *Dispatcher) *Runner {
	return &Runner{
		id:         ID(uuid.New().String()),
		iso:        nil,
		ctx:        nil,
		dispatcher: owner,
		signal:     make(chan uint8, 2),
		destroyed:  make(chan struct{}),
		keepalive:  keepalive,
		status:     RunnerStatusInit,
	}
}

// Start start the v8 runner
func (runner *Runner) Start(ready chan error) error {
	runner.mu.Lock()
	if runner.status != RunnerStatusInit {
		runner.mu.Unlock()
		err := fmt.Errorf("[runner] you can't start a runner with status: [%d]", runner.status)
		log.Error(err.Error())
		ready <- err
		return err
	}
	runner.closed = false
	runner.mu.Unlock()

	iso := v8go.YaoNewIsolate()
	tmpl := MakeTemplate(iso)
	ctx := v8go.NewContext(iso, tmpl)
	var inspector *v8go.Inspector
	if runtimeOption.Inspect.Enabled {
		var err error
		inspector, err = v8go.NewInspector(iso)
		if err != nil {
			ctx.Close()
			iso.Dispose()
			ready <- err
			return err
		}
	}
	runner.mu.Lock()
	runner.iso = iso
	runner.ctx = ctx
	runner.tmpl = tmpl
	runner.inspector = inspector
	runner.status = RunnerStatusReady
	runner.mu.Unlock()

	ticker := time.NewTicker(time.Millisecond * 50)
	defer ticker.Stop()

	ready <- nil

	// Command loop
	for {
		select {
		case <-ticker.C:
			break

		case signal := <-runner.signal:
			switch signal {

			case RunnerCommandReset:
				runner.reset()
				break

			case RunnerCommandExec:
				runner.exec()
				break

			case RunnerCommandDestroy:
				runner.destroy()
				return nil

			default:
				log.Warn("runner unknown signal: %d", signal)
			}

		}
	}
}

// Destroy send a destroy signal to the v8 runner
func (runner *Runner) Destroy(cb func()) {
	if runner == nil {
		return
	}

	runner.mu.Lock()
	if runner.closed && runner.status == RunnerStatusDestroy {
		runner.mu.Unlock()
		return
	}
	runner.closed = true
	signal := runner.signal
	runner.mu.Unlock()

	select {
	case signal <- RunnerCommandDestroy:
	default:
	}
}

func (runner *Runner) waitDestroyed(timeout time.Duration) bool {
	select {
	case <-runner.destroyed:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Reset send a reset signal to the v8 runner
func (runner *Runner) Reset() {
	if runner == nil {
		return
	}
	if runner.isClosed() {
		return
	}
	select {
	case runner.signal <- RunnerCommandReset:
	default:
	}
}

// Exec send a script to the v8 runner to execute
func (runner *Runner) Exec(script *Script) interface{} {
	return runner.ExecInvocation(runnerInvocation{script: script})
}

// ExecInvocation send an invocation to the v8 runner to execute
func (runner *Runner) ExecInvocation(inv runnerInvocation) interface{} {
	if runner == nil || inv.script == nil {
		return nil
	}

	runner.mu.Lock()
	if runner.closed {
		runner.mu.Unlock()
		return nil
	}
	runner.status = RunnerStatusRunning
	runner.script = inv.script
	runner.invocation = inv
	runner.chResp = make(chan interface{}, 1)
	method := inv.method
	status := runner.status
	keepalive := runner.keepalive
	signalLen := len(runner.signal)
	signal := runner.signal
	runner.mu.Unlock()

	log.Debug(fmt.Sprintf("2.  [%s] Exec script %s.%s. status:%d, keepalive:%v, signal:%d", runner.id, inv.script.ID, method, status, keepalive, signalLen))

	select {
	case signal <- RunnerCommandExec:
	default:
		return fmt.Errorf("[runner] command queue is full")
	}
	select {
	case res := <-runner.chResp:
		return res
	}
}

// Context get the context
func (runner *Runner) Context() (*v8go.Context, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.ctx, nil
}

func (runner *Runner) exec() {

	defer func() {
		go func() {
			if runner.isClosed() {
				return
			}
			status, keepalive := runner.snapshot()
			if !keepalive {
				log.Debug(fmt.Sprintf("3.1 [%s] Send a destroy signal to the v8 runner. status:%d, keepalive:%v", runner.id, status, keepalive))
				runner.sendCommand(RunnerCommandDestroy)
				log.Debug(fmt.Sprintf("3.2 [%s] Send a destroy signal to the v8 runner. sstatus:%d, keepalive:%v (done)", runner.id, status, keepalive))
				return
			}

			log.Debug(fmt.Sprintf("3.1 [%s] Send a reset signal to the v8 runner. status:%d, keepalive:%v", runner.id, status, keepalive))
			runner.sendCommand(RunnerCommandReset)
			log.Debug(fmt.Sprintf("3.2 [%s] Send a reset signal to the v8 runner. status:%d, keepalive:%v (done)", runner.id, status, keepalive))
		}()
	}()

	// runner.chResp <- "OK"
	runner._exec()
}

func (runner *Runner) _exec() {
	runner.mu.Lock()
	inv := runner.invocation
	iso := runner.iso
	ctx := runner.ctx
	inspector := runner.inspector
	runner.mu.Unlock()

	scriptTarget := v8Debug.targetForScript(inv.script)
	sessionTarget := v8Debug.sessionTargetForScript(inv.script)
	log.Info(fmt.Sprintf("[V8 Debug] _exec script %s, scriptTarget: %t, sessionTarget: %t, inspector: %t", inv.script.ID, scriptTarget != nil, sessionTarget != nil, inspector != nil))
	if sessionTarget != nil && inspector != nil {
		log.Info(fmt.Sprintf("[V8 Debug] attaching runner for script %s (target id: %s)", inv.script.ID, sessionTarget.id))
		if sessionTarget.attachRunner(runner, inspector, ctx, inv.script) {
			runner.mu.Lock()
			runner.debugTarget = sessionTarget
			runner.mu.Unlock()
		}
	}

	// Create instance of the script
	source := inv.script.Source
	origin := inv.script.File
	if scriptTarget != nil {
		source = scriptTarget.scriptSource()
		origin = scriptTarget.scriptURL()
	}
	instance, err := iso.CompileUnboundScript(source, origin, v8go.CompileOptions{})
	if err != nil {
		runner.chResp <- err
		return
	}
	v, err := instance.Run(ctx)
	if err != nil {
		runner.chResp <- err
		return
	}
	defer v.Release()

	// console.log("foo", "bar", 1, 2, 3, 4)
	err = console.New(runtimeOption.ConsoleMode).Set("console", ctx)
	if err != nil {
		runner.chResp <- err
		return
	}

	// Set the global data
	global := ctx.Global()
	err = bridge.SetShareData(ctx, global, &bridge.Share{
		Sid:    inv.sid,
		Root:   inv.script.Root,
		Global: inv.global,
	})
	if err != nil {
		runner.chResp <- err
		return
	}

	// Run the method
	jsArgs, err := bridge.JsValues(ctx, inv.args)
	if err != nil {
		runner.chResp <- err
		return

	}
	defer bridge.FreeJsValues(jsArgs)

	jsRes, err := global.MethodCall(inv.method, bridge.Valuers(jsArgs)...)
	if err != nil {
		if e, ok := err.(*v8go.JSError); ok {
			PrintException(inv.method, inv.args, e, inv.script.SourceRoots)
		}
		runner.chResp <- err
		return
	}

	goRes, err := bridge.GoValue(jsRes, ctx)
	if err != nil {
		runner.chResp <- err
		return
	}

	runner.chResp <- goRes
}

// destroy the runner
func (runner *Runner) destroy() {
	runner.mu.Lock()
	if runner.status == RunnerStatusDestroy {
		runner.mu.Unlock()
		return
	}
	runner.closed = true
	runner.status = RunnerStatusDestroy
	dispatcher := runner.dispatcher
	ctx := runner.ctx
	iso := runner.iso
	inspector := runner.inspector
	target := runner.debugTarget
	destroyed := runner.destroyed
	keepalive := runner.keepalive
	runner.ctx = nil
	runner.iso = nil
	runner.inspector = nil
	runner.debugTarget = nil
	runner.caches = nil
	runner.tmpl = nil
	runner.mu.Unlock()

	log.Debug(fmt.Sprintf("4.  [%s] destroy the runner. status:%d, keepalive:%v ", runner.id, RunnerStatusDestroy, keepalive))
	log.Debug(fmt.Sprintf("--- [%s] end -----------------", runner.id))

	if dispatcher != nil {
		dispatcher.runnerDestroyed(true)
	}
	if target != nil {
		if session := target.currentSession(); session != nil {
			session.detachNativeForRunner(runner)
		}
	}
	if ctx != nil {
		ctx.Close()
	}
	if inspector != nil {
		_ = inspector.Close()
	}
	if iso != nil {
		iso.Dispose()
	}
	if destroyed != nil {
		close(destroyed)
	}
}

// reset the runner
func (runner *Runner) reset() {
	if runner.isClosed() {
		runner.destroy()
		return
	}

	runner.mu.Lock()
	runner.status = RunnerStatusCleaning
	keepalive := runner.keepalive
	runner.mu.Unlock()

	log.Debug(fmt.Sprintf("4.  [%s] reset the runner. status:%d, keepalive:%v ", runner.id, RunnerStatusCleaning, keepalive))
	log.Debug(fmt.Sprintf("--- [%s] end -----------------", runner.id))

	runner.mu.Lock()
	ctx := runner.ctx
	iso := runner.iso
	tmpl := runner.tmpl
	target := runner.debugTarget
	runner.mu.Unlock()

	if target != nil {
		if session := target.currentSession(); session != nil {
			session.detachNativeForRunner(runner)
		}
	}
	if ctx != nil {
		ctx.Close()
	}

	if runner.isClosed() {
		runner.destroy()
		return
	}

	if !runnerHealthChecker(runner) {
		runner.dispatcher.healthEvictionCount()
		runner.destroy()
		return
	}

	if iso == nil || tmpl == nil {
		runner.destroy()
		return
	}

	nextCtx := v8go.NewContext(iso, tmpl)
	runner.mu.Lock()
	if runner.closed {
		runner.mu.Unlock()
		nextCtx.Close()
		runner.destroy()
		return
	}
	runner.ctx = nextCtx
	runner.debugTarget = nil
	runner.status = RunnerStatusReady
	runner.mu.Unlock()

	if !runner.dispatcher.release(runner, true) {
		runner.destroy()
	}

}

func (runner *Runner) health() bool {
	runner.mu.Lock()
	if runner.status == RunnerStatusDestroy || runner.iso == nil {
		runner.mu.Unlock()
		return true
	}
	runner.status = RunnerStatusCleaning
	iso := runner.iso
	runner.mu.Unlock()

	stat := iso.GetHeapStatistics()
	// utils.Dump(stat)

	log.Trace("[runner] [%s] health check. HeapStatistics:%d, HeapSizeRelease:%d", runner.id, stat.TotalHeapSize-stat.UsedHeapSize, runtimeOption.HeapSizeRelease)
	if stat.TotalHeapSize-stat.UsedHeapSize < runtimeOption.HeapSizeRelease || stat.NumberOfNativeContexts > 200 {
		log.Trace("[runner] [%s] health check. HeapStatistics: %d < %d Restart || %d", runner.id, stat.TotalHeapSize-stat.UsedHeapSize, runtimeOption.HeapSizeRelease, stat.NumberOfNativeContexts)
		return false
	}
	runner.mu.Lock()
	runner.status = RunnerStatusReady
	runner.mu.Unlock()
	return true
}

func (runner *Runner) isClosed() bool {
	if runner == nil {
		return true
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.closed
}

func (runner *Runner) snapshot() (uint8, bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.status, runner.keepalive
}

func (runner *Runner) sendCommand(command uint8) bool {
	runner.mu.Lock()
	signal := runner.signal
	runner.mu.Unlock()

	select {
	case signal <- command:
		return true
	default:
		return false
	}
}

func (runner *Runner) terminateExecution() {
	if runner == nil {
		return
	}

	runner.mu.Lock()
	iso := runner.iso
	runner.mu.Unlock()

	if iso != nil {
		iso.TerminateExecution()
	}
}
