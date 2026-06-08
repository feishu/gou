package v8

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/kun/log"
	v8go "rogchap.com/v8go"
)

type debugManager struct {
	mu                       sync.Mutex
	enabled                  bool
	host                     string
	port                     int
	policy                   debugTransportPolicy
	server                   *http.Server
	registry                 *debugRegistry
	beforeDebugServerPublish func()
}

type debugTarget struct {
	mu                   sync.Mutex
	id                   string
	groupID              int
	script               *Script
	session              *debugSession
	registry             *debugRegistry
	flatSourceMapOnce    sync.Once
	flatSourceMap        *SourceMap
	beforeSessionPublish func()
}

var (
	cdpLogMu   sync.Mutex
	cdpLogFile *os.File
)

func logCDP(direction string, msg []byte) {
	cdpLogMu.Lock()
	defer cdpLogMu.Unlock()
	if cdpLogFile != nil {
		_, _ = cdpLogFile.WriteString(direction + ": " + string(msg) + "\n")
	}
}

func configureCDPTrace(policy debugTransportPolicy) {
	cdpLogMu.Lock()
	defer cdpLogMu.Unlock()
	if cdpLogFile != nil {
		_ = cdpLogFile.Close()
		cdpLogFile = nil
	}
	if !policy.Trace.Enabled || policy.Trace.Path == "" {
		return
	}
	f, err := os.OpenFile(policy.Trace.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err == nil {
		cdpLogFile = f
	}
}

type debugTargetDescriptor struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	URL                  string `json:"url"`
	DevtoolsFrontendURL  string `json:"devtoolsFrontendUrl"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

const debugRuntimeTargetID = "runtime"

var v8Debug = newDebugManager()

func newDebugManager() *debugManager {
	policy := currentDebugTransportPolicy(Inspect{})
	manager := &debugManager{host: policy.Host, port: policy.Port, policy: policy}
	manager.registry = newDebugRegistry("127.0.0.1", 9229)
	manager.registry.manager = manager
	return manager
}

func startDebug(inspect Inspect) error {
	return v8Debug.start(inspect)
}

func stopDebug() {
	v8Debug.stop()
}

func (manager *debugManager) start(inspect Inspect) error {
	if !inspect.Enabled {
		return nil
	}

	policy := currentDebugTransportPolicy(inspect)
	host := policy.Host
	port := policy.Port

	manager.mu.Lock()
	if manager.enabled {
		manager.mu.Unlock()
		return nil
	}
	registry := newDebugRegistry(host, port)
	manager.enabled = true
	manager.host = host
	manager.port = port
	manager.policy = policy
	manager.registry = registry
	manager.registry.manager = manager
	manager.mu.Unlock()
	configureCDPTrace(policy)

	mux := http.NewServeMux()
	mux.HandleFunc("/json", manager.handleList)
	mux.HandleFunc("/json/list", manager.handleList)
	mux.HandleFunc("/json/version", manager.handleVersion)
	mux.HandleFunc("/json/new", manager.handleNew)
	mux.HandleFunc("/source-map/", manager.handleSourceMap)
	mux.HandleFunc("/ws/", manager.handleWebSocket)

	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		manager.mu.Lock()
		var targets []*debugTarget
		var runtimeTarget *debugTarget
		if manager.registry == registry {
			targets, runtimeTarget = registry.deactivateAndSnapshot()
			manager.enabled = false
		}
		manager.mu.Unlock()
		if runtimeTarget != nil {
			runtimeTarget.closeSession()
		}
		for _, target := range targets {
			target.closeSession()
		}
		return err
	}

	server := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	manager.mu.Lock()
	beforeDebugServerPublish := manager.beforeDebugServerPublish
	manager.mu.Unlock()
	if beforeDebugServerPublish != nil {
		beforeDebugServerPublish()
	}

	manager.mu.Lock()
	if manager.registry != registry || !manager.enabled {
		targets, runtimeTarget := registry.deactivateAndSnapshot()
		manager.mu.Unlock()
		_ = listener.Close()
		if runtimeTarget != nil {
			runtimeTarget.closeSession()
		}
		for _, target := range targets {
			target.closeSession()
		}
		return nil
	}
	manager.server = server
	manager.mu.Unlock()

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("[V8 Debug] inspect server error: %s", err.Error())
		}
	}()

	log.Info("[V8 Debug] inspect listening on http://%s:%d", host, port)
	return nil
}

func (manager *debugManager) stop() {
	manager.mu.Lock()
	server := manager.server
	registry := manager.registry
	targets, runtimeTarget := registry.deactivateAndSnapshot()
	manager.server = nil
	manager.enabled = false
	manager.registry = newDebugRegistry(manager.host, manager.port)
	manager.registry.manager = manager
	manager.mu.Unlock()

	if runtimeTarget != nil {
		runtimeTarget.closeSession()
	}
	for _, target := range targets {
		target.closeSession()
	}
	if server != nil {
		_ = server.Close()
	}
}

func (manager *debugManager) isEnabled() bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.enabled
}

func (manager *debugManager) activeRegistry() *debugRegistry {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.enabled {
		return nil
	}
	return manager.registry
}

func (manager *debugManager) currentPolicy() debugTransportPolicy {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.policy
}

func (manager *debugManager) registerScript(script *Script) {
	if script == nil {
		return
	}
	registry := manager.activeRegistry()
	if registry == nil {
		return
	}
	registry.registerScript(script)
}

func (manager *debugManager) ensureTarget(script *Script) *debugTarget {
	if script == nil {
		return nil
	}

	registry := manager.activeRegistry()
	if registry == nil {
		return nil
	}
	return registry.registerScript(script)
}

func (manager *debugManager) targetForScript(script *Script) *debugTarget {
	if script == nil {
		return nil
	}

	registry := manager.activeRegistry()
	if registry == nil {
		return nil
	}
	return registry.targetForScript(script)
}

func (manager *debugManager) sessionTargetForScript(script *Script) *debugTarget {
	if script == nil {
		return nil
	}

	registry := manager.activeRegistry()
	if registry == nil {
		return nil
	}
	return registry.sessionTargetForScript(script)
}

func (session *debugSession) hasBreakpointForTarget(target *debugTarget) bool {
	intents := session.breakpointIntentsSnapshot()

	if len(intents) == 0 {
		return true
	}

	hasURLBreakpoint := false
	for _, intent := range intents {
		if intent.Method != "Debugger.setBreakpointByUrl" {
			continue
		}
		if intent.URL == "" && intent.URLRegex == "" {
			continue
		}

		hasURLBreakpoint = true
		if intent.URL != "" && target.registry != nil {
			exactTarget := target.registry.targetForBreakpointURL(intent.URL)
			if exactTarget != nil {
				if exactTarget == target {
					return true
				}
				continue
			}
		}
		if _, sourceIdx := target.matchOwnBreakpointSourceMap(intent.URL, intent.URLRegex); sourceIdx >= 0 {
			return true
		}
	}

	return !hasURLBreakpoint
}

func (manager *debugManager) findTarget(id string) *debugTarget {
	registry := manager.activeRegistry()
	if registry == nil {
		return nil
	}
	return registry.findTarget(id)
}

func (manager *debugManager) descriptors(r *http.Request) []debugTargetDescriptor {
	registry := manager.activeRegistry()
	if registry == nil {
		return nil
	}
	return registry.descriptors(r)
}

func (manager *debugManager) runtimeDescriptor(r *http.Request) debugTargetDescriptor {
	registry := manager.activeRegistry()
	if registry == nil {
		return debugTargetDescriptor{}
	}
	return registry.runtimeDescriptor(r)
}

func (manager *debugManager) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeDebugJSON(w, map[string]string{
		"Browser":          "Yao/v8go",
		"Protocol-Version": "1.3",
		"V8-Version":       v8go.Version(),
	})
}

func (manager *debugManager) handleList(w http.ResponseWriter, r *http.Request) {
	log.Info(fmt.Sprintf("[V8 Debug] handleList requested from %s", r.RemoteAddr))

	if r.URL.Query().Get("all") != "1" {
		writeDebugJSON(w, []debugTargetDescriptor{manager.runtimeDescriptor(r)})
		return
	}

	descriptors := manager.descriptors(r)

	if application.App != nil && application.App.Root() != "" {
		targetFile := filepath.Join(application.App.Root(), ".debug_target")
		if data, err := os.ReadFile(targetFile); err == nil {
			filterTitle := strings.TrimSpace(string(data))
			if filterTitle != "" {
				filtered := []debugTargetDescriptor{}
				for _, desc := range descriptors {
					if strings.Contains(desc.Title, filterTitle) || strings.Contains(filterTitle, desc.Title) {
						filtered = append(filtered, desc)
					}
				}
				if len(filtered) > 0 {
					log.Info(fmt.Sprintf("[V8 Debug] handleList filtered by %s: %d -> %d targets", filterTitle, len(descriptors), len(filtered)))
					writeDebugJSON(w, filtered)
					return
				}
			}
		}
	}

	writeDebugJSON(w, descriptors)
}

func (manager *debugManager) handleNew(w http.ResponseWriter, r *http.Request) {
	scriptID := strings.TrimSpace(r.URL.Query().Get("script"))
	if strings.HasPrefix(scriptID, "scripts.") {
		scriptID = strings.TrimPrefix(scriptID, "scripts.")
	}
	if scriptID == "" {
		writeDebugJSON(w, manager.runtimeDescriptor(r))
		return
	}

	script, err := Select(scriptID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	target := manager.ensureTarget(script)
	if target == nil {
		http.Error(w, "inspector is disabled", http.StatusNotFound)
		return
	}
	writeDebugJSON(w, target.descriptor(r))
}

func (manager *debugManager) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/ws/")
	log.Info(fmt.Sprintf("[V8 Debug] handleWebSocket connect request for target id: %s", id))
	target := manager.findTarget(id)
	if target == nil {
		log.Warn(fmt.Sprintf("[V8 Debug] handleWebSocket target not found: %s", id))
		http.Error(w, "target not found", http.StatusNotFound)
		return
	}

	session, err := target.openSession()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	defer session.Close()

	policy := manager.currentPolicy()
	upgrader := websocket.Upgrader{CheckOrigin: policy.CheckOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	session.setTransportClose(conn.Close)
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range session.outbound {
			logCDP("V8->Client", msg)
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	for {
		typ, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if typ != websocket.TextMessage {
			continue
		}
		if err := session.Dispatch(msg); err != nil {
			log.Warn("[V8 Debug] dispatch failed: %s", err.Error())
			break
		}
	}
	session.Close()
	<-done
}

func (manager *debugManager) handleSourceMap(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/source-map/")
	target := manager.findTarget(id)
	if target == nil || target.script == nil {
		http.Error(w, "target not found", http.StatusNotFound)
		return
	}

	data, err := debugSourceMapBytes(target.script, manager.currentPolicy().ExposeSourceContent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		http.Error(w, "source map not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func debugNumberParam(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func (target *debugTarget) openSession() (*debugSession, error) {
	if target == nil || target.registry == nil {
		return nil, fmt.Errorf("debug target is inactive")
	}
	return target.registry.openSession(target)
}

func (target *debugTarget) currentSession() *debugSession {
	target.mu.Lock()
	session := target.session
	target.mu.Unlock()
	if session == nil || session.isClosed() {
		return nil
	}
	return session
}

func (target *debugTarget) closeSession() {
	session := target.currentSession()
	if session != nil {
		session.Close()
	}
}

func (target *debugTarget) acquireRunnerLease(runner *Runner, inspector *v8go.Inspector, ctx *v8go.Context, script *Script) (*debugRunnerLease, bool) {
	session := target.currentSession()
	if script == nil {
		script = target.script
	}
	scriptID := ""
	if script != nil {
		scriptID = script.ID
	}
	log.Info(fmt.Sprintf("[V8 Debug] acquireRunnerLease script:%s, targetID:%s, sessionExist:%t, inspectorExist:%t, ctxExist:%t", scriptID, target.id, session != nil, inspector != nil, ctx != nil))
	if session == nil || inspector == nil || ctx == nil || script == nil {
		return nil, false
	}

	scriptURL := debugScriptURL(script)
	opt := v8go.InspectorOptions{
		ContextGroupID: target.groupID,
		Name:           scriptURL,
		Origin:         scriptURL,
	}
	if err := inspector.NotifyContextCreated(ctx, opt); err != nil {
		log.Warn("[V8 Debug] context created failed: %s", err.Error())
		return nil, false
	}
	native, err := inspector.Connect(ctx, session, opt)
	if err != nil {
		log.Warn("[V8 Debug] inspector connect failed: %s", err.Error())
		return nil, false
	}
	if !session.attachNative(runner, native) {
		return nil, false
	}
	return newDebugRunnerLease(session, runner), true
}

func writeDebugJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func debugTargetID(scriptID string) string {
	sum := sha1.Sum([]byte(scriptID))
	return hex.EncodeToString(sum[:])[:16]
}

func debugContextGroupID(targetID string) int {
	sum := sha1.Sum([]byte(targetID))
	id := int(sum[0])<<24 | int(sum[1])<<16 | int(sum[2])<<8 | int(sum[3])
	if id < 0 {
		id = -id
	}
	if id == 0 {
		id = 1
	}
	return id
}
