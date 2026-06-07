package v8

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/kun/log"
)

var errDebugPendingOverflow = fmt.Errorf("debug session pending queue is full")

type debugBreakpointIntent struct {
	ID         int
	Method     string
	URL        string
	URLRegex   string
	LineNumber int
	Raw        []byte
}

type debugBreakpointRewrite struct {
	Intent        debugBreakpointIntent
	NativeMessage []byte
	Diagnostics   []string
}

type debugBreakpointRewriteFunc func(message []byte) debugBreakpointRewrite

type debugSession struct {
	mu                sync.Mutex
	nativeMu          sync.Mutex
	id                string
	target            *debugTarget
	native            debugNativeSession
	runner            *Runner
	pending           [][]byte
	outbound          chan []byte
	transportClose    func() error
	closed            bool
	simulated         map[int]bool
	simulatedMu       sync.Mutex
	breakpointsMu     sync.Mutex
	breakpointIntents []debugBreakpointIntent
	nativeBreakpoints [][]byte
	breakpointRewrite debugBreakpointRewriteFunc
	initializers      [][]byte
	initializersMu    sync.Mutex

	beforeNativeDispatchLock func(native debugNativeSession)
}

type debugNativeSession interface {
	Dispatch(message []byte) error
	Close() error
}

func (session *debugSession) shouldAttachScript(script *Script) bool {
	if session == nil || script == nil {
		return false
	}
	if session.target == nil || session.target.registry == nil {
		return true
	}

	target := session.target.registry.registerScript(script)
	if target == nil {
		return false
	}
	if !session.target.registry.isActive() {
		return false
	}
	return session.hasBreakpointForTarget(target) && session.target.registry.isActive()
}

func (session *debugSession) Dispatch(message []byte) error {
	// 拦截对第0行（lineNumber == 0）的 setBreakpointByUrl，避免 VSCode debugger 隐式入口断点卡死黑屏
	if bytes.Contains(message, []byte("setBreakpointByUrl")) {
		var msg struct {
			ID     int                    `json:"id"`
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := jsoniter.Unmarshal(message, &msg); err == nil && msg.Method == "Debugger.setBreakpointByUrl" {
			lineNumber, hasLineNumber := debugNumberParam(msg.Params["lineNumber"])
			if hasLineNumber && int(lineNumber) == 0 {
				response, _ := jsoniter.Marshal(map[string]interface{}{
					"id": msg.ID,
					"result": map[string]interface{}{
						"breakpointId": fmt.Sprintf("yao-ignored-bp-%d", msg.ID),
						"locations":    []interface{}{},
					},
				})
				session.sendRaw(response)
				log.Info("[V8 Debug] Ignored setBreakpointByUrl for line 0 (id: %d)", msg.ID)
				return nil
			}
		}
	}

	message = session.rememberBreakpoint(message)

	logCDP("Client->V8", message)

	// 记录调试器初始化状态指令，用于重连时恢复调试状态
	var msg struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := jsoniter.Unmarshal(message, &msg); err == nil && msg.Method != "" {
		isInit := false
		switch msg.Method {
		case "Debugger.enable", "Runtime.enable", "Profiler.enable",
			"Debugger.setPauseOnExceptions", "Debugger.setAsyncCallStackDepth",
			"Debugger.setBlackboxPatterns", "Debugger.setSkipAllPauses",
			"Network.enable", "NodeWorker.enable":
			isInit = true
		}
		if isInit {
			session.initializersMu.Lock()
			found := false
			for i, initBp := range session.initializers {
				var oldMsg struct {
					Method string `json:"method"`
				}
				if err := jsoniter.Unmarshal(initBp, &oldMsg); err == nil && oldMsg.Method == msg.Method {
					session.initializers[i] = append([]byte(nil), message...)
					found = true
					break
				}
			}
			if !found {
				session.initializers = append(session.initializers, append([]byte(nil), message...))
			}
			session.initializersMu.Unlock()
		}
	}

	// 移除断点指令
	if bytes.Contains(message, []byte("Debugger.removeBreakpoint")) {
		var removeMsg struct {
			Params struct {
				BreakpointID string `json:"breakpointId"`
			} `json:"params"`
		}
		if err := jsoniter.Unmarshal(message, &removeMsg); err == nil && removeMsg.Params.BreakpointID != "" {
			bpID := removeMsg.Params.BreakpointID
			if strings.HasPrefix(bpID, "yao-temp-bp-") {
				reqIDStr := strings.TrimPrefix(bpID, "yao-temp-bp-")
				if reqID, err := strconv.Atoi(reqIDStr); err == nil {
					session.removeBreakpointByRequestID(reqID)
				}
			}
		}
	}

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return fmt.Errorf("debug session is closed")
	}
	native := session.native
	if native == nil {
		if msg.ID > 0 {
			simulate := false
			var result interface{}

			switch msg.Method {
			case "Runtime.enable", "NodeWorker.enable", "Profiler.enable", "Runtime.runIfWaitingForDebugger", "Debugger.setPauseOnExceptions":
				simulate = true
				result = map[string]interface{}{}
			case "Debugger.enable":
				simulate = true
				result = map[string]interface{}{
					"debuggerId": "yao-debugger-id",
				}
			case "Debugger.setBreakpointByUrl":
				simulate = true
				result = map[string]interface{}{
					"breakpointId": fmt.Sprintf("yao-temp-bp-%d", msg.ID),
					"locations":    []interface{}{},
				}
			}

			if simulate {
				session.markSimulated(msg.ID)

				response, _ := jsoniter.Marshal(map[string]interface{}{
					"id":     msg.ID,
					"result": result,
				})

				err := session.enqueuePendingLocked(message)
				session.mu.Unlock()
				if err != nil {
					return session.closeOnPendingError(err)
				}

				session.sendRaw(response)
				return nil
			}
		}

		err := session.enqueuePendingLocked(message)
		session.mu.Unlock()
		return session.closeOnPendingError(err)
	}
	beforeNativeDispatchLock := session.beforeNativeDispatchLock
	session.mu.Unlock()

	if beforeNativeDispatchLock != nil {
		beforeNativeDispatchLock(native)
	}

	session.nativeMu.Lock()

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		session.nativeMu.Unlock()
		return fmt.Errorf("debug session is closed")
	}
	if session.native != native {
		err := session.enqueuePendingLocked(message)
		session.mu.Unlock()
		session.nativeMu.Unlock()
		return session.closeOnPendingError(err)
	}
	session.mu.Unlock()

	err := native.Dispatch(message)
	session.nativeMu.Unlock()
	return err
}

