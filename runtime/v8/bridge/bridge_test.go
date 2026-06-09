package bridge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yaoapp/gou/application"
	"rogchap.com/v8go"
)

func call(ctx *v8go.Context, method string, args ...interface{}) (interface{}, error) {

	global := ctx.Global()
	jsArgs, err := JsValues(ctx, args)
	if err != nil {
		return nil, err
	}
	defer FreeJsValues(jsArgs)

	jsRes, err := global.MethodCall(method, Valuers(jsArgs)...)
	if err != nil {
		return nil, err
	}

	goRes, err := GoValue(jsRes, ctx)
	if err != nil {
		return nil, err
	}

	return goRes, nil
}

func TestJsErrorFallsBackWhenGlobalErrorLookupFails(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	_, err := ctx.RunScript(`
		Object.defineProperty(globalThis, "Error", {
			configurable: true,
			get() { throw "error constructor failure"; },
		});
	`, "")
	if err != nil {
		t.Fatal(err)
	}

	jsErr := JsError(ctx, "fallback message")
	if jsErr == nil {
		t.Fatal("expected fallback error value")
	}

	obj, err := jsErr.AsObject()
	if err != nil {
		t.Fatal(err)
	}

	message, err := obj.Get("message")
	if err != nil {
		t.Fatal(err)
	}

	if message.String() != "fallback message" {
		t.Fatalf("expected fallback message, got %q", message.String())
	}
}

func prepare(t *testing.T) *v8go.Context {

	root := os.Getenv("GOU_TEST_APPLICATION")

	// Load app
	app, err := application.OpenFromDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	application.Load(app)

	file := filepath.Join("scripts", "runtime", "bridge.js")
	source, err := app.Read(file)
	if err != nil {
		t.Fatal(err)
	}

	iso := v8go.NewIsolate()
	ctx := v8go.NewContext(iso)
	_, err = ctx.RunScript(string(source), file)
	if err != nil {
		t.Fatal(err)
	}

	return ctx
}

func close(ctx *v8go.Context) {
	ctx.Close()
	ctx.Isolate().Dispose()
}
