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

// go test -bench=BenchmarkSelectIsoStandard
// go test -bench=BenchmarkSelectIsoStandard -benchmem -benchtime=5s
// go test -bench=BenchmarkSelectIsoStandard -benchtime=5s
func BenchmarkSelectIsoStandard(b *testing.B) {
	option := option()
	option.Mode = "standard"
	option.HeapSizeLimit = 4294967296

	b.ResetTimer()
	var t *testing.T
	prepare(t, option)
	defer Stop()
	log.SetLevel(log.FatalLevel)

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

	b.ResetTimer()
	var t *testing.T
	prepare(t, option)
	defer Stop()
	log.SetLevel(log.FatalLevel)

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
