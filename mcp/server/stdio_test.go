package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/mcp/types"
)

func TestServerServeStdio(t *testing.T) {
	srv := NewServer("test-stdio-server", "1.0.0")
	srv.RegisterTool(types.Tool{
		Name:        "echo",
		Description: "echo tool",
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		msg, _ := args["msg"].(string)
		return "echo: " + msg, nil
	})

	// 模拟连续的输入请求（initialize -> tools/list -> tools/call -> notifications/initialized）
	inputs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hello"}}}`,
	}
	inputData := strings.Join(inputs, "\n") + "\n"

	inBuf := bytes.NewBufferString(inputData)
	outBuf := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.ServeStdio(ctx, inBuf, outBuf)
	assert.NoError(t, err)

	// 解析输出的行
	outputLines := strings.Split(strings.TrimSpace(outBuf.String()), "\n")
	assert.Equal(t, 3, len(outputLines), "notifications/initialized should not produce response, expect 3 responses")

	// 验证 Response 1: initialize
	var resp1 JSONRPCResponse
	err = json.Unmarshal([]byte(outputLines[0]), &resp1)
	assert.NoError(t, err)
	assert.Equal(t, float64(1), resp1.ID)
	assert.Nil(t, resp1.Error)

	// 验证 Response 2: tools/list
	var resp2 JSONRPCResponse
	err = json.Unmarshal([]byte(outputLines[1]), &resp2)
	assert.NoError(t, err)
	assert.Equal(t, float64(2), resp2.ID)

	// 验证 Response 3: tools/call
	var resp3 JSONRPCResponse
	err = json.Unmarshal([]byte(outputLines[2]), &resp3)
	assert.NoError(t, err)
	assert.Equal(t, float64(3), resp3.ID)
	assert.Nil(t, resp3.Error)

	resMap, ok := resp3.Result.(map[string]interface{})
	assert.True(t, ok)
	contentArr, ok := resMap["content"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(contentArr))
	firstItem := contentArr[0].(map[string]interface{})
	assert.Equal(t, "echo: hello", firstItem["text"])
}
