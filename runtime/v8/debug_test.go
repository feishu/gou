package v8

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yaoapp/gou/application"
)

type fakeDebugNativeSession struct {
	dispatch func([]byte) error
	close    func() error
}

func (session *fakeDebugNativeSession) Dispatch(message []byte) error {
	if session.dispatch != nil {
		return session.dispatch(message)
	}
	return nil
}

func (session *fakeDebugNativeSession) Close() error {
	if session.close != nil {
		return session.close()
	}
	return nil
}

func TestDebugListReturnsRuntimeTargetByDefault(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	oldScripts := Scripts
	oldRootScripts := RootScripts
	t.Cleanup(func() {
		Scripts = oldScripts
		RootScripts = oldRootScripts
		manager.stop()
	})

	Scripts = map[string]*Script{
		"service.user": {
			ID:   "service.user",
			File: "scripts/service/user.ts",
		},
	}
	RootScripts = map[string]*Script{}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/json/list", nil)
	rec := httptest.NewRecorder()
	manager.handleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var targets []debugTargetDescriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected one target, got %d", len(targets))
	}

	target := targets[0]
	if target.Type != "node" {
		t.Fatalf("expected node target, got %s", target.Type)
	}
	if target.ID != debugRuntimeTargetID {
		t.Fatalf("expected runtime target id, got %s", target.ID)
	}
	if target.Title != "Yao Runtime" {
		t.Fatalf("unexpected target title: %s", target.Title)
	}
	if target.WebSocketDebuggerURL != "ws://127.0.0.1:9229/ws/runtime" {
		t.Fatalf("unexpected websocket debugger url: %s", target.WebSocketDebuggerURL)
	}
}

func TestDebugListIgnoresDebugTargetFileByDefault(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	oldApp := application.App
	oldScripts := Scripts
	oldRootScripts := RootScripts
	t.Cleanup(func() {
		application.App = oldApp
		Scripts = oldScripts
		RootScripts = oldRootScripts
		manager.stop()
	})

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".debug_target"), "scripts/service/user.ts")
	app, err := application.OpenFromDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	application.Load(app)

	Scripts = map[string]*Script{
		"service.user": {
			ID:   "service.user",
			File: "scripts/service/user.ts",
		},
		"service.order": {
			ID:   "service.order",
			File: "scripts/service/order.ts",
		},
	}
	RootScripts = map[string]*Script{}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/json/list", nil)
	rec := httptest.NewRecorder()
	manager.handleList(rec, req)

	var targets []debugTargetDescriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != debugRuntimeTargetID {
		t.Fatalf("expected default list to return runtime target, got %#v", targets)
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/json/list?all=1", nil)
	rec = httptest.NewRecorder()
	manager.handleList(rec, req)

	targets = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Title != "scripts/service/user.ts" {
		t.Fatalf("expected all list to honor debug target filter, got %#v", targets)
	}
}

func TestDebugListAllReturnsScriptTargets(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	oldScripts := Scripts
	oldRootScripts := RootScripts
	t.Cleanup(func() {
		Scripts = oldScripts
		RootScripts = oldRootScripts
		manager.stop()
	})

	Scripts = map[string]*Script{
		"service.user": {
			ID:   "service.user",
			File: "scripts/service/user.ts",
		},
	}
	RootScripts = map[string]*Script{}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/json/list?all=1", nil)
	rec := httptest.NewRecorder()
	manager.handleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var targets []debugTargetDescriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected one target, got %d", len(targets))
	}

	target := targets[0]
	if target.ID == debugRuntimeTargetID {
		t.Fatal("expected script target, got runtime target")
	}
	if target.Title != "scripts/service/user.ts" {
		t.Fatalf("unexpected target title: %s", target.Title)
	}
	if !strings.HasPrefix(target.WebSocketDebuggerURL, "ws://127.0.0.1:9229/ws/") {
		t.Fatalf("unexpected websocket debugger url: %s", target.WebSocketDebuggerURL)
	}
}

