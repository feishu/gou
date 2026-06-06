package v8

import (
	"context"
	"errors"
	"testing"
	"time"

	"rogchap.com/v8go"
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

func TestNewContextPerformanceCallUsesInvocation(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	script := &Script{
		ID:   "context-call-test",
		File: "context-call-test.js",
		Source: `
function Echo(value) {
	return [value, __yao_data.SID, __yao_data.DATA.name]
}
function Boom() {
	throw new Error("boom")
}
`,
	}

	v8ctx, err := script.NewContext("sid-123", map[string]interface{}{"name": "alice"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := v8ctx.Call("Echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.([]interface{})
	if !ok {
		t.Fatalf("expected array result, got %T", res)
	}
	if len(data) != 3 || data[0] != "hello" || data[1] != "sid-123" || data[2] != "alice" {
		t.Fatalf("unexpected call result: %#v", data)
	}

	if res, err := v8ctx.Call("Boom"); err == nil {
		t.Fatalf("expected error result, got %#v", res)
	}

	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewContextStandardCallCloseUsesPool(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 2
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	script := &Script{
		ID:   "context-standard-call-test",
		File: "context-standard-call-test.js",
		Source: `
function Echo(value) {
	return [value, __yao_data.SID, __yao_data.DATA.name]
}
`,
	}

	v8ctx, err := script.NewContext("sid-123", map[string]interface{}{"name": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if v8ctx.Runner == nil {
		t.Fatal("standard NewContext should use a pooled runner")
	}

	res, err := v8ctx.Call("Echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.([]interface{})
	if !ok {
		t.Fatalf("expected array result, got %T", res)
	}
	if len(data) != 3 || data[0] != "hello" || data[1] != "sid-123" || data[2] != "alice" {
		t.Fatalf("unexpected call result: %#v", data)
	}
	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewContextWithFunctionUsesPooledContextIsolate(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 1
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	script := &Script{
		ID:   "context-with-function-test",
		File: "context-with-function-test.js",
		Source: `
function Run() {
	ssEvent("message", "hello")
	return true
}
`,
	}

	v8ctx, err := script.NewContext("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v8ctx.Close()

	called := false
	v8ctx.WithFunction("ssEvent", func(info *v8go.FunctionCallbackInfo) *v8go.Value {
		called = true
		return v8go.Null(info.Context().Isolate())
	})

	res, err := v8ctx.Call("Run")
	if err != nil {
		t.Fatal(err)
	}
	if res != true {
		t.Fatalf("expected true result, got %#v", res)
	}
	if !called {
		t.Fatal("expected bound function to be called")
	}
}

func TestCallWithPerformanceTimeoutDestroysRunner(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	script := &Script{
		ID:     "context-timeout-test",
		File:   "context-timeout-test.js",
		Source: "function Run() { while (true) {} }",
	}

	v8ctx, err := script.NewContext("", nil)
	if err != nil {
		t.Fatal(err)
	}
	oldID := v8ctx.Runner.id

	cancelCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = v8ctx.CallWith(cancelCtx, "Run")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v8ctx.Close(); err != nil {
		t.Fatal(err)
	}

	waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.Destroyed == 1 && stats.Active == 0 && stats.Idle == 0
	})

	next := &Script{
		ID:     "context-timeout-next-test",
		File:   "context-timeout-next-test.js",
		Source: "function Run() { return 42 }",
	}
	nextCtx, err := next.NewContext("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer nextCtx.Close()

	if nextCtx.Runner.id == oldID {
		t.Fatal("expected timeout runner to be destroyed before next context")
	}
	res, err := nextCtx.Call("Run")
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("expected 42, got %#v", res)
	}
}

func TestCallWithPerformanceTimeoutAndCloseRace(t *testing.T) {
	option := option()
	option.Mode = "performance"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer cleanupDispatcherForTest(t)

	script := &Script{
		ID:     "context-timeout-close-race-test",
		File:   "context-timeout-close-race-test.js",
		Source: "function Run() { while (true) {} }",
	}

	v8ctx, err := script.NewContext("", nil)
	if err != nil {
		t.Fatal(err)
	}

	cancelCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	errs := make(chan error, 2)
	go func() {
		_, err := v8ctx.CallWith(cancelCtx, "Run")
		if !errors.Is(err, context.DeadlineExceeded) {
			errs <- err
			return
		}
		errs <- nil
	}()
	go func() {
		time.Sleep(5 * time.Millisecond)
		errs <- v8ctx.Close()
	}()

	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	waitForDispatcherStats(t, func(stats DispatcherStats) bool {
		return stats.Destroyed == 1 && stats.Active == 0
	})
}
