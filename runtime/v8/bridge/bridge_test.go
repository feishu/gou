package bridge

import (
	"bytes"
	"fmt"
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
	if root == "" {
		t.Skip("GOU_TEST_APPLICATION is not set")
	}

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

func TestBridgeDirectUint8ArrayRoundTrip(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// 1. Empty byte slice
	emptyBytes := []byte{}
	val, err := JsValue(ctx, emptyBytes)
	if err != nil {
		t.Fatalf("unexpected error converting empty bytes: %v", err)
	}
	if !val.IsUint8Array() {
		t.Fatalf("expected Uint8Array for empty byte slice")
	}
	goVal, err := GoValue(val, ctx)
	if err != nil {
		t.Fatalf("unexpected error reading back empty bytes: %v", err)
	}
	resBytes, ok := goVal.([]byte)
	if !ok {
		t.Fatalf("expected []byte return, got %T", goVal)
	}
	if len(resBytes) != 0 {
		t.Fatalf("expected 0 length, got %d", len(resBytes))
	}

	// 2. Normal byte slice round trip
	origin := []byte("Hello, Gou Direct Memory Codec!")
	val, err = JsValue(ctx, origin)
	if err != nil {
		t.Fatalf("unexpected error converting bytes: %v", err)
	}
	if !val.IsUint8Array() {
		t.Fatalf("expected Uint8Array")
	}

	// Read in JS
	ctx.Global().Set("testArr", val)
	checkRes, err := ctx.RunScript("testArr.length === 31 && testArr[0] === 72", "check.js")
	if err != nil {
		t.Fatalf("unexpected script execution error: %v", err)
	}
	if !checkRes.Boolean() {
		t.Fatalf("JS failed to access Uint8Array elements properly")
	}

	// Read back to Go
	goVal, err = GoValue(val, ctx)
	if err != nil {
		t.Fatalf("unexpected error reading back bytes: %v", err)
	}
	resBytes, ok = goVal.([]byte)
	if !ok {
		t.Fatalf("expected []byte return, got %T", goVal)
	}
	if !bytes.Equal(origin, resBytes) {
		t.Fatalf("content mismatch: expected %q, got %q", origin, resBytes)
	}
}

// --- Benchmarks at Gou Bridge Level ---

func BenchmarkBridgeDirect_64KB(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		// Go -> JS via Gou JsValue
		val, err := JsValue(ctx, payload)
		if err != nil {
			b.Fatal(err)
		}
		// JS -> Go via Gou GoValue
		res, err := GoValue(val, ctx)
		if err != nil {
			b.Fatal(err)
		}
		_ = res.([]byte)
	}
}

func BenchmarkBridgeLegacy_64KB(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		// Simulating Gou's legacy byte-by-byte conversion
		newObj, err := ctx.RunScript(fmt.Sprintf("new Uint8Array(%d)", len(payload)), "")
		if err != nil {
			b.Fatal(err)
		}
		jsObj, err := newObj.AsObject()
		if err != nil {
			b.Fatal(err)
		}
		for j := 0; j < len(payload); j++ {
			jsObj.SetIdx(uint32(j), uint32(payload[j]))
		}

		// Simulating Gou's legacy byte-by-byte extraction
		length, err := jsObj.Get("length")
		if err != nil {
			b.Fatal(err)
		}
		var goValue []byte
		for j := uint32(0); j < length.Uint32(); j++ {
			v, err := jsObj.GetIdx(j)
			if err != nil {
				b.Fatal(err)
			}
			goValue = append(goValue, byte(v.Uint32()))
		}
		_ = goValue
	}
}

func BenchmarkBridgeDirect_1MB(b *testing.B) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		val, err := JsValue(ctx, payload)
		if err != nil {
			b.Fatal(err)
		}
		res, err := GoValue(val, ctx)
		if err != nil {
			b.Fatal(err)
		}
		_ = res.([]byte)
	}
}

