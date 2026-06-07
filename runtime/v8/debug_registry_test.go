package v8

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestDebugRegistryReturnsRuntimeAndScriptDescriptors(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	script := &Script{ID: "service.schedule", File: "/app/scripts/schedule.ts"}
	registry.registerScript(script)

	req := httptest.NewRequest("GET", "http://127.0.0.1:9229/json/list", nil)
	runtime := registry.runtimeDescriptor(req)
	if runtime.ID != debugRuntimeTargetID || runtime.Title != "Yao Runtime" {
		t.Fatalf("unexpected runtime descriptor: %+v", runtime)
	}

	descriptors := registry.scriptDescriptors(req)
	if len(descriptors) != 1 {
		t.Fatalf("expected one script descriptor, got %d", len(descriptors))
	}
	if descriptors[0].Title != "/app/scripts/schedule.ts" {
		t.Fatalf("unexpected script descriptor: %+v", descriptors[0])
	}
}

func TestDebugRegistryBreakpointRewriteFailurePassesThrough(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	target := registry.registerScript(&Script{ID: "service.schedule", File: "/app/scripts/schedule.ts"})

	original := []byte(`{"id":9,"method":"Debugger.setBreakpointByUrl","params":{"lineNumber":12,"url":"file:///missing.ts"}}`)
	rewrite := target.rewriteBreakpointByURL(original)

	if !bytes.Equal(rewrite.NativeMessage, original) {
		t.Fatalf("expected original native message to pass through, got %s", rewrite.NativeMessage)
	}
	if rewrite.Intent.URL != "file:///missing.ts" || rewrite.Intent.LineNumber != 12 {
		t.Fatalf("expected client intent to be preserved, got %+v", rewrite.Intent)
	}
	if len(rewrite.Diagnostics) == 0 {
		t.Fatal("expected diagnostics for failed rewrite")
	}
}

func TestDebugRegistrySessionTargetForScriptReturnsNilWhenInactive(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	session, err := registry.runtimeTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	registry.deactivateAndSnapshot()

	target := registry.sessionTargetForScript(&Script{ID: "service.schedule", File: "/app/scripts/schedule.ts"})
	if target != nil {
		t.Fatalf("expected inactive registry to return nil session target, got %#v", target)
	}
}

func TestDebugRegistrySessionTargetForScriptDoesNotReturnScriptTargetAfterDeactivate(t *testing.T) {
	registry := newDebugRegistry("127.0.0.1", 9229)
	script := &Script{ID: "service.schedule", File: "/app/scripts/schedule.ts"}
	scriptTarget := registry.registerScript(script)
	session, err := scriptTarget.openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	registry.deactivateAndSnapshot()

	target := registry.sessionTargetForScript(script)
	if target != nil {
		t.Fatalf("expected inactive registry not to return script target, got %#v", target)
	}
}
