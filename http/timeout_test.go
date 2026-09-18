package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRequestTimeoutProtection(t *testing.T) {
	// 创建一个延迟 500ms 响应的测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// 1. 测试 WithTimeout 生效且不发生挂起
	start := time.Now()
	req := New(server.URL).WithTimeout(50 * time.Millisecond)
	res := req.Get()
	elapsed := time.Since(start)

	assert.Equal(t, 0, res.Code, "超时应返回非 200 错误响应")
	assert.Contains(t, res.Message, "Client.Timeout exceeded", "应明确包含超时错误信息")
	assert.True(t, elapsed < 250*time.Millisecond, "超时应在 100ms 左右返回，而非等待 300ms")

	// 2. 测试 Context 取消立即终止请求
	ctx, cancel := context.WithCancel(context.Background())
	reqWithCancel := New(server.URL).WithContext(ctx)

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	startCancel := time.Now()
	resCancel := reqWithCancel.Get()
	elapsedCancel := time.Since(startCancel)

	assert.Equal(t, 0, resCancel.Code)
	assert.Contains(t, resCancel.Message, "context canceled")
	assert.True(t, elapsedCancel < 200*time.Millisecond, "Context 取消应立即打断网络请求")
}
