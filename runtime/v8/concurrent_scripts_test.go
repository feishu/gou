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

func TestConcurrentReplaceScriptsZeroDowntime(t *testing.T) {
	// Seed initial registry
	initial := map[string]*Script{
		"test.zero.downtime": {ID: "test.zero.downtime", File: "v1.js"},
	}
	ReplaceScripts(initial)

	var wg sync.WaitGroup
	workers := 20
	iterations := 200

	// Readers continuously read test.zero.downtime - should NEVER hit 404
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sc, err := Select("test.zero.downtime")
				assert.NoError(t, err, "script must not experience 404 outage during atomic swap")
				assert.NotNil(t, sc)
			}
		}()
	}

	// Swapper simulates hot reloads with double-buffering
	wg.Add(1)
	go func() {
		defer wg.Done()
		for k := 0; k < 30; k++ {
			draft := map[string]*Script{
				"test.zero.downtime": {ID: "test.zero.downtime", File: fmt.Sprintf("v%d.js", k)},
				fmt.Sprintf("test.new.%d", k): {ID: fmt.Sprintf("test.new.%d", k), File: "new.js"},
			}
			ReplaceScripts(draft)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
}

func TestConcurrentReplaceRootScriptsZeroDowntime(t *testing.T) {
	// Seed initial root registry
	initial := map[string]*Script{
		"studio.zero.downtime": {ID: "studio.zero.downtime", File: "v1.js", Root: true},
	}
	ReplaceRootScripts(initial)

	var wg sync.WaitGroup
	workers := 20
	iterations := 200

	// Readers continuously read studio.zero.downtime - should NEVER hit 404
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sc, err := SelectRoot("studio.zero.downtime")
				assert.NoError(t, err, "root script must not experience 404 outage during atomic swap")
				assert.NotNil(t, sc)
			}
		}()
	}

	// Swapper simulates hot reloads with double-buffering
	wg.Add(1)
	go func() {
		defer wg.Done()
		for k := 0; k < 30; k++ {
			draft := map[string]*Script{
				"studio.zero.downtime": {ID: "studio.zero.downtime", File: fmt.Sprintf("v%d.js", k), Root: true},
				fmt.Sprintf("studio.new.%d", k): {ID: fmt.Sprintf("studio.new.%d", k), File: "new.js", Root: true},
			}
			ReplaceRootScripts(draft)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
}
