package http

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGracefulShutdownDrainsInFlightRequests(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	requestReceived := make(chan struct{})
	requestFinished := make(chan struct{})

	router.GET("/api/slow", func(c *gin.Context) {
		close(requestReceived)
		// Simulate in-flight work taking 80ms
		time.Sleep(80 * time.Millisecond)
		c.JSON(200, gin.H{"status": "completed"})
		close(requestFinished)
	})

	option := Option{
		Host:         "127.0.0.1",
		Port:         0, // random available port
		Timeout:      5 * time.Second,
		DrainTimeout: 2 * time.Second,
	}

	server := New(router, option)
	go func() {
		_ = server.Start()
	}()

	<-server.Event()
	assert.True(t, server.Ready())

	port, err := server.Port()
	assert.NoError(t, err)

	client := &http.Client{Timeout: 3 * time.Second}
	reqURL := fmt.Sprintf("http://127.0.0.1:%d/api/slow", port)

	respChan := make(chan int, 1)
	errChan := make(chan error, 1)

	// Send slow request in background
	go func() {
		resp, err := client.Get(reqURL)
		if err != nil {
			errChan <- err
			return
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
		respChan <- resp.StatusCode
	}()

	// Wait until the server receives the request
	<-requestReceived

	// Trigger graceful stop while request is still running
	stopStart := time.Now()
	err = server.Stop()
	assert.NoError(t, err)

	// Wait for the stop event
	<-server.Event()
	stopDuration := time.Since(stopStart)

	// Verify server waited for the in-flight request (> 60ms) and response succeeded (200 OK)
	select {
	case code := <-respChan:
		assert.Equal(t, 200, code, "In-flight request should complete with 200 OK")
	case err := <-errChan:
		t.Fatalf("In-flight request failed during graceful shutdown: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for response")
	}

	assert.True(t, stopDuration >= 60*time.Millisecond, "Server stop should have waited for in-flight request to finish")
	assert.False(t, server.Ready())
}
