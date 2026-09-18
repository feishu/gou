package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/mcp/types"
)

// safeRecorder 线程安全的 ResponseWriter 实现（支持 Flush，并互斥保护 Body 读写）
type safeRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
	mu     sync.RWMutex
}

func newSafeRecorder() *safeRecorder {
	return &safeRecorder{
		header: make(http.Header),
		code:   http.StatusOK,
	}
}

func (r *safeRecorder) Header() http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header
}

func (r *safeRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(b)
}

func (r *safeRecorder) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.code = statusCode
}

func (r *safeRecorder) Flush() {}

func (r *safeRecorder) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.body.String()
}

func TestServerStandardHTTPHandler(t *testing.T) {
	srv := NewServer("test-http-server", "1.0.0")
	srv.RegisterTool(types.Tool{
		Name:        "greet",
		Description: "greet user",
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		name, _ := args["name"].(string)
		return "Hello, " + name, nil
	})

	handler := srv.HTTPHandler()

	// 1. Direct POST 请求测试
	callReq := `{"jsonrpc":"2.0","id":100,"method":"tools/call","params":{"name":"greet","arguments":{"name":"Alice"}}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/mcps/default", bytes.NewBufferString(callReq))
	r.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, float64(100), resp.ID)
	assert.Nil(t, resp.Error)

	resMap, ok := resp.Result.(map[string]interface{})
	assert.True(t, ok)
	contentArr := resMap["content"].([]interface{})
	assert.Equal(t, "Hello, Alice", contentArr[0].(map[string]interface{})["text"])

	// 2. SSE 长连接与消息回发端到端测试（使用线程安全 safeRecorder）
	sseRec := newSafeRecorder()
	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()

	sseReq := httptest.NewRequest(http.MethodGet, "/v1/mcps/default/sse", nil).WithContext(sseCtx)

	doneChan := make(chan struct{})
	go func() {
		defer close(doneChan)
		handler.ServeHTTP(sseRec, sseReq)
	}()

	// 轮询等待下发 endpoint 事件
	var sessionID string
	assert.Eventually(t, func() bool {
		bodyStr := sseRec.String()
		if strings.Contains(bodyStr, "event: endpoint\n") && strings.Contains(bodyStr, "session_id=") {
			lines := strings.Split(bodyStr, "\n")
			for _, l := range lines {
				if strings.HasPrefix(l, "data: ") && strings.Contains(l, "session_id=") {
					parts := strings.Split(l, "session_id=")
					if len(parts) == 2 {
						sessionID = strings.TrimSpace(parts[1])
						return true
					}
				}
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "must capture session_id from SSE endpoint event")

	assert.NotEmpty(t, sessionID)

	// 3. 向 /messages 发送请求（通过 SSE 接收响应）
	msgBody := `{"jsonrpc":"2.0","id":200,"method":"tools/list"}`
	msgRec := httptest.NewRecorder()
	msgReq := httptest.NewRequest(http.MethodPost, "/v1/mcps/default/messages?session_id="+sessionID, bytes.NewBufferString(msgBody))
	handler.ServeHTTP(msgRec, msgReq)

	assert.Equal(t, http.StatusAccepted, msgRec.Code, "messages with active session must return 202 Accepted")

	// 验证 SSE 通道收到 tools/list 的推送响应
	assert.Eventually(t, func() bool {
		bodyStr := sseRec.String()
		return strings.Contains(bodyStr, "event: message\n") && strings.Contains(bodyStr, `"id":200`) && strings.Contains(bodyStr, `"greet"`)
	}, 2*time.Second, 10*time.Millisecond)

	sseCancel()
	<-doneChan
}
