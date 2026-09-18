package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/mcp/types"
)

func TestMCPServerProtocols(t *testing.T) {
	server := NewServer("test-server", "1.0.0")

	// 注册一个测试工具
	server.RegisterTool(types.Tool{
		Name:        "calculate_fee",
		Description: "计算挂号费",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"department_id":{"type":"string","description":"科室ID"},"is_expert":{"type":"boolean","description":"是否专家号"}},"required":["department_id"]}`),
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		dept, _ := args["department_id"].(string)
		isExpert, _ := args["is_expert"].(bool)
		fee := 20
		if isExpert {
			fee = 100
		}
		return map[string]interface{}{
			"department_id": dept,
			"fee":           fee,
			"status":        "success",
		}, nil
	})

	ctx := context.Background()

	// 1. 测试 initialize
	initReq := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`)
	initResp, err := server.DispatchJSONRPC(ctx, initReq)
	assert.NoError(t, err)
	assert.NotNil(t, initResp)
	assert.Equal(t, "2.0", initResp.JSONRPC)
	assert.Equal(t, 1, int(initResp.ID.(float64)))

	initResultBytes, _ := json.Marshal(initResp.Result)
	var initResult types.InitializeResponse
	err = json.Unmarshal(initResultBytes, &initResult)
	assert.NoError(t, err)
	assert.Equal(t, "test-server", initResult.ServerInfo.Name)
	assert.NotNil(t, initResult.Capabilities.Tools)

	// 2. 测试 notifications/initialized
	notifReq := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	notifResp, err := server.DispatchJSONRPC(ctx, notifReq)
	assert.NoError(t, err)
	assert.Nil(t, notifResp)

	// 3. 测试 ping
	pingReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	pingResp, err := server.DispatchJSONRPC(ctx, pingReq)
	assert.NoError(t, err)
	assert.NotNil(t, pingResp)
	assert.Nil(t, pingResp.Error)

	// 4. 测试 tools/list
	listReq := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	listResp, err := server.DispatchJSONRPC(ctx, listReq)
	assert.NoError(t, err)
	assert.NotNil(t, listResp)
	assert.Nil(t, listResp.Error)

	listBytes, _ := json.Marshal(listResp.Result)
	var listResult types.ListToolsResponse
	json.Unmarshal(listBytes, &listResult)
	assert.Equal(t, 1, len(listResult.Tools))
	assert.Equal(t, "calculate_fee", listResult.Tools[0].Name)
	assert.Equal(t, "计算挂号费", listResult.Tools[0].Description)

	// 5. 测试 tools/call 正常调用
	callReq := []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"calculate_fee","arguments":{"department_id":"DEPT_CARDIO","is_expert":true}}}`)
	callResp, err := server.DispatchJSONRPC(ctx, callReq)
	assert.NoError(t, err)
	assert.NotNil(t, callResp)
	assert.Nil(t, callResp.Error)

	callBytes, _ := json.Marshal(callResp.Result)
	var callResult types.CallToolResponse
	json.Unmarshal(callBytes, &callResult)
	assert.False(t, callResult.IsError)
	assert.Equal(t, 1, len(callResult.Content))
	assert.Contains(t, callResult.Content[0].Text, `"fee":100`)
	assert.Contains(t, callResult.Content[0].Text, `"department_id":"DEPT_CARDIO"`)

	// 6. 测试 tools/call 工具不存在
	notFoundReq := []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}`)
	notFoundResp, err := server.DispatchJSONRPC(ctx, notFoundReq)
	assert.NoError(t, err)
	assert.NotNil(t, notFoundResp)
	assert.NotNil(t, notFoundResp.Error)
	assert.Equal(t, CodeInvalidParams, notFoundResp.Error.Code)

	// 7. 测试注册抛错工具
	server.RegisterTool(types.Tool{
		Name: "error_tool",
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("database connection failed")
	})
	errReq := []byte(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"error_tool"}}`)
	errResp, err := server.DispatchJSONRPC(ctx, errReq)
	assert.NoError(t, err)
	assert.NotNil(t, errResp)
	assert.Nil(t, errResp.Error)
	var errResult types.CallToolResponse
	errBytes, _ := json.Marshal(errResp.Result)
	json.Unmarshal(errBytes, &errResult)
	assert.True(t, errResult.IsError)
	assert.Contains(t, errResult.Content[0].Text, "database connection failed")

	// 8. 测试未支持方法
	unsupportedReq := []byte(`{"jsonrpc":"2.0","id":7,"method":"unknown_method"}`)
	unsupportedResp, err := server.DispatchJSONRPC(ctx, unsupportedReq)
	assert.NoError(t, err)
	assert.NotNil(t, unsupportedResp)
	assert.NotNil(t, unsupportedResp.Error)
	assert.Equal(t, CodeMethodNotFound, unsupportedResp.Error.Code)
}

func TestMCPServerPromptsAndResources(t *testing.T) {
	server := NewServer("test-prompts-resources", "1.0.0")
	ctx := context.Background()

	// 注册 Prompt
	server.RegisterPrompt(types.Prompt{
		Name:        "doctor_prompt",
		Description: "医生问诊提示词",
		Arguments: []types.PromptArgument{
			{Name: "patient_name", Description: "患者姓名", Required: true},
		},
	}, func(ctx context.Context, args map[string]interface{}) (*types.GetPromptResponse, error) {
		name, _ := args["patient_name"].(string)
		return &types.GetPromptResponse{
			Description: "给 " + name + " 的问诊指引",
			Messages: []types.PromptMessage{
				{
					Role: "system",
					Content: types.PromptContent{
						Type: "text",
						Text: "你是一位专业心内科主治医生，正在为 " + name + " 进行初诊。",
					},
				},
			},
		}, nil
	})

	// 注册 Resource
	server.RegisterResource(types.Resource{
		URI:         "yao://hospital/departments",
		Name:        "departments_list",
		Description: "医院科室目录",
		MimeType:    "application/json",
	}, func(ctx context.Context, uri string) ([]types.ResourceContent, error) {
		return []types.ResourceContent{
			{
				URI:      uri,
				MimeType: "application/json",
				Text:     `[{"id":"cardio","name":"心内科"},{"id":"neuro","name":"神经内科"}]`,
			},
		}, nil
	})

	// 1. 测试 initialize 汇报能力
	initResp, err := server.DispatchJSONRPC(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	assert.NoError(t, err)
	assert.NotNil(t, initResp)
	var initResult types.InitializeResponse
	b, _ := json.Marshal(initResp.Result)
	json.Unmarshal(b, &initResult)
	assert.NotNil(t, initResult.Capabilities.Prompts)
	assert.NotNil(t, initResult.Capabilities.Resources)

	// 2. 测试 prompts/list
	pListResp, err := server.DispatchJSONRPC(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"prompts/list"}`))
	assert.NoError(t, err)
	assert.Nil(t, pListResp.Error)
	var pListResult types.ListPromptsResponse
	b, _ = json.Marshal(pListResp.Result)
	json.Unmarshal(b, &pListResult)
	assert.Equal(t, 1, len(pListResult.Prompts))
	assert.Equal(t, "doctor_prompt", pListResult.Prompts[0].Name)

	// 3. 测试 prompts/get
	pGetResp, err := server.DispatchJSONRPC(ctx, []byte(`{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"doctor_prompt","arguments":{"patient_name":"张三"}}}`))
	assert.NoError(t, err)
	assert.Nil(t, pGetResp.Error)
	var pGetResult types.GetPromptResponse
	b, _ = json.Marshal(pGetResp.Result)
	json.Unmarshal(b, &pGetResult)
	assert.Equal(t, "给 张三 的问诊指引", pGetResult.Description)
	assert.Equal(t, 1, len(pGetResult.Messages))

	// 4. 测试 resources/list
	rListResp, err := server.DispatchJSONRPC(ctx, []byte(`{"jsonrpc":"2.0","id":4,"method":"resources/list"}`))
	assert.NoError(t, err)
	assert.Nil(t, rListResp.Error)
	var rListResult types.ListResourcesResponse
	b, _ = json.Marshal(rListResp.Result)
	json.Unmarshal(b, &rListResult)
	assert.Equal(t, 1, len(rListResult.Resources))
	assert.Equal(t, "yao://hospital/departments", rListResult.Resources[0].URI)

	// 5. 测试 resources/read
	rReadResp, err := server.DispatchJSONRPC(ctx, []byte(`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"yao://hospital/departments"}}`))
	assert.NoError(t, err)
	assert.Nil(t, rReadResp.Error)
	var rReadResult types.ReadResourceResponse
	b, _ = json.Marshal(rReadResp.Result)
	json.Unmarshal(b, &rReadResult)
	assert.Equal(t, 1, len(rReadResult.Contents))
	assert.Contains(t, rReadResult.Contents[0].Text, "心内科")

	// 6. 测试 notifications/cancelled
	cancelResp, err := server.DispatchJSONRPC(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":123}}`))
	assert.NoError(t, err)
	assert.Nil(t, cancelResp)
}

