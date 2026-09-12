package flow

import (
	"fmt"
	"sync"

	"github.com/yaoapp/kun/log"

	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/query"
)

// Flows 已加载工作流列表
var Flows = map[string]*Flow{}
var lock sync.RWMutex

// Load the flow
func Load(file string, id string) (*Flow, error) {

	data, err := application.App.Read(file)
	if err != nil {
		return nil, err
	}

	flow := Flow{ID: id, File: file}
	err = application.Parse(file, data, &flow)
	if err != nil {
		return nil, err
	}

	flow.prepare()

	lock.Lock()
	defer lock.Unlock()
	Flows[id] = &flow
	return Flows[id], nil
}

// Prepare 预加载 Query DSL
func (flow *Flow) prepare() {

	for i, node := range flow.Nodes {
		if node.Query == nil {
			continue
		}

		if node.Engine == "" {
			log.Error("Node %s: 未指定数据查询分析引擎", node.Name)
			continue
		}

		if engine, has := query.Engines[node.Engine]; has {
			var err error
			flow.Nodes[i].DSL, err = engine.Load(node.Query)
			if err != nil {
				log.With(log.F{"query": node.Query}).Error("Node %s: %s 数据分析查询解析错误", node.Name, node.Engine)
			}
			continue
		}
		log.Error("Node %s: %s 数据分析引擎尚未注册", node.Name, node.Engine)
	}
}

// Reload 重新载入API
func (flow *Flow) Reload() (*Flow, error) {
	new, err := Load(flow.File, flow.Name)
	if err != nil {
		return nil, err
	}

	lock.Lock()
	defer lock.Unlock()
	flow = new
	Flows[flow.Name] = new
	return flow, nil
}

// WithSID 设定会话ID（返回安全的局部隔离副本，杜绝多协程指针踩踏）
func (flow *Flow) WithSID(sid string) *Flow {
	copy := *flow
	copy.Sid = sid
	return &copy
}

// WithGlobal 设定全局变量（返回安全的局部隔离副本，杜绝多协程指针踩踏）
func (flow *Flow) WithGlobal(global map[string]interface{}) *Flow {
	copy := *flow
	copy.Global = global
	return &copy
}

// Select 读取已加载Flow
func Select(name string) (*Flow, error) {
	lock.RLock()
	defer lock.RUnlock()

	flow, has := Flows[name]
	if !has {
		return nil, fmt.Errorf("flows.%s not loaded", name)
	}
	return flow, nil
}

// Count 获取已加载 Flow 数量
func Count() int {
	lock.RLock()
	defer lock.RUnlock()
	return len(Flows)
}

// Range 安全遍历已加载 Flow
func Range(fn func(id string, f *Flow) bool) {
	lock.RLock()
	defer lock.RUnlock()

	for id, f := range Flows {
		if !fn(id, f) {
			break
		}
	}
}

