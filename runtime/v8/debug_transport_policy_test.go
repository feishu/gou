package v8

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDebugTransportPolicyPreservesCurrentOriginBehavior(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	req := httptest.NewRequest("GET", "http://127.0.0.1:9229/ws/runtime", nil)
	req.Header.Set("Origin", "https://example.com")
	if !policy.CheckOrigin(req) {
		t.Fatal("expected behavior-preserving policy to allow any origin before behavior-change task")
	}
}

func TestDebugTransportPolicyPreservesCurrentTraceBehavior(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	if !policy.Trace.Enabled {
		t.Fatal("expected behavior-preserving trace policy to stay enabled before behavior-change task")
	}
	if policy.Trace.Path != "/tmp/cdp_trace.log" {
		t.Fatalf("unexpected trace path: %s", policy.Trace.Path)
	}
}

func TestDebugTransportPolicyPreservesCurrentSourceContentBehavior(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	if !policy.ExposeSourceContent {
		t.Fatal("expected behavior-preserving policy to expose source content before behavior-change task")
	}
}

func TestDebugTransportPolicyPreservesCurrentSessionReplacementBehavior(t *testing.T) {
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	if !policy.ReplaceSession {
		t.Fatal("expected behavior-preserving policy to replace existing sessions before behavior-change task")
	}
}

func TestConfigureCDPTraceSerializesConcurrentLogging(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "cdp_trace.log")
	policy := currentDebugTransportPolicy(Inspect{Enabled: true, Host: "127.0.0.1", Port: 9229})
	policy.Trace.Path = tracePath
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
