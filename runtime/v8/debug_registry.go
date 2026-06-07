package v8

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/kun/log"
)

type debugRegistry struct {
	mu            sync.Mutex
	active        bool
	host          string
	port          int
	runtimeTarget *debugTarget
	targets       map[string]*debugTarget
	byScript      map[string]*debugTarget
	manager       *debugManager
}

func newDebugRegistry(host string, port int) *debugRegistry {
	registry := &debugRegistry{
		active:   true,
		host:     host,
		port:     port,
		targets:  map[string]*debugTarget{},
		byScript: map[string]*debugTarget{},
	}
	registry.runtimeTarget = &debugTarget{
		id:       debugRuntimeTargetID,
		groupID:  debugContextGroupID(debugRuntimeTargetID),
		registry: registry,
	}
	return registry
}

func (registry *debugRegistry) registerScript(script *Script) *debugTarget {
	if registry == nil || script == nil {
		return nil
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.active {
		return nil
	}

	if target := registry.byScript[script.ID]; target != nil {
		target.script = script
		return target
	}

	id := debugTargetID(script.ID)
	target := &debugTarget{
		id:       id,
		groupID:  debugContextGroupID(id),
		script:   script,
		registry: registry,
	}
	registry.targets[id] = target
	registry.byScript[script.ID] = target
	return target
}

func (registry *debugRegistry) targetForScript(script *Script) *debugTarget {
	return registry.registerScript(script)
}

func (registry *debugRegistry) isActive() bool {
	if registry == nil {
		return false
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.active
}

func (registry *debugRegistry) sessionTargetForScript(script *Script) *debugTarget {
	if registry == nil || script == nil {
		return nil
	}

	scriptTarget := registry.registerScript(script)
	if scriptTarget == nil {
		return nil
	}
	if !registry.isActive() {
		return nil
	}
	if scriptTarget.currentSession() != nil {
		if !registry.isActive() {
			return nil
		}
		return scriptTarget
	}

	registry.mu.Lock()
	if !registry.active {
		registry.mu.Unlock()
		return nil
	}
	runtimeTarget := registry.runtimeTarget
	registry.mu.Unlock()
	if runtimeTarget != nil {
		session := runtimeTarget.currentSession()
		if session != nil && session.shouldAttachScript(script) {
			if !registry.isActive() {
				return nil
			}
			return runtimeTarget
		}
	}
	if !registry.isActive() {
		return nil
	}
	return scriptTarget
}

func (registry *debugRegistry) openSession(target *debugTarget) (*debugSession, error) {
	if registry == nil || target == nil {
		return nil, fmt.Errorf("debug target is inactive")
	}

	var oldSession *debugSession
	registry.mu.Lock()
	if !registry.active {
		registry.mu.Unlock()
		return nil, fmt.Errorf("debug target is inactive")
	}

	target.mu.Lock()
	if target.beforeSessionPublish != nil {
		target.beforeSessionPublish()
	}
	if !registry.active {
		target.mu.Unlock()
		registry.mu.Unlock()
		return nil, fmt.Errorf("debug target is inactive")
	}

	oldSession = target.session
	session := &debugSession{
		id:                target.id,
		target:            target,
		outbound:          make(chan []byte, 256),
		simulated:         make(map[int]bool),
		breakpointIntents: make([]debugBreakpointIntent, 0),
		nativeBreakpoints: make([][]byte, 0),
		initializers:      make([][]byte, 0),
	}
	session.breakpointRewrite = target.rewriteBreakpointByURL
	target.session = session
	target.mu.Unlock()
	registry.mu.Unlock()

	if oldSession != nil {
		_ = oldSession.Close()
	}
	return session, nil
}

func (registry *debugRegistry) targetForBreakpointURL(cdpURL string) *debugTarget {
	targets := registry.scriptTargetsSnapshot()

	for _, target := range targets {
		if debugSourceMatchesURL(debugScriptURL(target.script), cdpURL) {
			return target
		}
	}
	return nil
}

func (registry *debugRegistry) findTarget(id string) *debugTarget {
	if registry == nil {
		return nil
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.active {
		return nil
	}
	if id == debugRuntimeTargetID {
		return registry.runtimeTarget
	}
	return registry.targets[id]
}

func (registry *debugRegistry) refreshTargets() {
	if registry == nil {
		return
	}
	if registry.manager == nil || !registry.manager.isEnabled() {
		return
	}

	syncLock.Lock()
	defer syncLock.Unlock()
	for _, script := range Scripts {
		registry.registerScript(script)
	}
	for _, script := range RootScripts {
		registry.registerScript(script)
	}
}

func (registry *debugRegistry) descriptors(r *http.Request) []debugTargetDescriptor {
	return registry.scriptDescriptors(r)
}

func (registry *debugRegistry) scriptDescriptors(r *http.Request) []debugTargetDescriptor {
	registry.refreshTargets()

	targets := registry.scriptTargetsSnapshot()
	descriptors := make([]debugTargetDescriptor, 0, len(targets))
	for _, target := range targets {
		descriptors = append(descriptors, target.descriptor(r))
	}
	return descriptors
}

func (registry *debugRegistry) runtimeDescriptor(r *http.Request) debugTargetDescriptor {
	if registry == nil {
		return debugTargetDescriptor{}
	}

	registry.mu.Lock()
	target := registry.runtimeTarget
	registry.mu.Unlock()
	if target == nil {
		return debugTargetDescriptor{}
	}
	return target.descriptor(r)
}

func (registry *debugRegistry) scriptTargetsSnapshot() []*debugTarget {
	if registry == nil {
		return nil
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.active {
		return nil
	}

	targets := make([]*debugTarget, 0, len(registry.targets))
	for _, target := range registry.targets {
		targets = append(targets, target)
	}
	return targets
}

func (registry *debugRegistry) runtimeTargetSnapshot() *debugTarget {
	if registry == nil {
		return nil
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.active {
		return nil
	}
	return registry.runtimeTarget
}

func (registry *debugRegistry) deactivateAndSnapshot() ([]*debugTarget, *debugTarget) {
	if registry == nil {
		return nil, nil
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	targets := make([]*debugTarget, 0, len(registry.targets))
	for _, target := range registry.targets {
		targets = append(targets, target)
	}
	runtimeTarget := registry.runtimeTarget
	registry.active = false
	return targets, runtimeTarget
}

func (registry *debugRegistry) matchBreakpointSourceMap(cdpURL string, urlRegex string) (*SourceMap, int) {
	registry.refreshTargets()

	for _, target := range registry.scriptTargetsSnapshot() {
		sm, sourceIdx := target.matchOwnBreakpointSourceMap(cdpURL, urlRegex)
		if sm != nil && sourceIdx >= 0 {
			return sm, sourceIdx
		}
	}
	return nil, -1
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

// rewriteBreakpointByURL 通过 source map 将源文件行号反向映射为编译后行号。
func (target *debugTarget) rewriteBreakpointByURL(message []byte) debugBreakpointRewrite {
	rewrite := debugBreakpointRewrite{
		Intent:        parseDebugBreakpointIntent(message),
		NativeMessage: append([]byte(nil), message...),
	}
	if !bytes.Contains(message, []byte("setBreakpointByUrl")) {
		return rewrite
	}

	var msg map[string]interface{}
	if err := jsoniter.Unmarshal(message, &msg); err != nil {
		rewrite.Diagnostics = append(rewrite.Diagnostics, fmt.Sprintf("unmarshal setBreakpointByUrl failed: %s", err.Error()))
		log.Error("[V8 Debug] Unmarshal setBreakpointByUrl failed: %s", err.Error())
		return rewrite
	}

	method, _ := msg["method"].(string)
	if method != "Debugger.setBreakpointByUrl" {
		return rewrite
	}

	params, ok := msg["params"].(map[string]interface{})
	if !ok {
		rewrite.Diagnostics = append(rewrite.Diagnostics, "setBreakpointByUrl params is not map")
		log.Error("[V8 Debug] setBreakpointByUrl params is not map")
		return rewrite
	}

	lineNum, ok := debugNumberParam(params["lineNumber"])
	if !ok {
		rewrite.Diagnostics = append(rewrite.Diagnostics, "setBreakpointByUrl lineNumber is missing")
		return rewrite
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
		rewrite.Diagnostics = append(rewrite.Diagnostics, fmt.Sprintf("flat source map is nil for breakpoint: url=%s regex=%s", cdpURL, urlRegex))
		log.Warn("[V8 Debug] flat source map is nil for breakpoint. URL: %s, Regex: %s", cdpURL, urlRegex)
		return rewrite
	}
	if sourceIdx < 0 {
		rewrite.Diagnostics = append(rewrite.Diagnostics, fmt.Sprintf("source not matched in source map: url=%s regex=%s", cdpURL, urlRegex))
		log.Warn("[V8 Debug] source not matched in source map. URL: %s, Regex: %s, Sources: %v", cdpURL, urlRegex, sm.Sources)
		return rewrite
	}

	genLine, found := reverseMapSourcePosition(sm, sourceIdx, sourceLine)
	if !found {
		rewrite.Diagnostics = append(rewrite.Diagnostics, fmt.Sprintf("breakpoint reverse-map not found: %s:%d", sm.Sources[sourceIdx], sourceLine+1))
		log.Warn("[V8 Debug] breakpoint reverse-map not found: %s:%d (Line %d not mapped in source map)", sm.Sources[sourceIdx], sourceLine+1, sourceLine+1)
		return rewrite
	}

	log.Info("[V8 Debug] breakpoint reverse-map: %s:%d → compiled line %d",
		filepath.Base(sm.Sources[sourceIdx]), sourceLine+1, genLine+1)

	params["lineNumber"] = genLine
	params["columnNumber"] = 0

	modified, err := jsoniter.Marshal(msg)
	if err != nil {
		rewrite.Diagnostics = append(rewrite.Diagnostics, fmt.Sprintf("marshal modified breakpoint failed: %s", err.Error()))
		log.Error("[V8 Debug] Marshal modified breakpoint failed: %s", err.Error())
		return rewrite
	}
	rewrite.NativeMessage = modified
	return rewrite
}

func (target *debugTarget) matchBreakpointSourceMap(cdpURL string, urlRegex string) (*SourceMap, int) {
	if target.script != nil {
		return target.matchOwnBreakpointSourceMap(cdpURL, urlRegex)
	}
	if target.registry == nil {
		return nil, -1
	}
	return target.registry.matchBreakpointSourceMap(cdpURL, urlRegex)
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

func (target *debugTarget) host(r *http.Request) string {
	if r != nil && r.Host != "" {
		return r.Host
	}
	if target == nil || target.registry == nil {
		return ""
	}

	target.registry.mu.Lock()
	defer target.registry.mu.Unlock()
	return net.JoinHostPort(target.registry.host, strconv.Itoa(target.registry.port))
}
