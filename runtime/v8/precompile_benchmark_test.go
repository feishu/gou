package v8

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunnerScriptPrecompileCache(t *testing.T) {
	// Initialize runtime
	opt := &Option{
		MinSize:        2,
		MaxSize:        5,
		DefaultTimeout: 5000,
		ContextTimeout: 5000,
	}
	opt.Validate()

	err := Start(opt)
	assert.NoError(t, err)
	defer Stop()

	// 1. Create a script
	script := &Script{
		ID:     "test-precompile-1",
		File:   "test_precompile_1.js",
		Source: "function Add(a, b) { return a + b; }",
	}

	runner, err := dispatcher.Select(2 * time.Second)
	assert.NoError(t, err)
	assert.NotNil(t, runner)

	// First execution: cold compile, generates codeCache
	res := runner.ExecInvocation(runnerInvocation{
		script: script,
		method: "Add",
		args:   []interface{}{10, 20},
	})
	assert.Equal(t, 30, res)

	// Global codeCache must be generated
	codeCache := script.GetCodeCache()
	assert.NotNil(t, codeCache, "expected global CodeCache to be seeded on first compilation")

	// Runner local cache must be hit
	runner.mu.Lock()
	entry, ok := runner.scripts[script.ID]
	runner.mu.Unlock()
	assert.True(t, ok)
	assert.NotNil(t, entry)
	assert.Equal(t, script.Version(), entry.version)

	// Second execution on same runner: should be instant tier-1 hit
	res2 := runner.ExecInvocation(runnerInvocation{
		script: script,
		method: "Add",
		args:   []interface{}{100, 200},
	})
	assert.Equal(t, 300, res2)

	// 2. Select another runner from pool to verify Tier 2 (Bytecode Cache sharing)
	runner2, err := dispatcher.Select(2 * time.Second)
	assert.NoError(t, err)
	assert.NotNil(t, runner2)

	res3 := runner2.ExecInvocation(runnerInvocation{
		script: script,
		method: "Add",
		args:   []interface{}{1, 2},
	})
	assert.Equal(t, 3, res3)

	// Runner2 should now have cached its instance
	runner2.mu.Lock()
	entry2, ok2 := runner2.scripts[script.ID]
	runner2.mu.Unlock()
	assert.True(t, ok2)
	assert.NotNil(t, entry2)

	// 3. Test Invalidation on source update
	script.Source = "function Add(a, b) { return (a + b) * 2; }"
	script.InvalidateCache()
	assert.Nil(t, script.GetCodeCache())
	assert.Equal(t, uint64(1), script.Version())

	// Executing after invalidation should re-compile and produce new result
	res4 := runner.ExecInvocation(runnerInvocation{
		script: script,
		method: "Add",
		args:   []interface{}{10, 20},
	})
	assert.Equal(t, 60, res4)
}

func BenchmarkRunnerScriptExecWithCache(b *testing.B) {
	opt := &Option{
		MinSize:        10,
		MaxSize:        20,
		DefaultTimeout: 5000,
		ContextTimeout: 5000,
	}
	opt.Validate()

	err := Start(opt)
	if err != nil {
		b.Fatal(err)
	}
	defer Stop()

	script := &Script{
		ID:     "bench-script",
		File:   "bench_script.js",
		Source: `
			function Compute(x) {
				let sum = 0;
				for (let i = 0; i < 100; i++) {
					sum += (x + i);
				}
				return sum;
			}
		`,
	}

	// Warm up
	r, err := dispatcher.Select(2 * time.Second)
	if err != nil {
		b.Fatal(err)
	}
	r.ExecInvocation(runnerInvocation{
		script: script,
		method: "Compute",
		args:   []interface{}{1},
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		runner, err := dispatcher.Select(2 * time.Second)
		if err != nil {
			b.Fatal(err)
		}
		res := runner.ExecInvocation(runnerInvocation{
			script: script,
			method: "Compute",
			args:   []interface{}{i},
		})
		if res == nil {
			b.Fatal("nil response")
		}
	}
}

func BenchmarkRunnerScriptExecWithoutCache(b *testing.B) {
	opt := &Option{
		MinSize:        10,
		MaxSize:        20,
		DefaultTimeout: 5000,
		ContextTimeout: 5000,
	}
	opt.Validate()

	err := Start(opt)
	if err != nil {
		b.Fatal(err)
	}
	defer Stop()

	script := &Script{
		ID:     "bench-script-uncached",
		File:   "bench_script_uncached.js",
		Source: `
			function Compute(x) {
				let sum = 0;
				for (let i = 0; i < 100; i++) {
					sum += (x + i);
				}
				return sum;
			}
		`,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		script.InvalidateCache() // simulate cold compile every request as in old code
		runner, err := dispatcher.Select(2 * time.Second)
		if err != nil {
			b.Fatal(err)
		}
		res := runner.ExecInvocation(runnerInvocation{
			script: script,
			method: "Compute",
			args:   []interface{}{i},
		})
		if res == nil {
			b.Fatal("nil response")
		}
	}
}
