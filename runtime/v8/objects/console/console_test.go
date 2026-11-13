package console

import (
	"context"
	"os"
	"testing"

	"github.com/yaoapp/gou/runtime/v8/bridge"
	"rogchap.com/v8go"
)

func TestConsoleInDevelopmentMode(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("development")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()
	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("console.log", func(t *testing.T) {
		_, err := ctx.RunScript(`console.log("hello world", ["foo", "bar"], {"foo":"bar"})`, "")
		if err != nil {
			t.Fatal(err)
		}
		// Only verify that no error occurs, as we cannot reliably capture the output
	})

	t.Run("console.info", func(t *testing.T) {
		_, err := ctx.RunScript(`console.info("info message", 123)`, "")
		if err != nil {
			t.Fatal(err)
		}
		// Only verify that no error occurs
	})

	t.Run("console.warn", func(t *testing.T) {
		_, err := ctx.RunScript(`console.warn("warning message", true)`, "")
		if err != nil {
			t.Fatal(err)
		}
		// Only verify that no error occurs
	})

	t.Run("console.error", func(t *testing.T) {
		_, err := ctx.RunScript(`console.error("error message", null)`, "")
		if err != nil {
			t.Fatal(err)
		}
		// Only verify that no error occurs
	})
}

func TestConsoleInProductionMode(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("production")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()
	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("console.log should not output in production", func(t *testing.T) {
		// Cannot capture output, but console.log should not cause errors in production mode
		_, err := ctx.RunScript(`console.log("hello world")`, "")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("console.info, warn, error should still output in production", func(t *testing.T) {
		_, err := ctx.RunScript(`console.info("info in production")`, "")
		if err != nil {
			t.Fatal(err)
		}
		// Only verify that no error occurs
	})
}

func TestEmptyArgs(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("development")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()
	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ctx.RunScript(`console.log()`, "")
	if err == nil {
		t.Error("Expected error when calling console.log with no arguments")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

func TestExportObject(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("development")
	template := obj.ExportObject(iso)

	if template == nil {
		t.Fatal("ExportObject should return a valid object template")
	}

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	consoleObj, err := template.NewInstance(ctx)
	if err != nil {
		t.Fatal(err)
	}

	err = ctx.Global().Set("console", consoleObj)
	if err != nil {
		t.Fatal(err)
	}

	// Only test if execution succeeds
	_, err = ctx.RunScript(`console.log("exported object test")`, "")
	if err != nil {
		t.Fatal(err)
	}
	// Since we cannot reliably capture the output, we only verify no errors occur
}

func TestConsoleRetainedValuesZeroLeak(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("development")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	initialCount := ctx.RetainedValueCount()

	// 运行 200 次 console.log 调用，每次调用有 3 个参数和 this
	scriptVal, err := ctx.RunScript(`
		for (let i = 0; i < 200; i++) {
			console.log("hello", i, { foo: "bar" });
		}
	`, "console_leak.js")
	if err != nil {
		t.Fatal(err)
	}
	scriptVal.Release()

	afterCount := ctx.RetainedValueCount()
	if afterCount != initialCount {
		t.Fatalf("Expected retained values count to be %d, but got %d (leaked %d handles)",
			initialCount, afterCount, afterCount-initialCount)
	}
}

func TestConsoleProductionWithEnvVar(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	os.Setenv("YAO_V8_CONSOLE_LOG", "true")
	defer os.Unsetenv("YAO_V8_CONSOLE_LOG")

	obj := New("production")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()
	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !obj.ShouldLog(ctx) {
		t.Fatal("Expected ShouldLog to be true when YAO_V8_CONSOLE_LOG is true")
	}

	_, err = ctx.RunScript(`console.log("env var enabled log", 42, { test: true })`, "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsoleProductionWithContextDyeing(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("production")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()
	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Initially, should not log
	if obj.ShouldLog(ctx) {
		t.Fatal("Expected ShouldLog to be false by default in production")
	}

	// Bind Go context with debug and trace_id
	goCtx := context.WithValue(context.Background(), DebugContextKey, true)
	goCtx = context.WithValue(goCtx, "trace_id", "TRACE-ABC-123")
	unbind := bridge.BindGoContext(ctx, goCtx)
	defer unbind()

	if !obj.ShouldLog(ctx) {
		t.Fatal("Expected ShouldLog to be true when context has debug=true")
	}

	_, err = ctx.RunScript(`console.log("context dyed log message", { user: "admin" })`, "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsoleRetainedValuesZeroLeakInProduction(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	obj := New("production")
	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	err := obj.Set("console", ctx)
	if err != nil {
		t.Fatal(err)
	}

	initialCount := ctx.RetainedValueCount()

	// 运行 200 次 console.log 调用（被生产静默忽略），验证 info.Release() 确保零句柄泄露
	scriptVal, err := ctx.RunScript(`
		for (let i = 0; i < 200; i++) {
			console.log("hello silent", i, { foo: "bar" });
		}
	`, "console_leak_prod.js")
	if err != nil {
		t.Fatal(err)
	}
	scriptVal.Release()

	afterCount := ctx.RetainedValueCount()
	if afterCount != initialCount {
		t.Fatalf("Expected retained values count to be %d, but got %d (leaked %d handles in production)",
			initialCount, afterCount, afterCount-initialCount)
	}
}

