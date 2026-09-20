package websocket

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHubConcurrentNextIDAndClients(t *testing.T) {
	h := newHub()
	go h.run()
	defer func() {
		h.interrupt <- 1
	}()

	const numGoroutines = 20
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	idMap := sync.Map{}

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				id := h.NextID()
				assert.True(t, id > 0)
				// 验证无任何重复 ID
				_, loaded := idMap.LoadOrStore(id, true)
				assert.False(t, loaded, "duplicate client ID generated")

				_ = h.Clients()
				_ = h.Nums()
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, uint32(numGoroutines*iterations), h.counter)
}
