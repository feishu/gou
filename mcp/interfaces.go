package mcp

import (
	"context"
	"io"
	"net/http"

	"github.com/yaoapp/gou/mcp/server"
	"github.com/yaoapp/gou/mcp/types"
	gouTypes "github.com/yaoapp/gou/types"
)

// Client 定义客户端深模块接口，暴露真实且完备的消费能力
type Client interface {
	// 连接生命周期
	Connect(ctx context.Context, options ...types.ConnectionOptions) error
	Disconnect(ctx context.Context) error
	IsConnected() bool
	State() types.ConnectionState

	// 协议初始化与自省
	Initialize(ctx context.Context) (*types.InitializeResponse, error)
	Initialized(ctx context.Context) error

	// 工具调用与列表
	ListTools(ctx context.Context, cursor string) (*types.ListToolsResponse, error)
	CallTool(ctx context.Context, name string, arguments interface{}) (*types.CallToolResponse, error)
	CallToolsBatch(ctx context.Context, tools []types.ToolCall) (*types.CallToolsBatchResponse, error)

	// 资源读取与订阅
	ListResources(ctx context.Context, cursor string) (*types.ListResourcesResponse, error)
	ReadResource(ctx context.Context, uri string) (*types.ReadResourceResponse, error)
	SubscribeResource(ctx context.Context, uri string) error
	UnsubscribeResource(ctx context.Context, uri string) error

	// Prompt 交互
	ListPrompts(ctx context.Context, cursor string) (*types.ListPromptsResponse, error)
	GetPrompt(ctx context.Context, name string, arguments map[string]interface{}) (*types.GetPromptResponse, error)

	// 事件与通知监听
	OnEvent(eventType string, handler func(event types.Event))
	OnNotification(method string, handler types.NotificationHandler)
	OnError(handler types.ErrorHandler)

	// 元信息
	GetMetaInfo() gouTypes.MetaInfo
}

// Server 定义服务端深模块接口，由 *server.Server 完整实现
type Server interface {
	RegisterTool(tool types.Tool, handler server.ToolHandler)
	GetTools() []types.Tool
	DispatchJSONRPC(ctx context.Context, body []byte) (*server.JSONRPCResponse, error)
	HandleSSE() http.Handler
	HandleMessages() http.Handler
	HandleDirect() http.Handler
	HTTPHandler() http.Handler
	ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error
	RunStdio(ctx ...context.Context) error
}