func (session *debugSession) rememberBreakpoint(message []byte) []byte {
	if !bytes.Contains(message, []byte("Debugger.setBreakpoint")) {
		return message
	}

	var bp struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := jsoniter.Unmarshal(message, &bp); err != nil || bp.Method == "" {
		return message
	}
	if !strings.HasPrefix(bp.Method, "Debugger.setBreakpoint") {
		return message
	}

	intent := debugBreakpointIntent{}
	nativeMessage := append([]byte(nil), message...)
	if session.breakpointRewrite != nil {
		rewrite := session.breakpointRewrite(message)
		intent = rewrite.Intent
		if len(rewrite.NativeMessage) > 0 {
			nativeMessage = append([]byte(nil), rewrite.NativeMessage...)
		}
	}
	if isEmptyBreakpointIntent(intent) {
		intent = parseDebugBreakpointIntent(message)
	}
	if len(intent.Raw) == 0 {
		intent.Raw = append([]byte(nil), message...)
	}

	session.breakpointsMu.Lock()
	defer session.breakpointsMu.Unlock()
	session.upsertBreakpointLocked(intent, nativeMessage)
	return nativeMessage
}

func parseDebugBreakpointIntent(message []byte) debugBreakpointIntent {
	var msg struct {
		ID     int                    `json:"id"`
		Method string                 `json:"method"`
		Params map[string]interface{} `json:"params"`
	}
	if err := jsoniter.Unmarshal(message, &msg); err != nil || msg.Method == "" {
		return debugBreakpointIntent{}
	}
	if !strings.HasPrefix(msg.Method, "Debugger.setBreakpoint") {
		return debugBreakpointIntent{}
	}

	intent := debugBreakpointIntent{
		ID:     msg.ID,
		Method: msg.Method,
		Raw:    append([]byte(nil), message...),
	}
	if url, ok := msg.Params["url"].(string); ok {
		intent.URL = url
	}
	if urlRegex, ok := msg.Params["urlRegex"].(string); ok {
		intent.URLRegex = urlRegex
	}
	if lineNumber, ok := debugNumberParam(msg.Params["lineNumber"]); ok {
		intent.LineNumber = int(lineNumber)
	}
	return intent
}