func TestDebugSourceMapReturnsTypeScriptSources(t *testing.T) {
	oldApp := application.App
	oldRuntimeOption := runtimeOption
	oldModules := Modules
	oldImportMap := ImportMap
	oldSourceMaps := SourceMaps
	oldSourceCodes := SourceCodes
	oldModuleSourceMaps := ModuleSourceMaps
	t.Cleanup(func() {
		application.App = oldApp
		runtimeOption = oldRuntimeOption
		Modules = oldModules
		ImportMap = oldImportMap
		SourceMaps = oldSourceMaps
		SourceCodes = oldSourceCodes
		ModuleSourceMaps = oldModuleSourceMaps
	})

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "scripts", "service", "consumer", "registration", "helper.ts"), `
export function decorate(value: string): string {
  return value + "-ok";
}
`)
	writeTestFile(t, filepath.Join(root, "scripts", "service", "consumer", "registration", "schedule.ts"), `
import { decorate } from "./helper";

export function Run() {
  return decorate("registration");
}
`)

	app, err := application.OpenFromDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	application.Load(app)

	runtimeOption = option()
	runtimeOption.Import = true
	CLearModules()

	file := filepath.Join("scripts", "service", "consumer", "registration", "schedule.ts")
	source, err := application.App.Read(file)
	if err != nil {
		t.Fatal(err)
	}
	script, err := MakeScript(source, file, 0)
	if err != nil {
		t.Fatal(err)
	}

	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229
	target := manager.ensureTarget(script)
	if target == nil {
		t.Fatal("expected debug target")
	}

	expectedScriptURL := "file://" + filepath.ToSlash(filepath.Join(root, file))
	if got := target.scriptURL(); got != expectedScriptURL {
		t.Fatalf("expected script url %s, got %s", expectedScriptURL, got)
	}

	debugSource := target.scriptSource()
	expectedURL := "http://127.0.0.1:9229/source-map/" + target.id
	if !strings.Contains(debugSource, "\n//# sourceMappingURL="+expectedURL) {
		t.Fatalf("missing sourceMappingURL %s in:\n%s", expectedURL, debugSource)
	}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/source-map/"+target.id, nil)
	rec := httptest.NewRecorder()
	manager.handleSourceMap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// 现在返回的是标准扁平 Source Map（非 Index Map）
	var smap SourceMap
	if err := json.Unmarshal(rec.Body.Bytes(), &smap); err != nil {
		t.Fatal(err)
	}
	if smap.Version != 3 {
		t.Fatalf("expected version 3, got %d", smap.Version)
	}
	if smap.Mappings == "" {
		t.Fatal("expected non-empty mappings")
	}

	mainFile := "file://" + filepath.ToSlash(filepath.Join(root, file))
	if !containsString(smap.Sources, mainFile) {
		t.Fatalf("expected main source %s, got %#v", mainFile, smap.Sources)
	}

	importFile := "file://" + filepath.ToSlash(filepath.Join(root, "scripts", "service", "consumer", "registration", "helper.ts"))
	if !containsString(smap.Sources, importFile) {
		t.Fatalf("expected import source %s in sources %#v", importFile, smap.Sources)
	}

	// 确认 mappings 包含行分隔符（说明偏移量已被正确编码）
	if !strings.Contains(smap.Mappings, ";") {
		t.Fatal("expected semicolons in mappings for line offsets")
	}
}

func TestDebugOpenSessionReplacesExistingSession(t *testing.T) {
	target := &debugTarget{
		id:      "target",
		script:  &Script{ID: "service.user", File: "scripts/service/user.ts"},
		manager: newDebugManager(),
	}

	first, err := target.openSession()
	if err != nil {
		t.Fatal(err)
	}
	transportClosed := false
	first.setTransportClose(func() error {
		transportClosed = true
		return nil
	})

	second, err := target.openSession()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("expected a new session")
	}
	if !first.isClosed() {
		t.Fatal("expected first session to be closed")
	}
	if !transportClosed {
		t.Fatal("expected first transport to be closed")
	}
	if got := target.currentSession(); got != second {
		t.Fatalf("expected current session to be replaced, got %#v", got)
	}
}

