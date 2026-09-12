package moapi

import (
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/connector/base"
	"github.com/yaoapp/gou/helper"
	"github.com/yaoapp/gou/types"
)

// Connector connector
type Connector struct {
	base.NonSQL
	id      string
	file    string
	Name    string  `json:"name"`
	Options Options `json:"options"`
	types.MetaInfo
}

// Options the redis connector option
type Options struct {
	Proxy string `json:"proxy,omitempty"`
	Model string `json:"model,omitempty"`
	Key   string `json:"key"`
}

// Register the connections from dsl
func (o *Connector) Register(file string, id string, dsl []byte) error {
	o.id = id
	o.file = file
	err := application.Parse(file, dsl, o)
	if err != nil {
		return err
	}

	o.Options.Proxy = helper.EnvString(o.Options.Proxy)
	o.Options.Model = helper.EnvString(o.Options.Model)
	o.Options.Key = helper.EnvString(o.Options.Key)
	return nil
}

// Is the connections from dsl
func (o *Connector) Is(typ int) bool {
	return 6 == typ || 8 == typ
}

// ID get connector id
func (o *Connector) ID() string {
	return o.id
}

// Close connections
func (o *Connector) Close() error {
	return nil
}

// Setting get the connection setting
func (o *Connector) Setting() map[string]interface{} {

	host := "https://api.moapi.ai"
	if o.Options.Proxy != "" {
		host = o.Options.Proxy
	}

	return map[string]interface{}{
		"host":  host,
		"key":   o.Options.Key,
		"model": o.Options.Model,
	}
}

// GetMetaInfo returns the meta information
func (o *Connector) GetMetaInfo() types.MetaInfo {
	return o.MetaInfo
}
