package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/gou/mcp/types"
	"github.com/yaoapp/kun/log"
)

// ToolHandler 工具执行函数签名
type ToolHandler func(ctx context.Context, args map[string]interface{}) (interface{}, error)

// PromptHandler 处理 Prompt 获取请求
type PromptHandler func(ctx context.Context, args map[string]interface{}) (*types.GetPromptResponse, error)

// ResourceHandler 处理 Resource 读取请求
type ResourceHandler func(ctx context.Context, uri string) ([]types.ResourceContent, error)

// ToolItem 注册的工具项
type ToolItem struct {
	Tool    types.Tool
	Handler ToolHandler
}

// PromptItem 注册的提示词项
type PromptItem struct {
	Prompt  types.Prompt
	Handler PromptHandler
}

// ResourceItem 注册的资源项
type ResourceItem struct {
	Resource types.Resource
	Handler  ResourceHandler
}


// JSONRPCRequest 标准 JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse 标准 JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError 标准 JSON-RPC 2.0 错误
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// 标准 JSON-RPC 错误码
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

type contextKey string

const (
	// RequestContextKey 用于在 context.Context 中存取 HTTP 请求元信息
	RequestContextKey contextKey = "mcp:request_context"
)

// RequestInfo 包含 MCP 传输层的 HTTP 请求上下文
type RequestInfo struct {
	Header http.Header
	Query  map[string][]string
	Remote string
	Path   string
}

// GetRequestInfo 从 context.Context 中获取 HTTP 请求元信息
func GetRequestInfo(ctx context.Context) (*RequestInfo, bool) {
	if ctx == nil {
		return nil, false
	}
	info, ok := ctx.Value(RequestContextKey).(*RequestInfo)
	return info, ok
}

// WithRequestInfo 将 HTTP 请求元信息注入 context.Context
func WithRequestInfo(ctx context.Context, info *RequestInfo) context.Context {
	return context.WithValue(ctx, RequestContextKey, info)
}

func buildRequestInfoFromHTTP(r *http.Request) *RequestInfo {
	if r == nil {
		return nil
	}
	remote := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		remote = forwarded
	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		remote = realIP
	}
	return &RequestInfo{
		Header: r.Header.Clone(),
		Query:  r.URL.Query(),
		Remote: remote,
		Path:   r.URL.Path,
	}
}

func buildRequestInfoFromGin(c *gin.Context) *RequestInfo {
	if c == nil || c.Request == nil {
		return nil
	}
	info := buildRequestInfoFromHTTP(c.Request)
	if info != nil {
		info.Remote = c.ClientIP()
	}
	return info
}


// Server MCP 服务端实例
type Server struct {
	Name            string
	Version         string
	ProtocolVersion string
	tools           map[string]*ToolItem
	prompts         map[string]*PromptItem
	resources       map[string]*ResourceItem
	broker          SessionBroker // 安全的会话总线
	mu              sync.RWMutex
}

// NewServer 创建新的 MCP 服务端
func NewServer(name, version string) *Server {
	if name == "" {
		name = "yao-mcp-server"
	}
	if version == "" {
		version = "1.0.0"
	}
	return &Server{
		Name:            name,
		Version:         version,
		ProtocolVersion: types.ProtocolVersion,
		tools:           make(map[string]*ToolItem),
		prompts:         make(map[string]*PromptItem),
		resources:       make(map[string]*ResourceItem),
		broker:          NewDefaultSessionBroker(),
	}
}

// Broker 获取当前 Server 关联的 SessionBroker
func (s *Server) Broker() SessionBroker {
	return s.broker
}

// SetBroker 注入自定义的 SessionBroker（便于测试或扩展）
func (s *Server) SetBroker(broker SessionBroker) {
	s.broker = broker
}

// RegisterPrompt 注册一个提示词模板
func (s *Server) RegisterPrompt(prompt types.Prompt, handler PromptHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = &PromptItem{
		Prompt:  prompt,
		Handler: handler,
	}
}

// GetPrompts 获取当前注册的所有提示词模板
func (s *Server) GetPrompts() []types.Prompt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]types.Prompt, 0, len(s.prompts))
	for _, item := range s.prompts {
		list = append(list, item.Prompt)
	}
	return list
}

// RegisterResource 注册一个只读资源
func (s *Server) RegisterResource(resource types.Resource, handler ResourceHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource.URI] = &ResourceItem{
		Resource: resource,
		Handler:  handler,
	}
}

// GetResources 获取当前注册的所有资源列表
func (s *Server) GetResources() []types.Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]types.Resource, 0, len(s.resources))
	for _, item := range s.resources {
		list = append(list, item.Resource)
	}
	return list
}


