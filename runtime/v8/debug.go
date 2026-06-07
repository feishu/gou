package v8

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/kun/log"
	v8go "rogchap.com/v8go"
)

type debugManager struct {
	mu            sync.Mutex
	enabled       bool
	host          string
	port          int
	server        *http.Server
	runtimeTarget *debugTarget
	targets       map[string]*debugTarget
	byScript      map[string]*debugTarget
}

type debugTarget struct {
	mu                sync.Mutex
	id                string
	groupID           int
	script            *Script
	session           *debugSession
	manager           *debugManager
	flatSourceMapOnce sync.Once
	flatSourceMap     *SourceMap
}

var cdpLogFile *os.File

func init() {
	f, err := os.OpenFile("/tmp/cdp_trace.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err == nil {
		cdpLogFile = f
	}
}

func logCDP(direction string, msg []byte) {
	if cdpLogFile != nil {
		cdpLogFile.WriteString(direction + ": " + string(msg) + "\n")
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
	manager := &debugManager{
		targets:  map[string]*debugTarget{},
		byScript: map[string]*debugTarget{},
	}
	manager.runtimeTarget = &debugTarget{
		id:      debugRuntimeTargetID,
		groupID: debugContextGroupID(debugRuntimeTargetID),
		manager: manager,
	}
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

	host := inspect.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := inspect.Port
	if port == 0 {
		port = 9229
	}

	manager.mu.Lock()
	if manager.enabled {
		manager.mu.Unlock()
		return nil
	}
	manager.enabled = true
	manager.host = host
	manager.port = port
	manager.targets = map[string]*debugTarget{}
	manager.byScript = map[string]*debugTarget{}
	manager.mu.Unlock()

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
		manager.enabled = false
		manager.mu.Unlock()
		return err
	}

	server := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	manager.mu.Lock()
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
	targets := make([]*debugTarget, 0, len(manager.targets))
	for _, target := range manager.targets {
		targets = append(targets, target)
	}
	runtimeTarget := manager.runtimeTarget
	manager.server = nil
	manager.enabled = false
	manager.targets = map[string]*debugTarget{}
	manager.byScript = map[string]*debugTarget{}
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

func (manager *debugManager) registerScript(script *Script) {
	if script == nil || !manager.isEnabled() {
		return
	}
	manager.ensureTarget(script)
}

func (manager *debugManager) ensureTarget(script *Script) *debugTarget {
	if script == nil {
		return nil
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.enabled {
		return nil
	}
	if target := manager.byScript[script.ID]; target != nil {
		target.script = script
		return target
	}

	id := debugTargetID(script.ID)
	target := &debugTarget{
		id:      id,
		groupID: debugContextGroupID(id),
		script:  script,
		manager: manager,
	}
	manager.targets[id] = target
	manager.byScript[script.ID] = target
	return target
}

func (manager *debugManager) targetForScript(script *Script) *debugTarget {
	if script == nil || !manager.isEnabled() {
		return nil
	}
	return manager.ensureTarget(script)
}

func (manager *debugManager) sessionTargetForScript(script *Script) *debugTarget {
	if script == nil || !manager.isEnabled() {
		return nil
	}

	scriptTarget := manager.ensureTarget(script)
	if scriptTarget != nil && scriptTarget.currentSession() != nil {
		return scriptTarget
	}

	manager.mu.Lock()
	runtimeTarget := manager.runtimeTarget
	manager.mu.Unlock()
	if runtimeTarget != nil {
		session := runtimeTarget.currentSession()
		if session != nil && session.shouldAttachScript(script) {
			return runtimeTarget
		}
	}
	return scriptTarget
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
		if intent.URL != "" && target.manager != nil {
			exactTarget := target.manager.targetForBreakpointURL(intent.URL)
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

func (manager *debugManager) targetForBreakpointURL(cdpURL string) *debugTarget {
	manager.mu.Lock()
	targets := make([]*debugTarget, 0, len(manager.targets))
	for _, target := range manager.targets {
		targets = append(targets, target)
	}
	manager.mu.Unlock()

	for _, target := range targets {
		if debugSourceMatchesURL(debugScriptURL(target.script), cdpURL) {
			return target
		}
	}
	return nil
}

func (manager *debugManager) findTarget(id string) *debugTarget {
	if id == debugRuntimeTargetID {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return manager.runtimeTarget
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.targets[id]
}

func (manager *debugManager) refreshTargets() {
	if !manager.isEnabled() {
		return
	}

	syncLock.Lock()
	defer syncLock.Unlock()
	for _, script := range Scripts {
		manager.ensureTarget(script)
	}
	for _, script := range RootScripts {
		manager.ensureTarget(script)
	}
}

func (manager *debugManager) descriptors(r *http.Request) []debugTargetDescriptor {
	manager.refreshTargets()

	manager.mu.Lock()
	targets := make([]*debugTarget, 0, len(manager.targets))
	for _, target := range manager.targets {
		targets = append(targets, target)
	}
	manager.mu.Unlock()

	descriptors := make([]debugTargetDescriptor, 0, len(targets))
	for _, target := range targets {
		descriptors = append(descriptors, target.descriptor(r))
	}
	return descriptors
}

func (manager *debugManager) runtimeDescriptor(r *http.Request) debugTargetDescriptor {
	manager.mu.Lock()
	target := manager.runtimeTarget
	manager.mu.Unlock()
	return target.descriptor(r)
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

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
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

	data, err := debugSourceMapBytes(target.script)
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

func (target *debugTarget) descriptor(r *http.Request) debugTargetDescriptor {
	title := "Yao Runtime"
	description := "Yao v8go JavaScript runtime"
	targetURL := "yao://runtime"
	if target.script != nil {
		title = target.script.ID
		if target.script.File != "" {
			title = target.script.File
		}
		description = "Yao v8go JavaScript"
		targetURL = target.scriptURL()
	}
	wsURL := fmt.Sprintf("ws://%s/ws/%s", target.host(r), target.id)
	return debugTargetDescriptor{
		ID:                   target.id,
		Type:                 "node",
		Title:                title,
		Description:          description,
		URL:                  targetURL,
		DevtoolsFrontendURL:  fmt.Sprintf("devtools://devtools/bundled/js_app.html?ws=%s/ws/%s", target.host(r), target.id),
		WebSocketDebuggerURL: wsURL,
	}
}

func (target *debugTarget) scriptSource() string {
	source := target.script.Source
	if _, has := SourceMaps[target.script.File]; !has {
		return source
	}
	return strings.TrimRight(source, "\n") + "\n//# sourceMappingURL=" + target.sourceMapURL(nil)
}

func (target *debugTarget) sourceMapURL(r *http.Request) string {
	return fmt.Sprintf("http://%s/source-map/%s", target.host(r), target.id)
}

func (target *debugTarget) scriptURL() string {
	if target == nil {
		return ""
	}
	return debugScriptURL(target.script)
}

func debugScriptURL(script *Script) string {
	if script == nil || script.File == "" {
		return ""
	}

	file := filepath.Clean(filepath.FromSlash(script.File))
	if !filepath.IsAbs(file) && application.App != nil && application.App.Root() != "" {
		file = filepath.Join(application.App.Root(), file)
	}
	if filepath.IsAbs(file) {
		return (&url.URL{Scheme: "file", Path: file}).String()
	}
	return script.File
}

// getFlatSourceMap 返回缓存的扁平 source map，用于断点反向映射。
func (target *debugTarget) getFlatSourceMap() *SourceMap {
	if target.flatSourceMap != nil {
		return target.flatSourceMap
	}
	if target.script == nil {
		return nil
	}
	target.flatSourceMapOnce.Do(func() {
		sm, err := debugFlatSourceMap(target.script)
		if err == nil {
			target.flatSourceMap = sm
		}
	})
	return target.flatSourceMap
}

// interceptBreakpointByUrl 拦截 VSCode 发送的 Debugger.setBreakpointByUrl 消息，
// 通过 source map 将源文件行号反向映射为编译后行号。
// VSCode js-debug 发送源行号并期望 V8 内部通过 source map 解析，但 v8go 的
// V8 inspector 未加载 source map，所以必须在 proxy层完成反向映射。
func (target *debugTarget) interceptBreakpointByUrl(message []byte) []byte {
	if !bytes.Contains(message, []byte("setBreakpointByUrl")) {
		return message
	}

	var msg map[string]interface{}
	if err := jsoniter.Unmarshal(message, &msg); err != nil {
		log.Error("[V8 Debug] Unmarshal setBreakpointByUrl failed: %s", err.Error())
		return message
	}

	method, _ := msg["method"].(string)
	if method != "Debugger.setBreakpointByUrl" {
		return message
	}

	params, ok := msg["params"].(map[string]interface{})
	if !ok {
		log.Error("[V8 Debug] setBreakpointByUrl params is not map")
		return message
	}

	lineNum, ok := debugNumberParam(params["lineNumber"])
	if !ok {
		return message
	}
	sourceLine := int(lineNum)

	var cdpURL string
	if value, ok := params["url"].(string); ok {
		cdpURL = value
	}
	var urlRegex string
	if value, ok := params["urlRegex"].(string); ok {
		urlRegex = value
	}

	sm, sourceIdx := target.matchBreakpointSourceMap(cdpURL, urlRegex)
	if sm == nil {
		log.Warn("[V8 Debug] flat source map is nil for breakpoint. URL: %s, Regex: %s", cdpURL, urlRegex)
		return message
	}
	if sourceIdx < 0 {
		log.Warn("[V8 Debug] source not matched in source map. URL: %s, Regex: %s, Sources: %v", cdpURL, urlRegex, sm.Sources)
		return message
	}

	// 反向映射：源行号 → 编译行号
	genLine, found := reverseMapSourcePosition(sm, sourceIdx, sourceLine)
	if !found {
		log.Warn("[V8 Debug] breakpoint reverse-map not found: %s:%d (Line %d not mapped in source map)", sm.Sources[sourceIdx], sourceLine+1, sourceLine+1)
		return message
	}

	log.Info("[V8 Debug] breakpoint reverse-map: %s:%d → compiled line %d",
		filepath.Base(sm.Sources[sourceIdx]), sourceLine+1, genLine+1)

	params["lineNumber"] = genLine
	params["columnNumber"] = 0

	modified, err := jsoniter.Marshal(msg)
	if err != nil {
		log.Error("[V8 Debug] Marshal modified breakpoint failed: %s", err.Error())
		return message
	}
	return modified
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

func (target *debugTarget) matchBreakpointSourceMap(cdpURL string, urlRegex string) (*SourceMap, int) {
	if target.script != nil {
		return target.matchOwnBreakpointSourceMap(cdpURL, urlRegex)
	}
	if target.manager == nil {
		return nil, -1
	}
	return target.manager.matchBreakpointSourceMap(cdpURL, urlRegex)
}

func (target *debugTarget) matchOwnBreakpointSourceMap(cdpURL string, urlRegex string) (*SourceMap, int) {
	sm := target.getFlatSourceMap()
	if sm == nil {
		return nil, -1
	}
	sourceIdx := -1
	if cdpURL != "" {
		sourceIdx = matchSourceByURL(sm.Sources, cdpURL)
	}
	if sourceIdx < 0 && urlRegex != "" {
		sourceIdx = matchSourceByRegex(sm.Sources, urlRegex)
	}
	return sm, sourceIdx
}

func (manager *debugManager) matchBreakpointSourceMap(cdpURL string, urlRegex string) (*SourceMap, int) {
	manager.refreshTargets()

	manager.mu.Lock()
	targets := make([]*debugTarget, 0, len(manager.targets))
	for _, target := range manager.targets {
		targets = append(targets, target)
	}
	manager.mu.Unlock()

	for _, target := range targets {
		sm, sourceIdx := target.matchOwnBreakpointSourceMap(cdpURL, urlRegex)
		if sm != nil && sourceIdx >= 0 {
			return sm, sourceIdx
		}
	}
	return nil, -1
}

func (target *debugTarget) host(r *http.Request) string {
	if r != nil && r.Host != "" {
		return r.Host
	}
	target.manager.mu.Lock()
	defer target.manager.mu.Unlock()
	return net.JoinHostPort(target.manager.host, strconv.Itoa(target.manager.port))
}

func (target *debugTarget) openSession() (*debugSession, error) {
	target.mu.Lock()
	oldSession := target.session
	session := &debugSession{
		id:                target.id,
		target:            target,
		outbound:          make(chan []byte, 256),
		simulated:         make(map[int]bool),
		breakpointIntents: make([]debugBreakpointIntent, 0),
		nativeBreakpoints: make([][]byte, 0),
		initializers:      make([][]byte, 0),
	}
	session.breakpointRewrite = func(message []byte) debugBreakpointRewrite {
		intent := parseDebugBreakpointIntent(message)
		native := message
		if target != nil {
			native = target.interceptBreakpointByUrl(message)
		}
		return debugBreakpointRewrite{
			Intent:        intent,
			NativeMessage: append([]byte(nil), native...),
		}
	}
	target.session = session
	target.mu.Unlock()

	if oldSession != nil {
		_ = oldSession.Close()
	}
	return session, nil
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
