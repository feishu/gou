package v8

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConcurrentScriptSelectAndExists(t *testing.T) {
	// Seed a script
	syncLock.Lock()
	Scripts["test.concurrent"] = &Script{ID: "test.concurrent", File: "test.js"}
	syncLock.Unlock()

	var wg sync.WaitGroup
	workers := 20
	iterations := 100

	// Readers calling Select and Exists
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				exists := Exists("test.concurrent")
				assert.True(t, exists)

				sc, err := Select("test.concurrent")
				assert.NoError(t, err)
				assert.NotNil(t, sc)

				_, _ = SelectRoot("test.concurrent")
			}
		}(i)
	}

	// Writers simulating concurrent reloading
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				id := fmt.Sprintf("test.dynamic.%d.%d", wid, j)
				syncLock.Lock()
				Scripts[id] = &Script{ID: id, File: id + ".js"}
				syncLock.Unlock()
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
