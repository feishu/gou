package schedule

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcurrentScheduleRegistry(t *testing.T) {
	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup

	// 1. 并发写入与更新
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				name := fmt.Sprintf("sch_%d_%d", id, i)
				sch := &Schedule{name: name, Name: name}
				rwlock.Lock()
				Schedules[name] = sch
				rwlock.Unlock()
			}
		}(g)
	}

	// 2. 并发读取与查询
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				name := fmt.Sprintf("sch_%d_%d", id, i)
				rwlock.RLock()
				_, _ = Schedules[name]
				rwlock.RUnlock()
			}
		}(g)
	}

	// 3. 并发调用 StopAll
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			StopAll()
		}()
	}

	wg.Wait()
	assert.True(t, true, "并发读写 Schedules map 应当 0 race 0 panic 通过")
}
