package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v8 "github.com/yaoapp/gou/runtime/v8"
)

type closeNotifyingRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

func newCloseNotifyingRecorder() *closeNotifyingRecorder {
	return &closeNotifyingRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closed:           make(chan bool, 1),
	}
}

func (r *closeNotifyingRecorder) CloseNotify() <-chan bool {
	return r.closed
}

func TestStreamHandlerChannelCloseAndClientCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	scriptContent := `
function StreamSuccess() {
	ssEvent("ping", "pong");
	return true;
}

function StreamSlow() {
	for (let i = 0; i < 50; i++) {
		ssEvent("step", i);
	}
	return true;
}
`
	script, err := v8.MakeScript([]byte(scriptContent), "stream.js", 5*time.Second, false)
	require.NoError(t, err)

	v8.Scripts["tests.stream"] = script
	defer func() {
		delete(v8.Scripts, "tests.stream")
	}()

	// Ensure V8 engine is started
	_ = v8.Start(&v8.Option{
		MinSize:       1,
		MaxSize:       2,
		Mode:          "standard",
		HeapSizeLimit: 4294967296,
	})

	// Test 1: Successful stream exits cleanly after closing channels
	t.Run("NormalStreamExitsCleanly", func(t *testing.T) {
		router := gin.New()
		path := Path{
			Path:    "/stream/success",
			Method:  "GET",
			Process: "scripts.tests.stream.StreamSuccess",
			Out: Out{
				Type: "text/event-stream",
			},
		}

		handler := path.streamHandler(func(c *gin.Context) []interface{} {
			return nil
		})
		router.GET("/stream/success", handler)

		w := newCloseNotifyingRecorder()
		req, _ := http.NewRequest("GET", "/stream/success", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
		body := w.Body.String()
		assert.Contains(t, body, "event:ping")
		assert.Contains(t, body, "data:pong")
	})

	// Test 2: Client cancellation exits promptly
	t.Run("ClientCancelExitsPromptly", func(t *testing.T) {
		router := gin.New()
		path := Path{
			Path:    "/stream/slow",
			Method:  "GET",
			Process: "scripts.tests.stream.StreamSlow",
			Out: Out{
				Type: "text/event-stream",
			},
		}

		handler := path.streamHandler(func(c *gin.Context) []interface{} {
			return nil
		})
		router.GET("/stream/slow", handler)

		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", "/stream/slow", nil)
		w := newCloseNotifyingRecorder()

		// Cancel request after 5ms and trigger CloseNotify
		time.AfterFunc(5*time.Millisecond, func() {
			cancel()
			select {
			case w.closed <- true:
			default:
			}
		})

		done := make(chan struct{})
		start := time.Now()
		go func() {
			router.ServeHTTP(w, req)
			close(done)
		}()

		select {
		case <-done:
			require.True(t, time.Since(start) < 2*time.Second, "handler should return promptly on cancel")
		case <-time.After(3 * time.Second):
			t.Fatal("handler hung on client cancellation")
		}
	})
}
