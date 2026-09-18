package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPathMCPDeclaration(t *testing.T) {
	source := []byte(`{
		"name": "Test MCP API",
		"version": "1.0.0",
		"group": "hospital",
		"paths": [
			{
				"path": "/patients",
				"method": "GET",
				"process": "scripts.patient.List",
				"label": "患者列表查询",
				"description": "查询全院就诊患者简要列表",
				"mcp": true
			},
			{
				"path": "/appointment/cancel",
				"method": "POST",
				"process": "scripts.appointment.Cancel",
				"description": "取消挂号并释放排班",
				"mcp": {
					"name": "cancel_appointment",
					"description": "根据预约单号取消挂号并退号",
					"group": "outpatient",
					"params": {
						"order_id": {
							"type": "string",
							"description": "门诊就诊订单ID",
							"required": true
						},
						"reason": {
							"type": "string",
							"description": "取消原因说明"
						}
					}
				}
			},
			{
				"path": "/internal/ping",
				"method": "GET",
				"process": "scripts.test.Ping"
			}
		]
	}`)

	api, err := LoadSource("apis/test_mcp.http.json", source, "test.mcp")
	assert.NoError(t, err)
	assert.NotNil(t, api)
	assert.Equal(t, 3, len(api.HTTP.Paths))

	// Path 0: mcp: true 极简模式
	p0 := api.HTTP.Paths[0]
	assert.NotNil(t, p0.MCP)
	assert.True(t, p0.MCP.Enable)
	assert.Equal(t, "hospital", p0.MCP.Group)
	assert.Equal(t, "查询全院就诊患者简要列表", p0.MCP.Description)
	assert.Equal(t, "test_mcp_patients", p0.MCP.Name)

	// Path 1: mcp: { ... } 精细模式
	p1 := api.HTTP.Paths[1]
	assert.NotNil(t, p1.MCP)
	assert.True(t, p1.MCP.Enable)
	assert.Equal(t, "outpatient", p1.MCP.Group)
	assert.Equal(t, "cancel_appointment", p1.MCP.Name)
	assert.Equal(t, "根据预约单号取消挂号并退号", p1.MCP.Description)
	assert.Equal(t, 2, len(p1.MCP.Params))
	assert.Equal(t, "string", p1.MCP.Params["order_id"].Type)
	assert.True(t, p1.MCP.Params["order_id"].Required)
	assert.Equal(t, "门诊就诊订单ID", p1.MCP.Params["order_id"].Description)

	// Path 2: 未声明 MCP
	p2 := api.HTTP.Paths[2]
	assert.Nil(t, p2.MCP)
}