func TestDebugRuntimeSessionSelectedWithoutDebugTargetFile(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	session, err := manager.runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		manager.stop()
	})

	script := &Script{ID: "service.user", File: "scripts/service/user.ts"}
	if target := manager.sessionTargetForScript(script); target != manager.runtimeTarget {
		t.Fatalf("expected runtime target to be selected, got %#v", target)
	}

	scriptTarget := manager.targetForScript(script)
	if scriptTarget == nil {
		t.Fatal("expected script target to still be registered for source maps")
	}
	if scriptTarget.id == manager.runtimeTarget.id {
		t.Fatal("script target must stay separate from runtime target")
	}
}

func TestDebugRuntimeSessionSkipsScriptsWithoutMatchingBreakpoints(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	session, err := manager.runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		manager.stop()
	})

	schedule := &Script{ID: "service.schedule", File: "/Users/test/scripts/schedule.ts"}
	delayed := &Script{ID: "consumer.delayed_queue", File: "/Users/test/scripts/delayed_queue.ts"}
	scheduleTarget := manager.ensureTarget(schedule)
	delayedTarget := manager.ensureTarget(delayed)
	scheduleTarget.flatSourceMap = &SourceMap{Sources: []string{"file:///Users/test/scripts/schedule.ts"}}
	delayedTarget.flatSourceMap = &SourceMap{Sources: []string{"file:///Users/test/scripts/delayed_queue.ts"}}
	session.breakpointIntents = []debugBreakpointIntent{
		{
			Method:     "Debugger.setBreakpointByUrl",
			URL:        "file:///Users/test/scripts/schedule.ts",
			LineNumber: 101,
		},
	}

	if target := manager.sessionTargetForScript(schedule); target != manager.runtimeTarget {
		t.Fatalf("expected matching script to use runtime target, got %#v", target)
	}
	if target := manager.sessionTargetForScript(delayed); target != delayedTarget {
		t.Fatalf("expected non-matching script to avoid runtime target, got %#v", target)
	}
}

