package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigConnector(t *testing.T) {
	c := &Connector{}
	dsl := []byte(`{
		"name": "local",
		"label": "本地软件国密服务",
		"version": "1.0.0",
		"type": "config",
		"options": {
			"params": {
				"key": "0123456789abcdef",
				"iv": "fedcba9876543210",
				"kid": "default-sm4-1"
			}
		}
	}`)

	err := c.Register("local.conn.yao", "local", dsl)
	assert.Nil(t, err)
	assert.Equal(t, "local", c.ID())
	assert.True(t, c.Is(11))

	setting := c.Setting()
	assert.NotNil(t, setting)

	params, ok := setting["params"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "0123456789abcdef", params["key"])
	assert.Equal(t, "fedcba9876543210", params["iv"])
	assert.Equal(t, "default-sm4-1", params["kid"])

	// Test NonSQL contract
	assert.False(t, c.IsSQL())
	_, err = c.Query()
	assert.NotNil(t, err)
	_, err = c.Schema()
	assert.NotNil(t, err)
}
