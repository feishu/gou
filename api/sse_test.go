package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSourceParsesAuthenticatedSSEConfig(t *testing.T) {
	id := "test.authenticated.sse"
	t.Cleanup(func() {
		delete(APIs, id)
	})

	source := []byte(`{
		"name": "notification",
		"version": "1.0.0",
		"paths": [
			{
				"path": "/notifications",
				"method": "GET",
				"process": "scripts.notification.Stream",
				"sse": {
					"type": "authenticated",
					"adapter": "notification",
					"heartbeat": 15,
					"bus": {
						"connector": "redis",
						"channel": "yao:sse:notification"
					}
				}
			}
		]
	}`)

	api, err := LoadSource("/apis/notification.http.yao", source, id)
	require.NoError(t, err)
	require.Len(t, api.HTTP.Paths, 1)
	require.NotNil(t, api.HTTP.Paths[0].SSE)

	sse := api.HTTP.Paths[0].SSE
	assert.Equal(t, "authenticated", sse.Type)
	assert.Equal(t, "notification", sse.Adapter)
	assert.Equal(t, 15, sse.Heartbeat)
	assert.Equal(t, "redis", sse.Bus.Connector)
	assert.Equal(t, "yao:sse:notification", sse.Bus.Channel)
}