func isEmptyBreakpointIntent(intent debugBreakpointIntent) bool {
	return intent.ID == 0 && intent.Method == "" && intent.URL == "" && intent.URLRegex == "" && intent.LineNumber == 0 && len(intent.Raw) == 0
}

func (session *debugSession) upsertBreakpointLocked(intent debugBreakpointIntent, nativeMessage []byte) {
	if intent.ID > 0 {
		for i, existing := range session.breakpointIntents {
			if existing.ID == intent.ID {
				session.breakpointIntents[i] = cloneBreakpointIntent(intent)
				session.setNativeBreakpointLocked(i, nativeMessage)
				return
			}
		}
	}

	for i, existing := range session.breakpointIntents {
		if bytes.Equal(existing.Raw, intent.Raw) || (i < len(session.nativeBreakpoints) && bytes.Equal(session.nativeBreakpoints[i], nativeMessage)) {
			return
		}
	}

	session.breakpointIntents = append(session.breakpointIntents, cloneBreakpointIntent(intent))
	session.nativeBreakpoints = append(session.nativeBreakpoints, append([]byte(nil), nativeMessage...))
}

func (session *debugSession) setNativeBreakpointLocked(index int, nativeMessage []byte) {
	for len(session.nativeBreakpoints) <= index {
		session.nativeBreakpoints = append(session.nativeBreakpoints, nil)
	}
	session.nativeBreakpoints[index] = append([]byte(nil), nativeMessage...)
}

func (session *debugSession) removeBreakpointByRequestID(reqID int) {
	session.breakpointsMu.Lock()
	defer session.breakpointsMu.Unlock()

	for i, intent := range session.breakpointIntents {
		if intent.ID == reqID {
			session.breakpointIntents = append(session.breakpointIntents[:i], session.breakpointIntents[i+1:]...)
			if i < len(session.nativeBreakpoints) {
				session.nativeBreakpoints = append(session.nativeBreakpoints[:i], session.nativeBreakpoints[i+1:]...)
			}
			return
		}
	}
}

func (session *debugSession) breakpointIntentsSnapshot() []debugBreakpointIntent {
	session.breakpointsMu.Lock()
	defer session.breakpointsMu.Unlock()

	intents := make([]debugBreakpointIntent, len(session.breakpointIntents))
	for i, intent := range session.breakpointIntents {
		intents[i] = cloneBreakpointIntent(intent)
	}
	return intents
}

func (session *debugSession) nativeBreakpointsSnapshot() [][]byte {
	session.breakpointsMu.Lock()
	defer session.breakpointsMu.Unlock()

	nativeBreakpoints := make([][]byte, len(session.nativeBreakpoints))
	for i, message := range session.nativeBreakpoints {
		nativeBreakpoints[i] = append([]byte(nil), message...)
	}
	return nativeBreakpoints
}

func cloneBreakpointIntent(intent debugBreakpointIntent) debugBreakpointIntent {
	intent.Raw = append([]byte(nil), intent.Raw...)
	return intent
}

func (session *debugSession) attachNative(runner *Runner, native debugNativeSession) bool {
	session.nativeMu.Lock()
	defer session.nativeMu.Unlock()

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		_ = native.Close()
		return false
	}
	if session.native != nil && session.runner != nil && session.runner != runner && !session.prefersRunner(runner, session.runner) {
		session.mu.Unlock()
		_ = native.Close()
		log.Warn("[V8 Debug] native session is already attached to another runner")
		return false
	}
	oldNative := session.native
	session.runner = runner
	session.native = native
	pending := session.pending
	session.pending = nil
	session.mu.Unlock()

	if oldNative != nil {
		_ = oldNative.Close()
	}

	// 1. 恢复调试初始化状态指令
	session.initializersMu.Lock()
	restoreInits := make([][]byte, len(session.initializers))
	copy(restoreInits, session.initializers)
	session.initializersMu.Unlock()

	for _, initMsg := range restoreInits {
		var initCmd struct {
			ID int `json:"id"`
		}
		if err := jsoniter.Unmarshal(initMsg, &initCmd); err == nil && initCmd.ID > 0 {
			session.markSimulated(initCmd.ID)
		}
		if err := native.Dispatch(initMsg); err != nil {
			log.Warn("[V8 Debug] restore initializer failed: %s", err.Error())
		}
	}

	// 2. 恢复之前的历史断点到新的 native 通道中
	restoreBps := session.nativeBreakpointsSnapshot()

	for _, bpMessage := range restoreBps {
		var bp struct {
			ID int `json:"id"`
		}
		if err := jsoniter.Unmarshal(bpMessage, &bp); err == nil && bp.ID > 0 {
			session.markSimulated(bp.ID)
		}
		if err := native.Dispatch(bpMessage); err != nil {
			log.Warn("[V8 Debug] restore breakpoint failed: %s", err.Error())
		}
	}

	for _, message := range pending {
		if err := native.Dispatch(message); err != nil {
			log.Warn("[V8 Debug] pending dispatch failed: %s", err.Error())
			return true
		}
	}
	return true
}

