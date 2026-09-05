package v8

import (
	"sync"
	"testing"
	"time"

	"github.com/yaoapp/gou/runtime/v8/store"
	"github.com/yaoapp/kun/log"
)

func TestSelectIsoStandard(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer Stop()

	iso, err := SelectIsoStandard(time.Millisecond * 100)
	if err != nil {
		t.Fatal(err)
	}
	defer iso.Dispose()
}

func TestSelectIsoStandardCompatibilityIsBounded(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 2
	option.DefaultTimeout = 50
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer Stop()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	isos := make(chan *store.Isolate, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			iso, err := SelectIsoStandard(50 * time.Millisecond)
			if err != nil {
				errs <- err
				return
			}
			isos <- iso
		}()
	}
	wg.Wait()
	close(isos)
	close(errs)

	successes := 0
	for iso := range isos {
		successes++
		iso.Dispose()
	}
	failures := 0
	for err := range errs {
		failures++
		if err == nil {
			t.Fatal("unexpected nil error")
		}
	}
	if successes > int(option.MaxSize) {
		t.Fatalf("expected at most %d successful isolates, got %d", option.MaxSize, successes)
	}
	if successes+failures != 4 {
		t.Fatalf("expected 4 completed calls, got successes=%d failures=%d", successes, failures)
	}

	stats := standardCompatStats()
	if stats.Created > uint64(option.MaxSize) {
		t.Fatalf("expected at most %d created isolates, got %d", option.MaxSize, stats.Created)
	}
}

func TestSelectIsoStandardWakeupLatency(t *testing.T) {
	option := option()
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 1
	option.DefaultTimeout = 500
	option.HeapSizeLimit = 4294967296

	prepareSetup(t, option)
	defer Stop()

	iso1, err := SelectIsoStandard(100 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	done := make(chan time.Duration, 1)

	// Background worker waits for isolate
	go func() {
		iso2, err := SelectIsoStandard(500 * time.Millisecond)
		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("wait failed: %v", err)
			return
		}
		iso2.Dispose()
		done <- elapsed
	}()

	// Hold for 2ms then release
	time.Sleep(2 * time.Millisecond)
	iso1.Dispose()

	elapsed := <-done
	t.Logf("Wakeup latency after 2ms release: %v", elapsed)
}

// go test -bench=BenchmarkSelectIsoStandard
// go test -bench=BenchmarkSelectIsoStandard -benchmem -benchtime=5s
// go test -bench=BenchmarkSelectIsoStandard -benchtime=5s
func BenchmarkSelectIsoStandard(b *testing.B) {
	option := option()
	option.Mode = "standard"
	option.HeapSizeLimit = 4294967296

	EnablePrecompile()
	if err := Start(option); err != nil {
		b.Fatal(err)
	}
	defer Stop()
	log.SetLevel(log.FatalLevel)

	b.ResetTimer()
	// run the Call function b.N times
	for n := 0; n < b.N; n++ {
		iso, err := SelectIsoStandard(500 * time.Millisecond)
		if err != nil {
			b.Fatal(err)
		}
		iso.Dispose()
	}
	b.StopTimer()
}

func BenchmarkSelectIsoStandardPB(b *testing.B) {
	option := option()
	option.Mode = "standard"
	option.HeapSizeLimit = 4294967296

	EnablePrecompile()
	if err := Start(option); err != nil {
		b.Fatal(err)
	}
	defer Stop()
	log.SetLevel(log.FatalLevel)

	b.ResetTimer()
	// run the Call function b.N times
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			iso, err := SelectIsoStandard(500 * time.Millisecond)
			if err != nil {
				b.Fatal(err)
			}
			iso.Dispose()
		}
	})
	b.StopTimer()
}

// BenchmarkSelectIsoContention simulates high contention on a small isolate pool (max=2)
func BenchmarkSelectIsoContention(b *testing.B) {
	option := option()
	option.Mode = "standard"
	option.MinSize = 1
	option.MaxSize = 2
	option.HeapSizeLimit = 4294967296

	EnablePrecompile()
	if err := Start(option); err != nil {
		b.Fatal(err)
	}
	defer Stop()
	log.SetLevel(log.FatalLevel)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			iso, err := SelectIsoStandard(200 * time.Millisecond)
			if err != nil {
				continue
			}
			// simulate brief micro-work
			time.Sleep(10 * time.Microsecond)
			iso.Dispose()
		}
	})
	b.StopTimer()
}