func TestDebugDetachNativeForRunnerIgnoresDifferentRunner(t *testing.T) {
	session := &debugSession{
		id:       "target",
		outbound: make(chan []byte, 1),
	}
	owner := &Runner{}
	other := &Runner{}
	nativeClosed := false
	native := &fakeDebugNativeSession{
		close: func() error {
			nativeClosed = true
			return nil
		},
	}
	session.attachNative(owner, native)

	session.detachNativeForRunner(other)
	if nativeClosed {
		t.Fatal("expected native session to stay attached for a different runner")
	}
	session.mu.Lock()
	if session.native != native || session.runner != owner {
		session.mu.Unlock()
		t.Fatal("expected native session and runner to remain attached")
	}
	session.mu.Unlock()

	session.detachNativeForRunner(owner)
	if !nativeClosed {
		t.Fatal("expected native session to close for the owning runner")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.native != nil || session.runner != nil {
		t.Fatal("expected native session to detach")
	}
}

func TestDebugAttachNativeDoesNotReplaceDifferentActiveRunner(t *testing.T) {
	session := &debugSession{
		id:       "target",
		outbound: make(chan []byte, 1),
	}
	owner := &Runner{}
	other := &Runner{}
	ownerClosed := false
	otherClosed := false
	ownerNative := &fakeDebugNativeSession{
		close: func() error {
			ownerClosed = true
			return nil
		},
	}
	otherNative := &fakeDebugNativeSession{
		close: func() error {
			otherClosed = true
			return nil
		},
	}

	session.attachNative(owner, ownerNative)
	session.attachNative(other, otherNative)

	if ownerClosed {
		t.Fatal("expected active native session to remain open")
	}
	if !otherClosed {
		t.Fatal("expected competing native session to be closed")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.native != ownerNative || session.runner != owner {
		t.Fatal("expected active runner native session to remain attached")
	}
}

func TestDebugAttachNativeReplacesNonMatchingRunnerForBreakpointScript(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	session, err := manager.runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		manager.stop()
	})

	schedule := &Script{ID: "service.schedule", File: "/Users/test/scripts/schedule.ts"}
	delayed := &Script{ID: "consumer.delayed_queue", File: "/Users/test/scripts/delayed_queue.ts"}
	scheduleTarget := manager.ensureTarget(schedule)
	delayedTarget := manager.ensureTarget(delayed)
	scheduleTarget.flatSourceMap = &SourceMap{Sources: []string{"file:///Users/test/scripts/schedule.ts"}}
	delayedTarget.flatSourceMap = &SourceMap{Sources: []string{"file:///Users/test/scripts/delayed_queue.ts"}}

	background := &Runner{script: delayed}
	request := &Runner{script: schedule}
	backgroundClosed := false
	requestClosed := false
	backgroundNative := &fakeDebugNativeSession{
		close: func() error {
			backgroundClosed = true
			return nil
		},
	}
	requestNative := &fakeDebugNativeSession{
		close: func() error {
			requestClosed = true
			return nil
		},
	}

	if !session.attachNative(background, backgroundNative) {
		t.Fatal("expected background runner to attach before breakpoints exist")
	}
	session.breakpointIntents = []debugBreakpointIntent{
		{
			Method:     "Debugger.setBreakpointByUrl",
			URL:        "file:///Users/test/scripts/schedule.ts",
			LineNumber: 101,
		},
	}
	if !session.attachNative(request, requestNative) {
		t.Fatal("expected breakpoint script runner to replace non-matching runner")
	}
	if !backgroundClosed {
		t.Fatal("expected old background native session to close")
	}
	if requestClosed {
		t.Fatal("expected request native session to stay open")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.native != requestNative || session.runner != request {
		t.Fatal("expected breakpoint script runner to own native session")
	}
}

func TestDebugRunnerDestroyDetachesNativeWithoutClosingTransport(t *testing.T) {
	target := &debugTarget{
		id:      "target",
		script:  &Script{ID: "service.user", File: "scripts/service/user.ts"},
		manager: newDebugManager(),
	}
	session, err := target.openSession()
	if err != nil {
		t.Fatal(err)
	}
	transportClosed := false
	session.setTransportClose(func() error {
		transportClosed = true
		return nil
	})

	runner := &Runner{
		status:      RunnerStatusRunning,
		keepalive:   true,
		debugTarget: target,
		destroyed:   make(chan struct{}),
	}
	nativeClosed := false
	session.attachNative(runner, &fakeDebugNativeSession{
		close: func() error {
			nativeClosed = true
			return nil
		},
	})

	runner.destroy()
	if !nativeClosed {
		t.Fatal("expected runner destroy to close the native inspector session")
	}
	if transportClosed {
		t.Fatal("runner destroy must not close the websocket transport")
	}
	if session.isClosed() {
		t.Fatal("runner destroy must not close the debug session")
	}
	if got := target.currentSession(); got != session {
		t.Fatalf("expected debug session to remain attached to target, got %#v", got)
	}
}

func TestDebugDetachWaitsForNativeDispatch(t *testing.T) {
	session := &debugSession{
		id:       "target",
		outbound: make(chan []byte, 1),
	}
	dispatchStarted := make(chan struct{})
	allowDispatchReturn := make(chan struct{})
	nativeClosed := make(chan struct{})
	native := &fakeDebugNativeSession{
		dispatch: func([]byte) error {
			close(dispatchStarted)
			<-allowDispatchReturn
			return nil
		},
		close: func() error {
			close(nativeClosed)
			return nil
		},
	}
	session.attachNative(nil, native)

	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- session.Dispatch([]byte(`{"id":1,"method":"Debugger.setBreakpointByUrl"}`))
	}()

	select {
	case <-dispatchStarted:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not start")
	}

	detachDone := make(chan struct{})
	go func() {
		session.detachNative()
		close(detachDone)
	}()

	select {
	case <-nativeClosed:
		t.Fatal("native session closed before dispatch returned")
	case <-detachDone:
		t.Fatal("detach completed before dispatch returned")
	case <-time.After(20 * time.Millisecond):
	}

	close(allowDispatchReturn)
	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}

	select {
	case <-detachDone:
	case <-time.After(time.Second):
		t.Fatal("detach did not finish after dispatch returned")
	}
	select {
	case <-nativeClosed:
	default:
		t.Fatal("native session was not closed after detach")
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.native != nil {
		t.Fatal("expected native session to be detached")
	}
}