func (session *debugSession) prefersRunner(next *Runner, current *Runner) bool {
	nextScript := runnerScript(next)
	currentScript := runnerScript(current)
	if nextScript == nil || currentScript == nil {
		return false
	}

	nextMatches := session.shouldAttachScript(nextScript)
	currentMatches := session.shouldAttachScript(currentScript)
	return nextMatches && !currentMatches
}

func runnerScript(runner *Runner) *Script {
	if runner == nil {
		return nil
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.script
}

func (session *debugSession) detachNative() {
	session.detachNativeForRunner(nil)
}

func (session *debugSession) detachNativeForRunner(runner *Runner) {
	session.nativeMu.Lock()
	defer session.nativeMu.Unlock()

	session.mu.Lock()
	if runner != nil && session.runner != runner {
		session.mu.Unlock()
		return
	}
	native := session.native
	session.native = nil
	session.runner = nil
	session.mu.Unlock()
	if native != nil {
		_ = native.Close()
	}
}

func (session *debugSession) enqueuePendingLocked(message []byte) error {
	if len(session.pending) > 128 {
		return errDebugPendingOverflow
	}
	session.pending = append(session.pending, append([]byte(nil), message...))
	return nil
}

func (session *debugSession) closeOnPendingError(err error) error {
	if errors.Is(err, errDebugPendingOverflow) {
		_ = session.Close()
	}
	return err
}

func (session *debugSession) markSimulated(id int) {
	if id <= 0 {
		return
	}
	session.simulatedMu.Lock()
	if session.simulated == nil {
		session.simulated = map[int]bool{}
	}
	session.simulated[id] = true
	session.simulatedMu.Unlock()
}

func (session *debugSession) setTransportClose(close func() error) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		if close != nil {
			_ = close()
		}
		return
	}
	session.transportClose = close
	session.mu.Unlock()
}

func (session *debugSession) Close() error {
	session.nativeMu.Lock()
	defer session.nativeMu.Unlock()

	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil
	}
	session.closed = true
	native := session.native
	target := session.target
	transportClose := session.transportClose
	session.native = nil
	session.runner = nil
	session.transportClose = nil
	close(session.outbound)
	session.mu.Unlock()

	if native != nil {
		_ = native.Close()
	}
	if transportClose != nil {
		_ = transportClose()
	}
	if target != nil {
		target.mu.Lock()
		if target.session == session {
			target.session = nil
		}
		target.mu.Unlock()
	}
	return nil
}

func (session *debugSession) isClosed() bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.closed
}

func (session *debugSession) SendResponse(_ int, message []byte) {
	session.send(message)
}

func (session *debugSession) SendNotification(message []byte) {
	session.send(message)
}

func (session *debugSession) Flush() {}

func (session *debugSession) sendRaw(message []byte) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	outbound := session.outbound
	payload := append([]byte(nil), message...)

	select {
	case outbound <- payload:
		session.mu.Unlock()
	default:
		session.mu.Unlock()
		_ = session.Close()
	}
}

func (session *debugSession) send(message []byte) {
	var msg struct {
		ID int `json:"id"`
	}
	if err := jsoniter.Unmarshal(message, &msg); err == nil && msg.ID > 0 {
		session.simulatedMu.Lock()
		if session.simulated[msg.ID] {
			delete(session.simulated, msg.ID)
			session.simulatedMu.Unlock()
			logCDP("V8->Client (Dropped duplicate response)", message)
			return
		}
		session.simulatedMu.Unlock()
	}
	session.sendRaw(message)
}
