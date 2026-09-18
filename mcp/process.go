package mcp

import (
	"context"

	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

// MCPHandlers the mcp process handlers
var MCPHandlers = map[string]process.Handler{
	"call":   processCallTool,
	"list":   processListTools,
	"read":   processReadResource,
	"prompt": processGetPrompt,
}

func init() {
	process.RegisterGroup("mcp", MCPHandlers)
}

// processCallTool 调用指定 MCP 客户端的指定工具
// args[0] Client ID (string)
// args[1] Tool Name (string)
// args[2] Arguments (map[string]interface{}) <Optional>
func processCallTool(proc *process.Process) interface{} {
	proc.ValidateArgNums(2)
	clientID := proc.ArgsString(0)
	toolName := proc.ArgsString(1)

	var arguments interface{} = map[string]interface{}{}
	if proc.NumOfArgs() > 2 {
		arguments = proc.Args[2]
	}

	client, err := Select(clientID)
	if err != nil {
		exception.New("MCP Client '%s' not found: %v", 404, clientID, err).Throw()
	}

	ctx := context.Background()
	if proc.Context != nil {
		ctx = proc.Context
	}

	// 确保已连接
	if !client.IsConnected() {
		if err := client.Connect(ctx); err != nil {
			exception.New("Failed to connect MCP Client '%s': %v", 500, clientID, err).Throw()
		}
	}

	// 确保已初始化
	if client.State() != "initialized" {
		if _, err := client.Initialize(ctx); err != nil {
			exception.New("Failed to initialize MCP Client '%s': %v", 500, clientID, err).Throw()
		}
		_ = client.Initialized(ctx)
	}

	resp, err := client.CallTool(ctx, toolName, arguments)
	if err != nil {
		exception.New("Call MCP tool '%s' on '%s' failed: %v", 500, toolName, clientID, err).Throw()
	}

	if resp.IsError {
		errMsg := "MCP tool returned error"
		if len(resp.Content) > 0 {
			errMsg = resp.Content[0].Text
		}
		exception.New(errMsg, 500).Throw()
	}

	// 如果只有一个文本项，直接返回内容字符串或解析为数据
	if len(resp.Content) == 1 && resp.Content[0].Type == "text" {
		return resp.Content[0].Text
	}

	return resp.Content
}

// processListTools 获取指定 MCP 客户端的可用工具列表
// args[0] Client ID (string)
// args[1] Cursor (string) <Optional>
func processListTools(proc *process.Process) interface{} {
	proc.ValidateArgNums(1)
	clientID := proc.ArgsString(0)

	cursor := ""
	if proc.NumOfArgs() > 1 {
		cursor = proc.ArgsString(1)
	}

	client, err := Select(clientID)
	if err != nil {
		exception.New("MCP Client '%s' not found: %v", 404, clientID, err).Throw()
	}

	ctx := context.Background()
	if proc.Context != nil {
		ctx = proc.Context
	}

	if !client.IsConnected() {
		if err := client.Connect(ctx); err != nil {
			exception.New("Failed to connect MCP Client '%s': %v", 500, clientID, err).Throw()
		}
	}

	if client.State() != "initialized" {
		if _, err := client.Initialize(ctx); err != nil {
			exception.New("Failed to initialize MCP Client '%s': %v", 500, clientID, err).Throw()
		}
		_ = client.Initialized(ctx)
	}

	resp, err := client.ListTools(ctx, cursor)
	if err != nil {
		exception.New("List MCP tools on '%s' failed: %v", 500, clientID, err).Throw()
	}

	return resp.Tools
}

// processReadResource 读取指定 MCP 客户端的资源内容
// args[0] Client ID (string)
// args[1] Resource URI (string)
func processReadResource(proc *process.Process) interface{} {
	proc.ValidateArgNums(2)
	clientID := proc.ArgsString(0)
	uri := proc.ArgsString(1)

	client, err := Select(clientID)
	if err != nil {
		exception.New("MCP Client '%s' not found: %v", 404, clientID, err).Throw()
	}

	ctx := context.Background()
	if proc.Context != nil {
		ctx = proc.Context
	}

	if !client.IsConnected() {
		if err := client.Connect(ctx); err != nil {
			exception.New("Failed to connect MCP Client '%s': %v", 500, clientID, err).Throw()
		}
	}

	if client.State() != "initialized" {
		if _, err := client.Initialize(ctx); err != nil {
			exception.New("Failed to initialize MCP Client '%s': %v", 500, clientID, err).Throw()
		}
		_ = client.Initialized(ctx)
	}

	resp, err := client.ReadResource(ctx, uri)
	if err != nil {
		exception.New("Read MCP resource '%s' on '%s' failed: %v", 500, uri, clientID, err).Throw()
	}

	return resp.Contents
}

// processGetPrompt 获取指定 MCP 客户端的 Prompt
// args[0] Client ID (string)
// args[1] Prompt Name (string)
// args[2] Arguments (map[string]interface{}) <Optional>
func processGetPrompt(proc *process.Process) interface{} {
	proc.ValidateArgNums(2)
	clientID := proc.ArgsString(0)
	promptName := proc.ArgsString(1)

	argsMap := map[string]interface{}{}
	if proc.NumOfArgs() > 2 {
		if m, ok := proc.Args[2].(map[string]interface{}); ok {
			argsMap = m
		}
	}

	client, err := Select(clientID)
	if err != nil {
		exception.New("MCP Client '%s' not found: %v", 404, clientID, err).Throw()
	}

	ctx := context.Background()
	if proc.Context != nil {
		ctx = proc.Context
	}

	if !client.IsConnected() {
		if err := client.Connect(ctx); err != nil {
			exception.New("Failed to connect MCP Client '%s': %v", 500, clientID, err).Throw()
		}
	}

	if client.State() != "initialized" {
		if _, err := client.Initialize(ctx); err != nil {
			exception.New("Failed to initialize MCP Client '%s': %v", 500, clientID, err).Throw()
		}
		_ = client.Initialized(ctx)
	}

	resp, err := client.GetPrompt(ctx, promptName, argsMap)
	if err != nil {
		exception.New("Get MCP prompt '%s' on '%s' failed: %v", 500, promptName, clientID, err).Throw()
	}

	return resp
}