func TestDebugDispatchRequeuesWhenNativeDetachedBeforeDispatch(t *testing.T) {
	session := &debugSession{
		id:       "target",
		outbound: make(chan []byte, 1),
	}
	dispatchStarted := make(chan struct{})
	allowDispatchContinue := make(chan struct{})
	native := &fakeDebugNativeSession{
		dispatch: func([]byte) error {
			close(dispatchStarted)
			<-allowDispatchContinue
			return nil
		},
	}
	session.attachNative(nil, native)

	message := []byte(`{"id":1,"method":"Debugger.setBreakpointByUrl"}`)
	dispatchDone := make(chan error, 1)
	go func() {
		session.mu.Lock()
		oldNative := session.native
		session.mu.Unlock()
		if oldNative == nil {
			dispatchDone <- errors.New("expected native session")
			return
		}
		close(dispatchStarted)
		<-allowDispatchContinue
		dispatchDone <- session.Dispatch(message)
	}()

	select {
	case <-dispatchStarted:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not start")
	}
	session.detachNative()
	close(allowDispatchContinue)

	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.pending) != 1 {
		t.Fatalf("expected one pending message, got %d", len(session.pending))
	}
	if string(session.pending[0]) != string(message) {
		t.Fatalf("unexpected pending message: %s", session.pending[0])
	}
}

func TestDebugDispatchReturnsErrorWhenNativeStillAttached(t *testing.T) {
	session := &debugSession{
		id:       "target",
		outbound: make(chan []byte, 1),
	}
	nativeErr := errors.New("native dispatch failed")
	native := &fakeDebugNativeSession{
		dispatch: func([]byte) error {
			return nativeErr
		},
	}
	session.attachNative(nil, native)

	if err := session.Dispatch([]byte(`{"id":1}`)); !errors.Is(err, nativeErr) {
		t.Fatalf("expected native error, got %v", err)
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.pending) != 0 {
		t.Fatalf("expected no pending messages, got %d", len(session.pending))
	}
}

func TestDebugDispatchSetBreakpointWithoutLineNumberReachesNative(t *testing.T) {
	session := &debugSession{
		id:       "target",
		outbound: make(chan []byte, 1),
	}
	dispatched := false
	session.attachNative(nil, &fakeDebugNativeSession{
		dispatch: func([]byte) error {
			dispatched = true
			return nil
		},
	})

	if err := session.Dispatch([]byte(`{"id":1,"method":"Debugger.setBreakpointByUrl"}`)); err != nil {
		t.Fatal(err)
	}
	if !dispatched {
		t.Fatal("expected breakpoint request without lineNumber to reach native session")
	}
}

func writeTestFile(t *testing.T, file string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.TrimLeft(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestReverseMapSourcePosition(t *testing.T) {
	// 简单 source map: 2 个源文件，编译后 3 行
	// genLine 0 → source 0, line 0
	// genLine 1 → source 0, line 1
	// genLine 2 → source 1, line 0
	sm := &SourceMap{
		Version:  3,
		Sources:  []string{"/a.ts", "/b.ts"},
		Mappings: "AAAA;AACA;ACDA",
	}

	// 查找 source 0 (/a.ts) line 0 → genLine 0
	genLine, found := reverseMapSourcePosition(sm, 0, 0)
	if !found {
		t.Fatal("expected to find mapping for source 0, line 0")
	}
	if genLine != 0 {
		t.Fatalf("expected genLine 0, got %d", genLine)
	}

	// 查找 source 0 (/a.ts) line 1 → genLine 1
	genLine, found = reverseMapSourcePosition(sm, 0, 1)
	if !found {
		t.Fatal("expected to find mapping for source 0, line 1")
	}
	if genLine != 1 {
		t.Fatalf("expected genLine 1, got %d", genLine)
	}

	// 查找 source 1 (/b.ts) line 0 → genLine 2
	genLine, found = reverseMapSourcePosition(sm, 1, 0)
	if !found {
		t.Fatal("expected to find mapping for source 1, line 0")
	}
	if genLine != 2 {
		t.Fatalf("expected genLine 2, got %d", genLine)
	}

	// 查找不存在的行
	_, found = reverseMapSourcePosition(sm, 0, 99)
	if found {
		t.Fatal("expected not found for non-existent line")
	}
}

func TestMatchSourceByURL(t *testing.T) {
	sources := []string{"file:///Users/test/a.ts", "/Users/test/b.ts"}

	idx := matchSourceByURL(sources, "file:///Users/test/a.ts")
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}

	idx = matchSourceByURL(sources, "/Users/test/b.ts")
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}

	idx = matchSourceByURL(sources, "file:///nonexistent.ts")
	if idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}
}

