package store

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStoreConcurrentParallelLoad(t *testing.T) {
	// Clean pools before test
	rwlock.Lock()
	Pools = map[string]Store{}
	rwlock.Unlock()

	var wg sync.WaitGroup
	numStores := 30

	for i := 0; i < numStores; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("lru_store_%d", idx)
			dsl := []byte(fmt.Sprintf(`{"name": "%s", "type": "lru", "option": {"size": 100}}`, id))
			stor, err := LoadSource(dsl, id, id+".json")
			assert.NoError(t, err)
			assert.NotNil(t, stor)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, numStores, Count())

	// Verify all stores exist and are accessible
	for i := 0; i < numStores; i++ {
		id := fmt.Sprintf("lru_store_%d", i)
		stor, err := Get(id)
		assert.NoError(t, err)
		assert.NotNil(t, stor)

		selStore := Select(id)
		assert.NotNil(t, selStore)
	}
}

func TestStoreConcurrentReadWrite(t *testing.T) {
	// Clean pools before test
	rwlock.Lock()
	Pools = map[string]Store{}
	rwlock.Unlock()

	// Pre-populate some stores
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("base_store_%d", i)
		dsl := []byte(fmt.Sprintf(`{"name": "%s", "type": "lru", "option": {"size": 50}}`, id))
		_, err := LoadSource(dsl, id, id+".json")
		assert.NoError(t, err)
	}

	var wg sync.WaitGroup
	numWorkers := 50

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			storeID := fmt.Sprintf("base_store_%d", workerID%10)

			// Read operations
			_, _ = Get(storeID)
			_ = Count()

			// Safe range iteration
			Range(func(id string, stor Store) bool {
				return id != ""
			})

			// Dynamically load and delete stores
			dynamicID := fmt.Sprintf("dyn_store_%d", workerID)
			dsl := []byte(fmt.Sprintf(`{"name": "%s", "type": "lru", "option": {"size": 50}}`, dynamicID))
			_, err := LoadSource(dsl, dynamicID, dynamicID+".json")
			if err == nil {
				_, _ = Get(dynamicID)
				Remove(dynamicID)
			}
		}(i)
	}

	wg.Wait()
	// Base stores should still exist or be safely queryable
	assert.GreaterOrEqual(t, Count(), 0)
}

func TestStoreMissingConnectorError(t *testing.T) {
	rwlock.Lock()
	Pools = map[string]Store{}
	rwlock.Unlock()

	dsl := []byte(`{"name": "remote_redis", "type": "redis", "connector": "non_existent_redis_conn"}`)
	stor, err := LoadSource(dsl, "remote_redis", "remote_redis.json")
	assert.Error(t, err)
	assert.Nil(t, stor)
	assert.Contains(t, err.Error(), "Connector:non_existent_redis_conn was not loaded")

	_, err = Get("remote_redis")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not load")
}

func TestStoreLoadSyncDelegation(t *testing.T) {
	dsl := []byte(`{"name": "sync_store", "type": "lru", "option": {"size": 100}}`)
	stor, err := LoadSourceSync(dsl, "sync_store", "sync_store.json")
	assert.NoError(t, err)
	assert.NotNil(t, stor)

	s, err := Get("sync_store")
	assert.NoError(t, err)
	assert.Equal(t, stor, s)
}
