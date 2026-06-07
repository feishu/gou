package v8

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDebugSessionStoresBreakpointIntentAndNativeCommandSeparately(t *testing.T) {
	session := &debugSession{
		id:           "runtime",
		outbound:     make(chan []byte, 8),
		simulated:    map[int]bool{},
		initializers: [][]byte{},
	}
	rewritten := []byte(`{"id":7,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":41,"columnNumber":0,"url":"file:///app/schedule.ts"}}`)
	session.breakpointRewrite = func(message []byte) debugBreakpointRewrite {
		return debugBreakpointRewrite{
			Intent: debugBreakpointIntent{
				ID:         7,
				Method:     "Debugger.setBreakpointByUrl",
				URL:        "file:///app/schedule.ts",
				LineNumber: 101,
				Raw:        append([]byte(nil), message...),
			},
			NativeMessage: rewritten,
		}
	}

	err := session.Dispatch([]byte(`{"id":7,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":101,"url":"file:///app/schedule.ts"}}`))
	if err != nil {
		t.Fatal(err)
	}

	intents := session.breakpointIntentsSnapshot()
	if len(intents) != 1 {
		t.Fatalf("expected one client breakpoint intent, got %d", len(intents))
	}
	if intents[0].LineNumber != 101 || intents[0].URL != "file:///app/schedule.ts" {
		t.Fatalf("unexpected intent: %+v", intents[0])
	}

	native := session.nativeBreakpointsSnapshot()
	if len(native) != 1 {
		t.Fatalf("expected one native breakpoint command, got %d", len(native))
	}
	if !bytes.Equal(native[0], rewritten) {
		t.Fatalf("expected rewritten native command %s, got %s", rewritten, native[0])
	}
}

func TestDebugSessionPendingOverflowReturnsError(t *testing.T) {
	session := &debugSession{
		id:       "runtime",
		outbound: make(chan []byte, 8),
		pending:  make([][]byte, 129),
	}
	transportClosed := false
	session.setTransportClose(func() error {
		transportClosed = true
		return nil
	})

	err := session.Dispatch([]byte(`{"method":"Runtime.evaluate"}`))
	if err == nil || !strings.Contains(err.Error(), "pending queue is full") {
		t.Fatalf("expected pending overflow error, got %v", err)
	}
	if !transportClosed {
		t.Fatal("expected transport to close on pending overflow")
	}
	if !session.isClosed() {
		t.Fatal("expected session to close on pending overflow")
	}

	err = session.Dispatch([]byte(`{"method":"Runtime.evaluate"}`))
	if err == nil || !strings.Contains(err.Error(), "debug session is closed") {
		t.Fatalf("expected closed session error, got %v", err)
	}
}

func TestDebugSessionPendingQueueAllowsLimit(t *testing.T) {
	session := &debugSession{
		id:       "runtime",
		outbound: make(chan []byte, 8),
		pending:  make([][]byte, 128),
	}

	err := session.Dispatch([]byte(`{"method":"Runtime.evaluate"}`))
	if err != nil {
		t.Fatal(err)
	}
	if session.isClosed() {
		t.Fatal("expected session to stay open")
	}
	if len(session.pending) != 129 {
		t.Fatalf("expected pending queue to accept one message, got %d", len(session.pending))
	}
}

func TestDebugSessionPendingOverflowAfterNativeReplacementReturnsError(t *testing.T) {
	originalNative := &fakeDebugNativeSession{}
	replacementNative := &fakeDebugNativeSession{}
	session := &debugSession{
		id:       "runtime",
		outbound: make(chan []byte, 8),
		pending:  make([][]byte, 129),
		native:   originalNative,
	}
	nativeRead := make(chan struct{})
	nativeReplaced := make(chan struct{})
	session.beforeNativeDispatchLock = func(native debugNativeSession) {
		if native != originalNative {
			t.Errorf("expected original native before dispatch lock, got %#v", native)
		}
		close(nativeRead)
		<-nativeReplaced
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Dispatch([]byte(`{"method":"Runtime.evaluate"}`))
	}()
	select {
	case <-nativeRead:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not read the active native session")
	}

	session.mu.Lock()
	session.native = replacementNative
	session.mu.Unlock()
	close(nativeReplaced)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "pending queue is full") {
			t.Fatalf("expected pending overflow error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch deadlocked after native replacement pending overflow")
	}
	if !session.isClosed() {
		t.Fatal("expected session to close on pending overflow")
	}
}

func TestDebugSessionRemoveBreakpointClearsIntentAndNativeCommand(t *testing.T) {
	session := &debugSession{
		id:       "runtime",
		outbound: make(chan []byte, 8),
	}

	err := session.Dispatch([]byte(`{"id":12,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":101,"url":"file:///app/schedule.ts"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(session.breakpointIntentsSnapshot()) != 1 {
		t.Fatal("expected one breakpoint intent")
	}
	if len(session.nativeBreakpointsSnapshot()) != 1 {
		t.Fatal("expected one native breakpoint command")
	}

	err = session.Dispatch([]byte(`{"id":13,"method":"Debugger.removeBreakpoint","params":{"breakpointId":"yao-temp-bp-12"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(session.breakpointIntentsSnapshot()) != 0 {
		t.Fatal("expected breakpoint intent to be removed")
	}
	if len(session.nativeBreakpointsSnapshot()) != 0 {
		t.Fatal("expected native breakpoint command to be removed")
	}
}