// RegisterTool 注册一个工具
func (s *Server) RegisterTool(tool types.Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(tool.InputSchema) == 0 {
		tool.InputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
	}

	s.tools[tool.Name] = &ToolItem{
		Tool:    tool,
		Handler: handler,
	}
}

// GetTools 获取当前注册的所有工具列表
func (s *Server) GetTools() []types.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]types.Tool, 0, len(s.tools))
	for _, item := range s.tools {
		tools = append(tools, item.Tool)
	}
	return tools
}

// DispatchJSONRPC 处理原始 JSON-RPC 请求
func (s *Server) DispatchJSONRPC(ctx context.Context, body []byte) (*JSONRPCResponse, error) {
	var req JSONRPCRequest
	if err := jsoniter.Unmarshal(body, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    CodeParseError,
				Message: fmt.Sprintf("Invalid JSON: %v", err),
			},
		}, nil
	}

	if req.JSONRPC != "2.0" {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeInvalidRequest,
				Message: "jsonrpc must be '2.0'",
			},
		}, nil
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil, nil
	case "notifications/cancelled":
		return nil, nil
	case "ping":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}, nil
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "prompts/list":
		return s.handleListPrompts(req)
	case "prompts/get":
		return s.handleGetPrompt(ctx, req)
	case "resources/list":
		return s.handleListResources(req)
	case "resources/read":
		return s.handleReadResource(ctx, req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeMethodNotFound,
				Message: fmt.Sprintf("Method '%s' not supported", req.Method),
			},
		}, nil
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) (*JSONRPCResponse, error) {
	s.mu.RLock()
	hasPrompts := len(s.prompts) > 0
	hasResources := len(s.resources) > 0
	s.mu.RUnlock()

	caps := types.ServerCapabilities{
		Tools: &types.ToolsCapability{ListChanged: false},
	}
	if hasPrompts {
		caps.Prompts = &types.PromptsCapability{ListChanged: false}
	}
	if hasResources {
		caps.Resources = &types.ResourcesCapability{Subscribe: false, ListChanged: false}
	}

	res := types.InitializeResponse{
		ProtocolVersion: s.ProtocolVersion,
		Capabilities:    caps,
		ServerInfo: types.ServerInfo{
			Name:    s.Name,
			Version: s.Version,
		},
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  res,
	}, nil
}

func (s *Server) handleListTools(req JSONRPCRequest) (*JSONRPCResponse, error) {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: types.ListToolsResponse{
			Tools: s.GetTools(),
		},
	}, nil
}

func (s *Server) handleListPrompts(req JSONRPCRequest) (*JSONRPCResponse, error) {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: types.ListPromptsResponse{
			Prompts: s.GetPrompts(),
		},
	}, nil
}

func (s *Server) handleGetPrompt(ctx context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments,omitempty"`
	}
	if len(req.Params) > 0 {
		if err := jsoniter.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeInvalidParams,
					Message: fmt.Sprintf("Invalid params: %v", err),
				},
			}, nil
		}
	}

	s.mu.RLock()
	item, ok := s.prompts[params.Name]
	s.mu.RUnlock()

	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Prompt '%s' not found", params.Name),
			},
		}, nil
	}

	res, err := item.Handler(ctx, params.Arguments)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeInternalError,
				Message: err.Error(),
			},
		}, nil
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  res,
	}, nil
}

func (s *Server) handleListResources(req JSONRPCRequest) (*JSONRPCResponse, error) {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: types.ListResourcesResponse{
			Resources: s.GetResources(),
		},
	}, nil
}

func (s *Server) handleReadResource(ctx context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if len(req.Params) > 0 {
		if err := jsoniter.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeInvalidParams,
					Message: fmt.Sprintf("Invalid params: %v", err),
				},
			}, nil
		}
	}

	s.mu.RLock()
	item, ok := s.resources[params.URI]
	s.mu.RUnlock()

	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Resource '%s' not found", params.URI),
			},
		}, nil
	}

	contents, err := item.Handler(ctx, params.URI)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeInternalError,
				Message: err.Error(),
			},
		}, nil
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: types.ReadResourceResponse{
			Contents: contents,
		},
	}, nil
}


func (s *Server) handleCallTool(ctx context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments,omitempty"`
	}

	if len(req.Params) > 0 {
		if err := jsoniter.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeInvalidParams,
					Message: fmt.Sprintf("Invalid params: %v", err),
				},
			}, nil
		}
	}

	s.mu.RLock()
	item, ok := s.tools[params.Name]
	s.mu.RUnlock()

	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Tool '%s' not found", params.Name),
			},
		}, nil
	}

	if params.Arguments == nil {
		params.Arguments = make(map[string]interface{})
	}

	val, err := item.Handler(ctx, params.Arguments)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: types.CallToolResponse{
				Content: []types.ToolContent{
					{Type: types.ToolContentTypeText, Text: fmt.Sprintf("Error: %v", err)},
				},
				IsError: true,
			},
		}, nil
	}

	var textOut string
	switch v := val.(type) {
	case string:
		textOut = v
	case []byte:
		textOut = string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			textOut = fmt.Sprintf("%v", v)
		} else {
			textOut = string(b)
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: types.CallToolResponse{
			Content: []types.ToolContent{
				{Type: types.ToolContentTypeText, Text: textOut},
			},
			IsError: false,
		},
	}, nil
}

