package v8

import (
	"encoding/json"
	"errors"
	"net"
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

func debugBreakpointMessages(messages [][]byte) [][]byte {
	breakpoints := [][]byte{}
	for _, message := range messages {
		if strings.Contains(string(message), `"method":"Debugger.setBreakpointByUrl"`) {
			breakpoints = append(breakpoints, message)
		}
	}
	return breakpoints
}

func debugBreakpointLineNumber(t *testing.T, message []byte) int {
	t.Helper()

	var parsed struct {
		Params struct {
			LineNumber int `json:"lineNumber"`
		} `json:"params"`
	}
	if err := json.Unmarshal(message, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Params.LineNumber
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

func TestDebugManagerActiveRegistryReturnsNilWhenDisabled(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = false

	if registry := manager.activeRegistry(); registry != nil {
		t.Fatalf("expected disabled manager to have no active registry, got %#v", registry)
	}
	if target := manager.ensureTarget(&Script{ID: "service.user", File: "scripts/service/user.ts"}); target != nil {
		t.Fatalf("expected disabled manager not to register target, got %#v", target)
	}
	if len(manager.registry.scriptTargetsSnapshot()) != 0 {
		t.Fatal("expected disabled manager registry to stay empty")
	}
}

func TestDebugManagerFindTargetReturnsNilWhenDisabled(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = false

	if target := manager.findTarget(debugRuntimeTargetID); target != nil {
		t.Fatalf("expected disabled manager not to find runtime target, got %#v", target)
	}
	if descriptor := manager.runtimeDescriptor(nil); descriptor.ID == debugRuntimeTargetID {
		t.Fatalf("expected disabled manager not to publish runtime descriptor, got %+v", descriptor)
	}
	if descriptors := manager.descriptors(nil); len(descriptors) != 0 {
		t.Fatalf("expected disabled manager not to publish script descriptors, got %+v", descriptors)
	}
}

func TestDebugManagerStopDeactivatesStaleRegistry(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	oldRegistry := manager.registry

	manager.stop()

	if target := oldRegistry.registerScript(&Script{ID: "service.user", File: "scripts/service/user.ts"}); target != nil {
		t.Fatalf("expected stopped registry not to register target, got %#v", target)
	}
	if len(oldRegistry.scriptTargetsSnapshot()) != 0 {
		t.Fatal("expected stopped registry to stay empty")
	}
}

func TestDebugManagerStartListenFailureDeactivatesRegistry(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	manager := newDebugManager()
	err = manager.start(Inspect{Enabled: true, Host: "127.0.0.1", Port: port})
	if err == nil {
		t.Fatal("expected listen failure")
	}

	failedRegistry := manager.registry
	if target := failedRegistry.registerScript(&Script{ID: "service.user", File: "scripts/service/user.ts"}); target != nil {
		t.Fatalf("expected failed-start registry not to register target, got %#v", target)
	}
	if len(failedRegistry.scriptTargetsSnapshot()) != 0 {
		t.Fatal("expected failed-start registry to stay empty")
	}
}

func TestDebugManagerStartStopBeforeServerPublishDoesNotLeakListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	manager := newDebugManager()
	t.Cleanup(manager.stop)

	hookCalled := false
	manager.beforeDebugServerPublish = func() {
		hookCalled = true
		manager.stop()
	}

	err = manager.start(Inspect{Enabled: true, Host: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Fatal("expected publish hook to run")
	}

	manager.mu.Lock()
	server := manager.server
	enabled := manager.enabled
	manager.mu.Unlock()
	if server != nil {
		t.Fatal("expected stopped manager not to publish server")
	}
	if enabled {
		t.Fatal("expected manager to stay disabled after stop in publish hook")
	}

	manager.beforeDebugServerPublish = nil
	if err := manager.start(Inspect{Enabled: true, Host: "127.0.0.1", Port: port}); err != nil {
		t.Fatalf("expected restart on same port after stopped start, got %v", err)
	}
}

func TestDebugSessionShouldAttachScriptDoesNotRegisterAfterRegistryStopped(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	oldRegistry := manager.registry
	session, err := oldRegistry.runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}

	manager.stop()

	if session.shouldAttachScript(&Script{ID: "service.user", File: "scripts/service/user.ts"}) {
		t.Fatal("expected stopped session registry not to allow attach")
	}
	if len(oldRegistry.scriptTargetsSnapshot()) != 0 {
		t.Fatal("expected stopped session registry to stay empty")
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

func prepareDebugSourceMapTarget(t *testing.T) (*debugManager, *debugTarget) {
	t.Helper()

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

	return manager, target
}

func TestDebugSourceMapReturnsTypeScriptSources(t *testing.T) {
	manager, target := prepareDebugSourceMapTarget(t)
	root := application.App.Root()
	file := target.script.File

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
	if len(smap.SourcesContent) == 0 {
		t.Fatal("expected sourcesContent to be exposed by default")
	}
}

func TestDebugSourceMapOmitsSourcesContentWhenPolicyDisabled(t *testing.T) {
	manager, target := prepareDebugSourceMapTarget(t)
	exposeSourceContent := false
	manager.policy = currentDebugTransportPolicy(Inspect{
		Enabled:             true,
		Host:                "127.0.0.1",
		Port:                9229,
		ExposeSourceContent: &exposeSourceContent,
	})

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/source-map/"+target.id, nil)
	rec := httptest.NewRecorder()
	manager.handleSourceMap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var smap SourceMap
	if err := json.Unmarshal(rec.Body.Bytes(), &smap); err != nil {
		t.Fatal(err)
	}
	if len(smap.Sources) == 0 {
		t.Fatal("expected sources to remain available")
	}
	if smap.Mappings == "" {
		t.Fatal("expected mappings to remain available")
	}
	if len(smap.SourcesContent) != 0 {
		t.Fatalf("expected sourcesContent to be omitted, got %d entries", len(smap.SourcesContent))
	}
}

func TestDebugOpenSessionReplacesExistingSession(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	target := &debugTarget{
		id:       "target",
		script:   &Script{ID: "service.user", File: "scripts/service/user.ts"},
		registry: registry,
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

func TestDebugWebSocketRejectsInvalidOriginBeforeReplacingSession(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229
	manager.policy = currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	manager.registry = newDebugRegistry("127.0.0.1", 9229)
	manager.registry.manager = manager

	target := manager.registry.registerScript(&Script{ID: "service.user", File: "scripts/service/user.ts"})
	first, err := target.openSession()
	if err != nil {
		t.Fatal(err)
	}
	transportClosed := false
	first.setTransportClose(func() error {
		transportClosed = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9229/ws/"+target.id, nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	manager.handleWebSocket(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden websocket origin, got %d: %s", rec.Code, rec.Body.String())
	}
	if first.isClosed() {
		t.Fatal("invalid origin must not close the existing session")
	}
	if transportClosed {
		t.Fatal("invalid origin must not close the existing transport")
	}
	if got := target.currentSession(); got != first {
		t.Fatalf("expected current session to remain unchanged, got %#v", got)
	}
}

func TestDebugTargetOpenSessionRejectsInactiveRegistry(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	target := registry.registerScript(&Script{ID: "service.user", File: "scripts/service/user.ts"})
	registry.deactivateAndSnapshot()

	session, err := target.openSession()
	if err == nil {
		_ = session.Close()
		t.Fatal("expected inactive debug target to reject session")
	}
	if !strings.Contains(err.Error(), "debug target is inactive") {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.currentSession() != nil {
		t.Fatal("expected inactive debug target not to create session")
	}
}

func TestDebugTargetOpenSessionRejectsDeactivateBeforePublish(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	target := registry.registerScript(&Script{ID: "service.user", File: "scripts/service/user.ts"})
	target.beforeSessionPublish = func() {
		registry.active = false
	}

	session, err := target.openSession()
	if err == nil {
		_ = session.Close()
		t.Fatal("expected inactive debug target to reject session")
	}
	if !strings.Contains(err.Error(), "debug target is inactive") {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.currentSession() != nil {
		t.Fatal("expected inactive debug target not to publish session")
	}
}

func TestDebugRuntimeSessionSelectedWithoutDebugTargetFile(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	runtimeTarget := manager.registry.runtimeTarget
	session, err := runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		manager.stop()
	})

	script := &Script{ID: "service.user", File: "scripts/service/user.ts"}
	if target := manager.sessionTargetForScript(script); target != runtimeTarget {
		t.Fatalf("expected runtime target to be selected, got %#v", target)
	}

	scriptTarget := manager.targetForScript(script)
	if scriptTarget == nil {
		t.Fatal("expected script target to still be registered for source maps")
	}
	if scriptTarget.id == runtimeTarget.id {
		t.Fatal("script target must stay separate from runtime target")
	}
}

func TestDebugRuntimeSessionSkipsScriptsWithoutMatchingBreakpoints(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	runtimeTarget := manager.registry.runtimeTarget
	session, err := runtimeTarget.openSession()
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

	if target := manager.sessionTargetForScript(schedule); target != runtimeTarget {
		t.Fatalf("expected matching script to use runtime target, got %#v", target)
	}
	if target := manager.sessionTargetForScript(delayed); target != delayedTarget {
		t.Fatalf("expected non-matching script to avoid runtime target, got %#v", target)
	}
}

func TestDebugRuntimeSessionStepIntoFollowsScriptWithoutBreakpoint(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	runtimeTarget := manager.registry.runtimeTarget
	session, err := runtimeTarget.openSession()
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

	if target := manager.sessionTargetForScript(delayed); target != delayedTarget {
		t.Fatalf("expected non-matching script to avoid runtime target before stepInto, got %#v", target)
	}

	if !session.attachNative(&Runner{script: schedule}, &fakeDebugNativeSession{}) {
		t.Fatal("expected schedule runner to attach")
	}
	if err := session.Dispatch([]byte(`{"id":88,"method":"Debugger.stepInto"}`)); err != nil {
		t.Fatal(err)
	}

	if target := manager.sessionTargetForScript(delayed); target != runtimeTarget {
		t.Fatalf("expected stepInto to follow next script through runtime target, got %#v", target)
	}
}

func TestDebugRuntimeSessionStepIntoPausesFollowedScriptOnAttach(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	runtimeTarget := manager.registry.runtimeTarget
	session, err := runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		manager.stop()
	})

	schedule := &Script{ID: "service.schedule", File: "/Users/test/scripts/schedule.ts"}
	delayed := &Script{ID: "consumer.delayed_queue", File: "/Users/test/scripts/delayed_queue.ts"}
	manager.ensureTarget(schedule).flatSourceMap = &SourceMap{Sources: []string{"file:///Users/test/scripts/schedule.ts"}}
	manager.ensureTarget(delayed).flatSourceMap = &SourceMap{Sources: []string{"file:///Users/test/scripts/delayed_queue.ts"}}
	session.breakpointIntents = []debugBreakpointIntent{
		{
			Method:     "Debugger.setBreakpointByUrl",
			URL:        "file:///Users/test/scripts/schedule.ts",
			LineNumber: 101,
		},
	}

	if !session.attachNative(&Runner{script: schedule}, &fakeDebugNativeSession{}) {
		t.Fatal("expected schedule runner to attach")
	}
	if err := session.Dispatch([]byte(`{"id":89,"method":"Debugger.stepInto"}`)); err != nil {
		t.Fatal(err)
	}

	dispatched := [][]byte{}
	delayedNative := &fakeDebugNativeSession{
		dispatch: func(message []byte) error {
			dispatched = append(dispatched, append([]byte(nil), message...))
			return nil
		},
	}
	if !session.attachNative(&Runner{script: delayed}, delayedNative) {
		t.Fatal("expected stepInto followed runner to attach")
	}

	foundPause := false
	for _, message := range dispatched {
		if strings.Contains(string(message), `"method":"Debugger.pause"`) {
			foundPause = true
			break
		}
	}
	if !foundPause {
		t.Fatalf("expected followed script attach to request Debugger.pause, got %q", dispatched)
	}
}

func TestDebugRuntimeSessionRewritesStoredBreakpointWhenScriptRegistersLater(t *testing.T) {
	manager := newDebugManager()
	manager.enabled = true
	manager.host = "127.0.0.1"
	manager.port = 9229

	runtimeTarget := manager.registry.runtimeTarget
	session, err := runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		manager.stop()
	})

	sourceFile := "/Users/test/scripts/service/consumer/consult.ts"
	sourceURL := "file:///Users/test/scripts/service/consumer/consult.ts"
	message := []byte(`{"id":42,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":1,"columnNumber":0,"url":"` + sourceURL + `"}}`)
	if err := session.Dispatch(message); err != nil {
		t.Fatal(err)
	}

	nativeBeforeRegister := session.nativeBreakpointsSnapshot()
	if len(nativeBeforeRegister) != 1 {
		t.Fatalf("expected one stored native breakpoint, got %d", len(nativeBeforeRegister))
	}
	if got := debugBreakpointLineNumber(t, nativeBeforeRegister[0]); got != 1 {
		t.Fatalf("expected unresolved breakpoint line 1 before script registration, got %d", got)
	}

	session.mu.Lock()
	pendingBeforeAttach := append([][]byte(nil), session.pending...)
	session.mu.Unlock()
	if len(pendingBeforeAttach) != 1 {
		t.Fatalf("expected one pending breakpoint before attach, got %d", len(pendingBeforeAttach))
	}
	if got := debugBreakpointLineNumber(t, pendingBeforeAttach[0]); got != 1 {
		t.Fatalf("expected pending breakpoint line 1 before attach, got %d", got)
	}

	consult := &Script{ID: "scripts.service.consumer.consult", File: sourceFile}
	consultTarget := manager.ensureTarget(consult)
	consultTarget.flatSourceMap = &SourceMap{
		Version:  3,
		Sources:  []string{sourceFile},
		Mappings: ";;;;;" + "AACA",
	}

	if target := manager.sessionTargetForScript(consult); target != runtimeTarget {
		t.Fatalf("expected runtime target to be selected for consult breakpoint, got %#v", target)
	}

	var dispatched [][]byte
	native := &fakeDebugNativeSession{
		dispatch: func(message []byte) error {
			dispatched = append(dispatched, append([]byte(nil), message...))
			return nil
		},
	}
	if !session.attachNative(&Runner{script: consult}, native) {
		t.Fatal("expected consult runner native session to attach")
	}

	breakpoints := debugBreakpointMessages(dispatched)
	if len(breakpoints) != 2 {
		t.Fatalf("expected restored and pending breakpoint dispatches, got %d breakpoint messages out of %d total", len(breakpoints), len(dispatched))
	}
	for i, breakpoint := range breakpoints {
		if got := debugBreakpointLineNumber(t, breakpoint); got != 5 {
			t.Fatalf("expected restored breakpoint %d to be rewritten to compiled line 5, got %d; message=%s", i, got, breakpoint)
		}
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

	runtimeTarget := manager.registry.runtimeTarget
	session, err := runtimeTarget.openSession()
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
	registry := newDebugRegistry("127.0.0.1", 9229)
	target := &debugTarget{
		id:       "target",
		script:   &Script{ID: "service.user", File: "scripts/service/user.ts"},
		registry: registry,
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
		status:    RunnerStatusRunning,
		keepalive: true,
		destroyed: make(chan struct{}),
	}
	nativeClosed := false
	session.attachNative(runner, &fakeDebugNativeSession{
		close: func() error {
			nativeClosed = true
			return nil
		},
	})
	runner.debugLease = newDebugRunnerLease(session, runner)

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

	registry := newDebugRegistry("127.0.0.1", 9229)
	target := registry.registerScript(&Script{ID: "test", File: sourceFile})
	// 直接设置缓存的 source map
	target.flatSourceMap = sm

	// 模拟 VSCode 发送的 setBreakpointByUrl 消息：源行 0
	msg := `{"id":42,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":0,"columnNumber":0,"url":"file:///Users/test/scripts/schedule.ts"}}`
	rewrite := target.rewriteBreakpointByURL([]byte(msg))

	// 解析结果，检查 lineNumber 被修改为 5
	var parsed map[string]interface{}
	if err := json.Unmarshal(rewrite.NativeMessage, &parsed); err != nil {
		t.Fatal(err)
	}
	params := parsed["params"].(map[string]interface{})
	gotLine := int(params["lineNumber"].(float64))
	if gotLine != 5 {
		t.Fatalf("expected lineNumber 5 (compiled), got %d", gotLine)
	}

	// 不匹配的消息应原样返回
	otherMsg := `{"id":43,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":10,"columnNumber":0,"url":"file:///nonexistent.ts"}}`
	otherRewrite := target.rewriteBreakpointByURL([]byte(otherMsg))
	if string(otherRewrite.NativeMessage) != otherMsg {
		t.Fatal("expected unmatched message to pass through unchanged")
	}

	// 非 setBreakpointByUrl 消息应原样返回
	enableMsg := `{"id":1,"method":"Debugger.enable"}`
	enableRewrite := target.rewriteBreakpointByURL([]byte(enableMsg))
	if string(enableRewrite.NativeMessage) != enableMsg {
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

	scriptTarget := manager.registry.registerScript(&Script{ID: "service.user", File: sourceFile})
	scriptTarget.flatSourceMap = sm

	msg := `{"id":42,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":1,"columnNumber":0,"url":"file:///Users/test/scripts/schedule.ts"}}`
	rewrite := manager.registry.runtimeTarget.rewriteBreakpointByURL([]byte(msg))

	var parsed map[string]interface{}
	if err := json.Unmarshal(rewrite.NativeMessage, &parsed); err != nil {
		t.Fatal(err)
	}
	params := parsed["params"].(map[string]interface{})
	gotLine := int(params["lineNumber"].(float64))
	if gotLine != 5 {
		t.Fatalf("expected lineNumber 5 (compiled), got %d", gotLine)
	}
}
