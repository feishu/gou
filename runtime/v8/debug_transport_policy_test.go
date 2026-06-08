package v8

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDebugTransportPolicyRejectsNonLocalHTTPOrigin(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	req := httptest.NewRequest("GET", "http://127.0.0.1:9229/ws/runtime", nil)
	req.Header.Set("Origin", "https://example.com")
	if policy.CheckOrigin(req) {
		t.Fatal("expected non-local http origin to be rejected")
	}
}

func TestDebugTransportPolicyAllowsLocalAndDevtoolOrigins(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	origins := []string{"", "http://127.0.0.1:3000", "http://localhost:3000", "http://[::1]:3000", "devtools://devtools", "vscode-file://vscode-app"}
	for _, origin := range origins {
		req := httptest.NewRequest("GET", "http://127.0.0.1:9229/ws/runtime", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if !policy.CheckOrigin(req) {
			t.Fatalf("expected origin %q to be allowed", origin)
		}
	}
}

func TestDebugTransportPolicyTraceDefaultsOff(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	if policy.Trace.Enabled {
		t.Fatal("expected trace to default off")
	}
}

func TestDebugTransportPolicyTraceCanBeEnabled(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{
		Enabled:   true,
		Host:      "127.0.0.1",
		Port:      9229,
		Trace:     true,
		TracePath: "/tmp/custom-cdp.log",
	})
	if !policy.Trace.Enabled || policy.Trace.Path != "/tmp/custom-cdp.log" {
		t.Fatalf("unexpected trace policy: %+v", policy.Trace)
	}
}

func TestDebugTransportPolicySourceContentDefaultsOn(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	if !policy.ExposeSourceContent {
		t.Fatal("expected source content exposure to default on for local development inspect")
	}
}

func TestDebugTransportPolicySourceContentCanBeDisabled(t *testing.T) {
	exposeSourceContent := false
	policy := currentDebugTransportPolicy(Inspect{
		Enabled:             true,
		Host:                "127.0.0.1",
		Port:                9229,
		ExposeSourceContent: &exposeSourceContent,
	})
	if policy.ExposeSourceContent {
		t.Fatal("expected explicit source content disable to be honored")
	}
}

func TestDebugTransportPolicySourceContentCanBeDisabledFromJSON(t *testing.T) {
	var inspect Inspect
	if err := json.Unmarshal([]byte(`{"enabled":true,"host":"127.0.0.1","port":9229,"exposeSourceContent":false}`), &inspect); err != nil {
		t.Fatal(err)
	}

	policy := currentDebugTransportPolicy(inspect)
	if policy.ExposeSourceContent {
		t.Fatal("expected JSON source content disable to be honored")
	}
}

func TestConfigureCDPTraceSerializesConcurrentLogging(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "cdp_trace.log")
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	policy.Trace.Path = tracePath
	policy.Trace.Enabled = true
	disabled := policy
	disabled.Trace.Enabled = false

	configureCDPTrace(policy)
	t.Cleanup(func() {
		configureCDPTrace(disabled)
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				logCDP("test", []byte(`{"id":1,"method":"Runtime.evaluate"}`))
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if j%2 == 0 {
					configureCDPTrace(policy)
					continue
				}
				configureCDPTrace(disabled)
			}
		}()
	}
	wg.Wait()

	configureCDPTrace(policy)
	logCDP("test", []byte(`{"id":2,"method":"Runtime.evaluate"}`))
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("expected trace file to exist: %v", err)
	}
}