// HandleSSE 返回处理 SSE 长连接的标准 http.Handler
func (s *Server) HandleSSE() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		session, err := s.broker.CreateSession()
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}
		session.SetMeta("req_info", buildRequestInfoFromHTTP(r))
		sessionID := session.ID()
		defer s.broker.RemoveSession(sessionID)

		messagesURL := fmt.Sprintf("%s/messages?session_id=%s", r.URL.Path, sessionID)

		// 1. 下发 endpoint 事件告知客户端往何处 POST 报文
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", messagesURL)
		flusher.Flush()

		notify := r.Context().Done()
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		// 2. 循环监听：若有消息推回客户端，有心跳则发送 ping，客户端断开则释放
		for {
			select {
			case <-notify:
				return
			case msg, ok := <-session.Receive():
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(msg))
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	})
}

// HandleMessages 返回处理发往 SSE 会话消息的标准 http.Handler
func (s *Server) HandleMessages() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read request body"})
			return
		}

		sessionID := r.URL.Query().Get("session_id")
		reqInfo := buildRequestInfoFromHTTP(r)
		if sessionID != "" {
			if session, ok := s.broker.GetSession(sessionID); ok {
				if reqInfo != nil && reqInfo.Header.Get("Authorization") == "" {
					if metaVal, ok := session.GetMeta("req_info"); ok {
						if metaReqInfo, ok := metaVal.(*RequestInfo); ok && metaReqInfo.Header.Get("Authorization") != "" {
							reqInfo.Header.Set("Authorization", metaReqInfo.Header.Get("Authorization"))
						}
					}
				}
			}
		}
		reqCtx := WithRequestInfo(r.Context(), reqInfo)

		resp, err := s.DispatchJSONRPC(reqCtx, body)
		if err != nil {
			log.Error("[MCP] Dispatch error: %v", err)
		}

		// 规范要求：如果绑定了有效 SSE 会话，结果应通过 SSE 流推送，HTTP 返回 202 Accepted
		if sessionID != "" {
			if session, ok := s.broker.GetSession(sessionID); ok {
				if resp != nil {
					respBytes, _ := json.Marshal(resp)
					sendErr := session.Send(respBytes)
					if sendErr == nil {
						w.WriteHeader(http.StatusAccepted)
						return
					}
					log.Warn("[MCP] Session %s send failed (%v), falling back to direct HTTP response", sessionID, sendErr)
				} else {
					w.WriteHeader(http.StatusAccepted)
					return
				}
			}
		}

		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// HandleDirect 返回处理单端点 POST 请求的标准 http.Handler
func (s *Server) HandleDirect() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read request body"})
			return
		}

		reqCtx := WithRequestInfo(r.Context(), buildRequestInfoFromHTTP(r))
		resp, err := s.DispatchJSONRPC(reqCtx, body)
		if err != nil {
			log.Error("[MCP] Dispatch error: %v", err)
		}

		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// HTTPHandler 返回统一自适应的 MCP 标准 http.Handler
func (s *Server) HTTPHandler() http.Handler {
	sseHandler := s.HandleSSE()
	messagesHandler := s.HandleMessages()
	directHandler := s.HandleDirect()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && (path == "/sse" || strings.HasSuffix(path, "/sse")):
			sseHandler.ServeHTTP(w, r)
		case r.Method == http.MethodPost && (path == "/messages" || strings.HasSuffix(path, "/messages")):
			messagesHandler.ServeHTTP(w, r)
		case r.Method == http.MethodPost:
			directHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// SSEHandler 处理 SSE 长连接请求（向下兼容 Gin）
func (s *Server) SSEHandler(c *gin.Context) {
	s.HandleSSE().ServeHTTP(c.Writer, c.Request)
}

// MessagesHandler 接收客户端发往 SSE 会话的 JSON-RPC 报文（向下兼容 Gin）
func (s *Server) MessagesHandler(c *gin.Context) {
	s.HandleMessages().ServeHTTP(c.Writer, c.Request)
}

// DirectHandler 单端点 POST 请求处理器（向下兼容 Gin）
func (s *Server) DirectHandler(c *gin.Context) {
	s.HandleDirect().ServeHTTP(c.Writer, c.Request)
}

