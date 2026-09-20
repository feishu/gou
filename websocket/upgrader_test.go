package websocket

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcurrentUpgraderRegistry(t *testing.T) {
	var wg sync.WaitGroup
	workers := 10
	iterations := 20

	// 并发写
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				name := fmt.Sprintf("upgrader-%d-%d", workerID, i)
				u, err := NewUpgrader(name)
				assert.NoError(t, err)
				assert.NotNil(t, u)
			}
		}(w)
	}

	// 并发读
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				name := fmt.Sprintf("upgrader-%d-%d", workerID, i)
				_, _ = SelectUpgrader(name)
				_ = AllUpgraders()
			}
		}(w)
	}

	wg.Wait()

	// 最终验证全部读取安全
	all := AllUpgraders()
	assert.GreaterOrEqual(t, len(all), workers*iterations)
}
