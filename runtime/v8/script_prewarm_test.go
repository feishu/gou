package v8

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPrewarm(t *testing.T) {
	ResetScripts()
	defer ResetScripts()

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("test_prewarm_%d", i)
		source := fmt.Sprintf(`function Hello() { return "world_%d"; }`, i)
		s, err := MakeScript([]byte(source), id, 5*time.Second)
		assert.NoError(t, err)
		assert.Nil(t, s.GetCodeCache())
		Select(id) // Ensure registered in Scripts
		syncLock.Lock()
		Scripts[id] = s
		syncLock.Unlock()
	}

	err := Prewarm(4)
	assert.NoError(t, err)

	syncLock.RLock()
	defer syncLock.RUnlock()
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("test_prewarm_%d", i)
		s := Scripts[id]
		assert.NotNil(t, s)
		assert.NotNil(t, s.GetCodeCache(), "Script %s should have code cache after Prewarm", id)
	}
}
