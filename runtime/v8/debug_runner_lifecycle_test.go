package v8

import "testing"

func TestDebugRunnerLeaseCloseIsOwnerCheckedAndIdempotent(t *testing.T) {
	session := &debugSession{id: "runtime", outbound: make(chan []byte, 1)}
	owner := &Runner{}
	other := &Runner{}
	closeCount := 0
	native := &fakeDebugNativeSession{
		close: func() error {
			closeCount++
			return nil
		},
	}
	if !session.attachNative(owner, native) {
		t.Fatal("expected owner native to attach")
	}

	lease := newDebugRunnerLease(session, owner)
	newDebugRunnerLease(session, other).Close()
	if closeCount != 0 {
		t.Fatalf("different runner lease closed native %d times", closeCount)
	}

	lease.Close()
	lease.Close()
	if closeCount != 1 {
		t.Fatalf("expected idempotent close once, got %d", closeCount)
	}
}
