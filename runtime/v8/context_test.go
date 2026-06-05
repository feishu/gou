package v8

import (
	"context"
	"testing"
	"time"
)

func TestCallWithCancelsBeforeClose(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer Stop()

	script := &Script{
		ID:     "cancel-test",
		File:   "cancel-test.js",
		Source: "function Run() { while (true) {} }",
	}

	v8ctx, err := script.NewContext("", nil)
	if err != nil {
		t.Fatal(err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	start := time.Now()
	_, err = v8ctx.CallWith(cancelCtx, "Run")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if time.Since(start) > time.Second {
		t.Fatal("call with cancellation took too long")
	}

	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}
}
