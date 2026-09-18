package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

const allowHeaders = "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, Yao-Gateway-Billing, Yao-Request-Uid, Yao-Builder-Uid"
const allowMethods = "POST, GET, OPTIONS, PUT, DELETE, HEAD, PATCH"

// API 数据接口
type API struct {
	ID   string `jsong:"id"`
	Name string
	File string
	Type string
	HTTP HTTP
}

// HTTP http 协议服务
type HTTP struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Group       string `json:"group,omitempty"`
	Guard       string `json:"guard,omitempty"`
	Paths       []Path `json:"paths,omitempty"`
}

// Path HTTP Path
type Path struct {
	Label          string        `json:"label,omitempty"`
	Description    string        `json:"description,omitempty"`
	Path           string        `json:"path"`
	Method         string        `json:"method"`
	Process        string        `json:"process"`
	Guard          string        `json:"guard,omitempty"`
	SSE            *SSEConfig    `json:"sse,omitempty"`
	In             []interface{} `json:"in,omitempty"`
	Out            Out           `json:"out,omitempty"`
	ProcessHandler bool          `json:"processHandler,omitempty"`
	MCP            *MCPConfig    `json:"-"`
	RawMCP         interface{}   `json:"mcp,omitempty"`
}

// MCPConfig MCP 工具声明配置
type MCPConfig struct {
	Enable      bool                `json:"enable,omitempty"`
	Name        string              `json:"name,omitempty"`
	Description string              `json:"description,omitempty"`
	Group       string              `json:"group,omitempty"`
	Params      map[string]MCPParam `json:"params,omitempty"`
}

// MCPParam MCP 工具入参声明
type MCPParam struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// SSEConfig SSE 配置
type SSEConfig struct {
	Type      string       `json:"type,omitempty"`
	Adapter   string       `json:"adapter,omitempty"`
	Heartbeat int          `json:"heartbeat,omitempty"`
	Bus       SSEBusConfig `json:"bus,omitempty"`
}

// SSEBusConfig SSE 消息总线配置
type SSEBusConfig struct {
	Connector string `json:"connector,omitempty"`
	Channel   string `json:"channel,omitempty"`
}

// Out http 输出
type Out struct {
	Status   int               `json:"status"`
	Type     string            `json:"type,omitempty"`
	Body     interface{}       `json:"body,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Redirect *Redirect         `json:"redirect,omitempty"`
}

// Redirect out redirect
type Redirect struct {
	Code     int    `json:"code,omitempty"`
	Location string `json:"location,omitempty"`
}

type ssEventData struct {
	Name    string
	Message interface{}
}

type argsHandler func(c *gin.Context) []interface{}

// NormalizeMCP 规范化 MCP 配置
func (path *Path) NormalizeMCP(defaultGroup, id string) {
	if path.RawMCP == nil {
		return
	}

	switch v := path.RawMCP.(type) {
	case bool:
		if v {
			path.MCP = &MCPConfig{
				Enable: true,
			}
		}
	case map[string]interface{}:
		mcp := &MCPConfig{Enable: true}
		if enable, ok := v["enable"].(bool); ok {
			mcp.Enable = enable
		}
		if name, ok := v["name"].(string); ok {
			mcp.Name = name
		}
		if desc, ok := v["description"].(string); ok {
			mcp.Description = desc
		}
		if group, ok := v["group"].(string); ok {
			mcp.Group = group
		}
		if params, ok := v["params"].(map[string]interface{}); ok {
			mcp.Params = map[string]MCPParam{}
			for pName, pVal := range params {
				if pMap, ok := pVal.(map[string]interface{}); ok {
					param := MCPParam{}
					if pType, ok := pMap["type"].(string); ok {
						param.Type = pType
					}
					if pDesc, ok := pMap["description"].(string); ok {
						param.Description = pDesc
					}
					if pReq, ok := pMap["required"].(bool); ok {
						param.Required = pReq
					}
					if pEnum, ok := pMap["enum"].([]interface{}); ok {
						for _, e := range pEnum {
							if s, ok := e.(string); ok {
								param.Enum = append(param.Enum, s)
							}
						}
					}
					mcp.Params[pName] = param
				}
			}
		}
		path.MCP = mcp
	}

	if path.MCP != nil && path.MCP.Enable {
		if path.MCP.Group == "" {
			path.MCP.Group = defaultGroup
		}
		if path.MCP.Description == "" {
			if path.Description != "" {
				path.MCP.Description = path.Description
			} else if path.Label != "" {
				path.MCP.Description = path.Label
			}
		}
		if path.MCP.Name == "" {
			path.MCP.Name = formatMCPToolName(id, path.Path, path.Method)
		}
	}
}

func formatMCPToolName(id, pathStr, method string) string {
	clean := strings.Trim(pathStr, "/")
	clean = strings.ReplaceAll(clean, "/", "_")
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.ReplaceAll(clean, ":", "")
	clean = strings.ReplaceAll(clean, ".", "_")
	if clean == "" {
		clean = strings.ToLower(method)
	}
	cleanID := strings.ReplaceAll(id, ".", "_")
	cleanID = strings.ReplaceAll(cleanID, "/", "_")
	if cleanID != "" && !strings.HasPrefix(clean, cleanID) {
		return fmt.Sprintf("%s_%s", cleanID, clean)
	}
	return clean
}
