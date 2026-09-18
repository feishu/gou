package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/mcp/types"
	"github.com/yaoapp/gou/process"
	gouTypes "github.com/yaoapp/gou/types"
)

// mockClient 用于测试 Process 调度的 Mock MCP 客户端
type mockClient struct {
	connected   bool
	initialized bool
}

func (m *mockClient) Connect(ctx context.Context, options ...types.ConnectionOptions) error {
	m.connected = true
	return nil
}

func (m *mockClient) Disconnect(ctx context.Context) error {
	m.connected = false
	return nil
}

func (m *mockClient) IsConnected() bool {
	return m.connected
}

func (m *mockClient) State() types.ConnectionState {
	if m.initialized {
		return types.StateInitialized
	}
	if m.connected {
		return types.StateConnected
	}
	return types.StateDisconnected
}

func (m *mockClient) Initialize(ctx context.Context) (*types.InitializeResponse, error) {
	m.initialized = true
	return &types.InitializeResponse{
		ProtocolVersion: types.ProtocolVersion,
		ServerInfo: types.ServerInfo{
			Name:    "mock-server",
			Version: "1.0",
		},
	}, nil
}

func (m *mockClient) Initialized(ctx context.Context) error {
	return nil
}

func (m *mockClient) ListTools(ctx context.Context, cursor string) (*types.ListToolsResponse, error) {
	return &types.ListToolsResponse{
		Tools: []types.Tool{
			{Name: "sum", Description: "calculate sum"},
		},
	}, nil
}

func (m *mockClient) CallTool(ctx context.Context, name string, arguments interface{}) (*types.CallToolResponse, error) {
	return &types.CallToolResponse{
		Content: []types.ToolContent{
			{Type: types.ToolContentTypeText, Text: "result: 42"},
		},
	}, nil
}

func (m *mockClient) CallToolsBatch(ctx context.Context, tools []types.ToolCall) (*types.CallToolsBatchResponse, error) {
	return nil, nil
}

func (m *mockClient) ListResources(ctx context.Context, cursor string) (*types.ListResourcesResponse, error) {
	return nil, nil
}

func (m *mockClient) ReadResource(ctx context.Context, uri string) (*types.ReadResourceResponse, error) {
	return &types.ReadResourceResponse{
		Contents: []types.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: "mock resource content"},
		},
	}, nil
}

func (m *mockClient) SubscribeResource(ctx context.Context, uri string) error {
	return nil
}

func (m *mockClient) UnsubscribeResource(ctx context.Context, uri string) error {
	return nil
}

func (m *mockClient) ListPrompts(ctx context.Context, cursor string) (*types.ListPromptsResponse, error) {
	return nil, nil
}

func (m *mockClient) GetPrompt(ctx context.Context, name string, arguments map[string]interface{}) (*types.GetPromptResponse, error) {
	return &types.GetPromptResponse{
		Description: "mock prompt " + name,
	}, nil
}

func (m *mockClient) OnEvent(eventType string, handler func(event types.Event))               {}
func (m *mockClient) OnNotification(method string, handler types.NotificationHandler)         {}
func (m *mockClient) OnError(handler types.ErrorHandler)                                      {}
func (m *mockClient) GetMetaInfo() gouTypes.MetaInfo                                         { return gouTypes.MetaInfo{} }

func TestMCPProcessHandlers(t *testing.T) {
	// 注册 mock client 到全局 clients 表
	clientsLock.Lock()
	clients["demo_client"] = &mockClient{}
	clientsLock.Unlock()
	defer UnloadClient("demo_client")

	// 1. 测试 mcp.call
	pCall, err := process.Of("mcp.call", "demo_client", "sum", map[string]interface{}{"a": 10, "b": 32})
	assert.NoError(t, err)
	err = pCall.Execute()
	assert.NoError(t, err)
	assert.Equal(t, "result: 42", pCall.Value())

	// 2. 测试 mcp.list
	pList, err := process.Of("mcp.list", "demo_client")
	assert.NoError(t, err)
	err = pList.Execute()
	assert.NoError(t, err)
	tools, ok := pList.Value().([]types.Tool)
	assert.True(t, ok)
	assert.Equal(t, 1, len(tools))
	assert.Equal(t, "sum", tools[0].Name)

	// 3. 测试 mcp.read
	pRead, err := process.Of("mcp.read", "demo_client", "yao://mock/data")
	assert.NoError(t, err)
	err = pRead.Execute()
	assert.NoError(t, err)
	contents, ok := pRead.Value().([]types.ResourceContent)
	assert.True(t, ok)
	assert.Equal(t, 1, len(contents))
	assert.Equal(t, "mock resource content", contents[0].Text)

	// 4. 测试 mcp.prompt
	pPrompt, err := process.Of("mcp.prompt", "demo_client", "greeting", map[string]interface{}{})
	assert.NoError(t, err)
	err = pPrompt.Execute()
	assert.NoError(t, err)
	promptResp, ok := pPrompt.Value().(*types.GetPromptResponse)
	assert.True(t, ok)
	assert.Equal(t, "mock prompt greeting", promptResp.Description)
}
