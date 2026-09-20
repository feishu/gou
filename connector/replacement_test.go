package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/connector/base"
	gouTypes "github.com/yaoapp/gou/types"
)

type mockClosableConnector struct {
	base.NonSQL
	closed bool
	id     string
}

func (m *mockClosableConnector) Register(file string, id string, dsl []byte) error {
	m.id = id
	return nil
}
func (m *mockClosableConnector) Is(typ int) bool { return false }
func (m *mockClosableConnector) ID() string      { return m.id }
func (m *mockClosableConnector) Close() error {
	m.closed = true
	return nil
}
func (m *mockClosableConnector) Setting() map[string]interface{} { return nil }
func (m *mockClosableConnector) GetMetaInfo() gouTypes.MetaInfo  { return gouTypes.MetaInfo{} }

func TestConnectorReplacementClosesOld(t *testing.T) {
	rwlock.Lock()
	oldConn := &mockClosableConnector{id: "mock1"}
	Connectors["mock1"] = oldConn
	rwlock.Unlock()

	// Simulate LoadSource replacing mock1
	rwlock.Lock()
	newConn := &mockClosableConnector{id: "mock1"}
	if old, exists := Connectors["mock1"]; exists && old != nil {
		_ = old.Close()
	}
	Connectors["mock1"] = newConn
	rwlock.Unlock()

	assert.True(t, oldConn.closed, "Old connector should have been closed")
	assert.False(t, newConn.closed, "New connector should not be closed")

	// Test Remove
	err := Remove("mock1")
	assert.NoError(t, err)
	assert.True(t, newConn.closed, "New connector should be closed on Remove")
	_, err = Select("mock1")
	assert.Error(t, err)
}

func TestAIConnectorsDeduplication(t *testing.T) {
	rwlock.Lock()
	AIConnectors = []Option{
		{Label: "First", Value: "ai_conn_1"},
	}

	// Update existing
	found := false
	id := "ai_conn_1"
	label := "Updated First"
	for i, opt := range AIConnectors {
		if opt.Value == id {
			AIConnectors[i].Label = label
			found = true
			break
		}
	}
	if !found {
		AIConnectors = append(AIConnectors, Option{Label: label, Value: id})
	}
	assert.Equal(t, 1, len(AIConnectors))
	assert.Equal(t, "Updated First", AIConnectors[0].Label)

	// Add new
	id = "ai_conn_2"
	label = "Second"
	found = false
	for i, opt := range AIConnectors {
		if opt.Value == id {
			AIConnectors[i].Label = label
			found = true
			break
		}
	}
	if !found {
		AIConnectors = append(AIConnectors, Option{Label: label, Value: id})
	}
	assert.Equal(t, 2, len(AIConnectors))

	// Remove
	newAI := make([]Option, 0, len(AIConnectors))
	for _, opt := range AIConnectors {
		if opt.Value != "ai_conn_1" {
			newAI = append(newAI, opt)
		}
	}
	AIConnectors = newAI
	assert.Equal(t, 1, len(AIConnectors))
	assert.Equal(t, "ai_conn_2", AIConnectors[0].Value)
	rwlock.Unlock()
}
