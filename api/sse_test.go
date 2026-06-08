package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

func TestAuthenticatedSSERouteUsesFactory(t *testing.T) {
	id := "test.authenticated.sse.route.factory"
	t.Cleanup(func() {
		delete(APIs, id)
		ResetSSEHandlerFactoryForTest()
	})

	api := loadAuthenticatedSSERoute(t, id)
	router := gin.New()

	factoryCalled := false
	handlerCalled := false
	SetSSEHandlerFactory(func(path Path) gin.HandlerFunc {
		factoryCalled = true
		assert.Equal(t, "/events", path.Path)
		assert.NotNil(t, path.SSE)
		assert.Equal(t, "authenticated", path.SSE.Type)

		return func(c *gin.Context) {
			handlerCalled = true
			c.String(http.StatusOK, "native-sse")
		}
	})

	api.HTTP.Routes(router, "/")

	response := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/events", nil)
	require.NoError(t, err)
	router.ServeHTTP(response, req)

	assert.True(t, factoryCalled)
	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "native-sse", response.Body.String())
}

func TestAuthenticatedSSERouteWithoutFactoryReturns503(t *testing.T) {
	id := "test.authenticated.sse.route.without.factory"
	t.Cleanup(func() {
		delete(APIs, id)
		ResetSSEHandlerFactoryForTest()
	})
	ResetSSEHandlerFactoryForTest()

	api := loadAuthenticatedSSERoute(t, id)
	router := gin.New()
	api.HTTP.Routes(router, "/")

	response := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/events", nil)
	require.NoError(t, err)
	router.ServeHTTP(response, req)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "authenticated sse is unavailable")
}

func TestAuthenticatedSSERouteWithNilFactoryHandlerReturns503(t *testing.T) {
	id := "test.authenticated.sse.route.nil.factory.handler"
	t.Cleanup(func() {
		delete(APIs, id)
		ResetSSEHandlerFactoryForTest()
	})

	SetSSEHandlerFactory(func(path Path) gin.HandlerFunc {
		assert.Equal(t, "/events", path.Path)
		return nil
	})

	api := loadAuthenticatedSSERoute(t, id)
	router := gin.New()
	api.HTTP.Routes(router, "/")

	response := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/events", nil)
	require.NoError(t, err)
	router.ServeHTTP(response, req)

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "authenticated sse is unavailable")
}

func loadAuthenticatedSSERoute(t *testing.T, id string) *API {
	t.Helper()

	source := []byte(`{
		"name": "notification",
		"version": "1.0.0",
		"group": "/",
		"guard": "-",
		"paths": [
			{
				"path": "/events",
				"method": "GET",
				"process": "scripts.notification.Stream",
				"sse": {
					"type": "authenticated",
					"adapter": "notification"
				},
				"out": {
					"type": "text/event-stream"
				}
			}
		]
	}`)

	api, err := LoadSource("/apis/notification.http.yao", source, id)
	require.NoError(t, err)
	require.Len(t, api.HTTP.Paths, 1)
	require.NotNil(t, api.HTTP.Paths[0].SSE)
	return api
}