func TestMatchSourceByRegex(t *testing.T) {
	sources := []string{"/Users/test/scripts/a.ts", "/Users/test/scripts/b.ts"}

	regex := `file:\/\/\/Users\/test\/scripts\/a\.ts`
	idx := matchSourceByRegex(sources, regex)
	if idx != 0 {
		t.Fatalf("expected index 0, got %d", idx)
	}

	regex = `file:\/\/\/Users\/test\/scripts\/b\.ts`
	idx = matchSourceByRegex(sources, regex)
	if idx != 1 {
		t.Fatalf("expected index 1, got %d", idx)
	}

	regex = `file:\/\/\/nonexistent\.ts`
	idx = matchSourceByRegex(sources, regex)
	if idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}
}

func TestInterceptBreakpointByUrl(t *testing.T) {
	sourceFile := "/Users/test/scripts/schedule.ts"

	// 构造一个含有简单 mappings 的 source map:
	// 编译行 5 对应源行 0
	// AAAA = genCol=0, srcIdx=0, srcLine=0, srcCol=0
	sm := &SourceMap{
		Version:  3,
		Sources:  []string{sourceFile},
		Mappings: ";;;;;" + "AAAA", // 5 个空行后，第 6 行 (idx 5) 映射到 source 0, line 0
	}

	target := &debugTarget{
		id:      "test-target",
		script:  &Script{ID: "test", File: sourceFile},
		manager: newDebugManager(),
	}
	// 直接设置缓存的 source map
	target.flatSourceMap = sm

	// 模拟 VSCode 发送的 setBreakpointByUrl 消息：源行 0
	msg := `{"id":42,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":0,"columnNumber":0,"url":"file:///Users/test/scripts/schedule.ts"}}`
	result := target.interceptBreakpointByUrl([]byte(msg))

	// 解析结果，检查 lineNumber 被修改为 5
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}
	params := parsed["params"].(map[string]interface{})
	gotLine := int(params["lineNumber"].(float64))
	if gotLine != 5 {
		t.Fatalf("expected lineNumber 5 (compiled), got %d", gotLine)
	}

	// 不匹配的消息应原样返回
	otherMsg := `{"id":43,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":10,"columnNumber":0,"url":"file:///nonexistent.ts"}}`
	otherResult := target.interceptBreakpointByUrl([]byte(otherMsg))
	if string(otherResult) != otherMsg {
		t.Fatal("expected unmatched message to pass through unchanged")
	}

	// 非 setBreakpointByUrl 消息应原样返回
	enableMsg := `{"id":1,"method":"Debugger.enable"}`
	enableResult := target.interceptBreakpointByUrl([]byte(enableMsg))
	if string(enableResult) != enableMsg {
		t.Fatal("expected non-breakpoint message to pass through unchanged")
	}
}

func TestRuntimeTargetInterceptBreakpointByUrlFindsScriptSourceMap(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	sourceFile := "/Users/test/scripts/schedule.ts"

	sm := &SourceMap{
		Version:  3,
		Sources:  []string{sourceFile},
		Mappings: ";;;;;" + "AACA",
	}

	scriptTarget := &debugTarget{
		id:            "script-target",
		script:        &Script{ID: "service.user", File: sourceFile},
		manager:       manager,
		flatSourceMap: sm,
	}
	manager.targets[scriptTarget.id] = scriptTarget
	manager.byScript[scriptTarget.script.ID] = scriptTarget

	msg := `{"id":42,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":1,"columnNumber":0,"url":"file:///Users/test/scripts/schedule.ts"}}`
	result := manager.runtimeTarget.interceptBreakpointByUrl([]byte(msg))

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}
	params := parsed["params"].(map[string]interface{})
	gotLine := int(params["lineNumber"].(float64))
	if gotLine != 5 {
		t.Fatalf("expected lineNumber 5 (compiled), got %d", gotLine)
	}
}
