package v8

import (
	"fmt"
	"sync"
	"time"

	atobT "github.com/yaoapp/gou/runtime/v8/functions/atob"
	btoaT "github.com/yaoapp/gou/runtime/v8/functions/btoa"
	evalT "github.com/yaoapp/gou/runtime/v8/functions/eval"
	langT "github.com/yaoapp/gou/runtime/v8/functions/lang"
	processT "github.com/yaoapp/gou/runtime/v8/functions/process"
	exceptionT "github.com/yaoapp/gou/runtime/v8/objects/exception"
	fsT "github.com/yaoapp/gou/runtime/v8/objects/fs"
	httpT "github.com/yaoapp/gou/runtime/v8/objects/http"
	jobT "github.com/yaoapp/gou/runtime/v8/objects/job"
	logT "github.com/yaoapp/gou/runtime/v8/objects/log"
	planT "github.com/yaoapp/gou/runtime/v8/objects/plan"
	queryT "github.com/yaoapp/gou/runtime/v8/objects/query"
	storeT "github.com/yaoapp/gou/runtime/v8/objects/store"
	timeT "github.com/yaoapp/gou/runtime/v8/objects/time"
	websocketT "github.com/yaoapp/gou/runtime/v8/objects/websocket"
	"github.com/yaoapp/gou/runtime/v8/store"

	"github.com/yaoapp/kun/log"
	"rogchap.com/v8go"
)

var standardCompat = newStandardCompat()

// initialize create a new Isolate
// in performance mode, the minSize isolates will be created
func initialize() error {

	log.Info("[V8] initialize mode: %s", runtimeOption.Mode)
	v8go.YaoInit(uint(runtimeOption.HeapSizeLimit / 1024 / 1024))

	if err := startDebug(runtimeOption.Inspect); err != nil {
		v8go.YaoDispose()
		return err
	}

	dispatcher = NewDispatcher(runtimeOption.MinSize, runtimeOption.MaxSize)
	if err := dispatcher.Start(); err != nil {
		stopDebug()
		v8go.YaoDispose()
		return err
	}

	standardCompat.reset(runtimeOption.MaxSize)
	return nil

}

func release() {
	stopDebug()
	if dispatcher != nil {
		dispatcher.Stop()
	}
	standardCompat.stop()
}

// precompile compile the loaded scirpts
// it cost too much time and memory to compile all scripts
// ignore the error
func precompile(iso *store.Isolate) {
	return
}

// MakeTemplate make a new template
func MakeTemplate(iso *v8go.Isolate) *v8go.ObjectTemplate {

	template := v8go.NewObjectTemplate(iso)
	template.Set("log", logT.New().ExportObject(iso))
	template.Set("time", timeT.New().ExportObject(iso))
	template.Set("http", httpT.New(runtimeOption.DataRoot).ExportObject(iso))

	// set functions
	template.Set("Exception", exceptionT.New().ExportFunction(iso))
	template.Set("FS", fsT.New().ExportFunction(iso))
	template.Set("Job", jobT.New().ExportFunction(iso))
	template.Set("Store", storeT.New().ExportFunction(iso))
	template.Set("Plan", planT.New().ExportFunction(iso))
	template.Set("Query", queryT.New().ExportFunction(iso))
	template.Set("WebSocket", websocketT.New().ExportFunction(iso))
	template.Set("$L", langT.ExportFunction(iso))
	template.Set("Process", processT.ExportFunction(iso))
	template.Set("Eval", evalT.ExportFunction(iso))

	// Deprecated Studio and Require
	// template.Set("Studio", studioT.ExportFunction(iso))
	// template.Set("Require", Require(iso))

	// Window object (std functions)
	template.Set("atob", atobT.ExportFunction(iso))
	template.Set("btoa", btoaT.ExportFunction(iso))
	return template
}

func makeGlobalIsolate() {
	iso := v8go.YaoNewIsolate()
	iso.AsGlobal()
}

func makeIsolate() *store.Isolate {
	// iso, err := v8go.YaoNewIsolateFromGlobal()
	// if err != nil {
	// 	log.Error("[V8] Create isolate failed: %s", err.Error())
	// 	return nil
	// }

	iso := v8go.YaoNewIsolate()
	return &store.Isolate{
		Isolate:  iso,
		Template: MakeTemplate(iso),
		Status:   IsoReady,
	}
}

// SelectIsoStandard one ready isolate ( the max size is 2 )
func SelectIsoStandard(timeout time.Duration) (*store.Isolate, error) {
	iso, err := standardCompat.selectIsolate(timeout)
	if err != nil {
		log.Error("[V8] Select isolate timeout %v", timeout)
		return nil, err
	}
	return iso, nil
}

func standardCompatStats() StandardCompatStats {
	return standardCompat.stats()
}

type standardCompatStore struct {
	mu      sync.Mutex
	cond    *sync.Cond
	idle    []*store.Isolate
	max     uint
	total   uint
	created uint64
	closed  bool
}

func newStandardCompat() *standardCompatStore {
	compat := &standardCompatStore{}
	compat.cond = sync.NewCond(&compat.mu)
	return compat
}

func (compat *standardCompatStore) reset(max uint) {
	compat.mu.Lock()
	compat.idle = nil
	compat.max = max
	compat.total = 0
	compat.created = 0
	compat.closed = false
	compat.mu.Unlock()
	compat.cond.Broadcast()
}

func (compat *standardCompatStore) selectIsolate(timeout time.Duration) (*store.Isolate, error) {
	deadline := time.Now().Add(timeout)
	for {
		compat.mu.Lock()
		if compat.closed {
			compat.mu.Unlock()
			return nil, fmt.Errorf("Select isolate closed")
		}
		if len(compat.idle) > 0 {
			last := len(compat.idle) - 1
			iso := compat.idle[last]
			compat.idle = compat.idle[:last]
			iso.Lock()
			compat.mu.Unlock()
			return iso, nil
		}
		if compat.total < compat.max {
			compat.total++
			compat.created++
			compat.mu.Unlock()

			iso := makeIsolate()
			iso.Lock()
			iso.OnDispose = compat.releaseIsolate
			return iso, nil
		}
		compat.mu.Unlock()

		if time.Until(deadline) <= 0 {
			return nil, fmt.Errorf("Select isolate timeout %v", timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (compat *standardCompatStore) releaseIsolate(iso *store.Isolate) {
	if iso == nil {
		return
	}

	compat.mu.Lock()
	if compat.closed || iso.Isolate == nil {
		if compat.total > 0 {
			compat.total--
		}
		compat.mu.Unlock()
		compat.cond.Broadcast()
		iso.OnDispose = nil
		iso.DisposeDirect()
		return
	}

	iso.Unlock()
	compat.idle = append(compat.idle, iso)
	compat.mu.Unlock()
	compat.cond.Broadcast()
}

func (compat *standardCompatStore) stop() {
	compat.mu.Lock()
	compat.closed = true
	idle := compat.idle
	compat.idle = nil
	compat.total = 0
	compat.mu.Unlock()
	compat.cond.Broadcast()

	for _, iso := range idle {
		iso.OnDispose = nil
		iso.DisposeDirect()
	}
}

func (compat *standardCompatStore) stats() StandardCompatStats {
	compat.mu.Lock()
	defer compat.mu.Unlock()
	return StandardCompatStats{
		Created: compat.created,
		Active:  uint64(compat.total),
		Idle:    uint64(len(compat.idle)),
	}
}
