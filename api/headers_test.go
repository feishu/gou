package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSetResponseHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	path := Path{
		Out: Out{
			Type: "application/json",
			Headers: map[string]string{
				"X-User-Name":  "{{user.name}}",
				"X-User-Role":  "?:user.role",
				"X-Request-Id": "123456",
				"Content-Type": "text/plain",
			},
		},
	}

	resp := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "alice",
			"role": "admin",
		},
	}

	ct := path.setResponseHeaders(c, resp, "application/json")
	assert.Equal(t, "text/plain", ct)
	assert.Equal(t, "alice", c.Writer.Header().Get("X-User-Name"))
	assert.Equal(t, "admin", c.Writer.Header().Get("X-User-Role"))
	assert.Equal(t, "123456", c.Writer.Header().Get("X-Request-Id"))
	assert.Equal(t, "text/plain", c.Writer.Header().Get("Content-Type"))
}
