package process

import (
	"context"
	"testing"
	"time"
)

func BenchmarkProcessExecuteSync(b *testing.B) {
	prepare(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := New("unit.test.prepare", "foo", "bar")
		err := p.Execute()
		if err != nil {
			b.Fatal(err)
		}
		p.Dispose()
	}
}

func BenchmarkProcessExecuteWithTimeout(b *testing.B) {
	prepare(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p := New("unit.test.prepare", "foo", "bar").WithContext(ctx)
		err := p.Execute()
		cancel()
		if err != nil {
			b.Fatal(err)
		}
		p.Dispose()
	}
}
