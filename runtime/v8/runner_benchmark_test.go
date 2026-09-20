package v8

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunnerContextRecycle(t *testing.T) {
	opt := option()
	opt.Mode = "performance"
	opt.MinSize = 1
	opt.MaxSize = 1
	opt.DefaultTimeout = 5000
	opt.HeapSizeLimit = 4294967296

	prepareSetup(t, opt)
	defer cleanupDispatcherForTest(t)

	// Create a test script in memory
	scriptSource := []byte(`
		function TestRecycle(arg) {
			return { echo: arg, timestamp: Date.now() };
		}
	`)
	script, err := MakeScript(scriptSource, "test_recycle.js", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Record baseline stats before executions
	baselineRunner, err := dispatcher.Select(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	initialNativeContexts := baselineRunner.iso.GetHeapStatistics().NumberOfNativeContexts
	runnerID := baselineRunner.id

	// Run 100 consecutive executions on this runner
	for i := 0; i < 100; i++ {
		var runner *Runner
		if i == 0 {
			runner = baselineRunner
		} else {
			var selErr error
			runner, selErr = dispatcher.Select(2 * time.Second)
			if selErr != nil {
				t.Fatalf("failed to select runner at iteration %d: %v", i, selErr)
			}
			assert.Equal(t, runnerID, runner.id, "runner should be recycled and reused, not destroyed")
		}

		res := runner.ExecInvocation(runnerInvocation{
			script: script,
			method: "TestRecycle",
			args:   []interface{}{i},
			sid:    fmt.Sprintf("session-%d", i),
			global: map[string]interface{}{"call_idx": i},
		})

		resMap, ok := res.(map[string]interface{})
		assert.True(t, ok, "expected map[string]interface{} result")
		assert.Equal(t, i, int(resMap["echo"].(float64)))
	}

	// Select the runner once more to inspect heap stats
	runner, err := dispatcher.Select(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer destroyLeasedDispatcherRunners([]*Runner{runner})

	// Critical check: NativeContexts must remain at baseline (2) instead of accumulating to 100+!
	stat := runner.iso.GetHeapStatistics()
	assert.Equal(t, initialNativeContexts, stat.NumberOfNativeContexts, "NumberOfNativeContexts must remain at baseline with recycled context")
	assert.LessOrEqual(t, stat.NumberOfNativeContexts, uint64(2))
}

func BenchmarkRunner_ExecInvocation(b *testing.B) {
	opt := option()
	opt.Mode = "performance"
	opt.MinSize = 1
	opt.MaxSize = 1
	opt.DefaultTimeout = 5000
	opt.HeapSizeLimit = 4294967296

	EnablePrecompile()
	if err := Start(opt); err != nil {
		b.Fatal(err)
	}
	defer Stop()

	scriptSource := []byte(`
		function Add(a, b) {
			return a + b;
		}
	`)
	script, err := MakeScript(scriptSource, "bench_add.js", 5*time.Second)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		runner, err := dispatcher.Select(2 * time.Second)
		if err != nil {
			b.Fatal(err)
		}
		res := runner.ExecInvocation(runnerInvocation{
			script: script,
			method: "Add",
			args:   []interface{}{i, 1},
			sid:    "bench-session",
		})
		if res == nil {
			b.Fatal("unexpected nil response")
		}
	}
}
