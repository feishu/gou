package config

import (
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/connector/base"
	"github.com/yaoapp/gou/helper"
	gouTypes "github.com/yaoapp/gou/types"
)

// Connector 通用配置型连接器 (type: config)
type Connector struct {
	base.NonSQL
	id      string
	file    string
	Name    string                 `json:"name"`
	Label   string                 `json:"label,omitempty"`
	Version string                 `json:"version,omitempty"`
	Type    string                 `json:"type"`
	Options map[string]interface{} `json:"options,omitempty"`
	gouTypes.MetaInfo
}

// Register 从 DSL 注册连接器并自动解析 options 中的 $ENV.xxx 环境变量
func (c *Connector) Register(file string, id string, dsl []byte) error {
	c.id = id
	c.file = file
	err := application.Parse(file, dsl, c)
	if err != nil {
		return err
	}
	if c.Options == nil {
		c.Options = make(map[string]interface{})
	}
	c.Options = parseEnvInMap(c.Options)
	return nil
}

// parseEnvInMap 递归替换 map 中的 $ENV.xxx 声明为实际环境变量值 (与 redis/database connector 规范对齐)
func parseEnvInMap(m map[string]interface{}) map[string]interface{} {
	res := make(map[string]interface{})
	for k, v := range m {
		switch val := v.(type) {
		case string:
			res[k] = helper.EnvString(val)
		case map[string]interface{}:
			res[k] = parseEnvInMap(val)
		default:
			res[k] = val
		}
	}
	return res
}

// Is 判断连接器类型 (11 为 CONFIG)
func (c *Connector) Is(typ int) bool {
	return 11 == typ
}

// ID 获取连接器 ID
func (c *Connector) ID() string {
	return c.id
}

// Close 关闭连接器
func (c *Connector) Close() error {
	return nil
}

// Setting 获取连接器配置选项字典
func (c *Connector) Setting() map[string]interface{} {
	return c.Options
}

// GetMetaInfo 获取连接器元数据
func (c *Connector) GetMetaInfo() gouTypes.MetaInfo {
	return c.MetaInfo
}
