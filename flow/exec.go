package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/yaoapp/gou/helper"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/maps"
)

// Exec execute flow
func (flow *Flow) Exec(args ...interface{}) (interface{}, error) {
	return flow.ExecWithContext(context.Background(), flow.Sid, flow.Global, args...)
}

// ExecWithContext execute flow with isolated context, sid and global
func (flow *Flow) ExecWithContext(ctx context.Context, sid string, global map[string]interface{}, args ...interface{}) (interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if sid == "" {
		sid = flow.Sid
	}
	if global == nil {
		global = flow.Global
	}

	res := map[string]interface{}{} // 局部结果集，每个并发调用独立
	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	flowCtx := &Context{
		Context: &innerCtx,
		Cancel:  cancel,
		Res:     res,
		In:      args,
		Sid:     sid,
		Global:  global,
	}

	flowProcess := "flows." + flow.Name
	for i, node := range flow.Nodes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if strings.HasPrefix(node.Process, flowProcess) {
			return nil, fmt.Errorf("cannot call self flow(%s)", node.Process)
		}

		_, err := flow.ExecNode(&node, flowCtx, i-1)
		if err != nil {
			return nil, err
		}
	}

	return flow.FormatResult(flowCtx)
}

// ExtendIn Extend params
func (ctx *Context) ExtendIn(data maps.Map) maps.Map {
	if len(ctx.In) < 1 {
		return data
	}

	item, ok := ctx.In[0].(map[string]interface{})
	if !ok {
		return data
	}
	for key, value := range item {
		data["$"+key] = value
	}
	return data
}

// FormatResult format result
func (flow *Flow) FormatResult(ctx *Context) (interface{}, error) {
	if flow.Output == nil {
		return ctx.Res, nil
	}
	global := ctx.Global
	if global == nil {
		global = flow.Global
	}
	data := maps.Map{"$in": ctx.In, "$res": ctx.Res, "$global": global}
	data = ctx.ExtendIn(data)
	return helper.Bind(flow.Output, data), nil
}

// ExecNode Execute node
func (flow *Flow) ExecNode(node *Node, ctx *Context, prev int) ([]interface{}, error) {
	global := ctx.Global
	if global == nil {
		global = flow.Global
	}
	data := maps.Map{"$in": ctx.In, "$res": ctx.Res, "$global": global}
	data = ctx.ExtendIn(data)
	var outs = []interface{}{}
	var err error

	if node.DSL != nil {
		_, outs, err = flow.RunQuery(node, ctx, data)
		return outs, err
	}

	_, outs, err = flow.RunProcess(node, ctx, data)
	return outs, err
}

// RunQuery execute Query DSL
func (flow *Flow) RunQuery(node *Node, ctx *Context, data maps.Map) (interface{}, []interface{}, error) {

	var res interface{}
	outs := []interface{}{}
	resp := node.DSL.Run(data)

	if node.Outs == nil || len(node.Outs) == 0 {
		res = resp
	} else {
		data["$out"] = resp
		for _, value := range node.Outs {
			outs = append(outs, helper.Bind(value, data))
		}
		res = outs
	}

	if node.Name != "" {
		ctx.Res[node.Name] = res
	}
	return resp, outs, nil
}

// RunProcess exec process
func (flow *Flow) RunProcess(node *Node, ctx *Context, data maps.Map) (interface{}, []interface{}, error) {

	args := []interface{}{}
	outs := []interface{}{}
	var resp interface{}
	var res interface{}
	for _, arg := range node.Args {
		args = append(args, helper.Bind(arg, data))
	}

	if node.Process != "" {
		sid := ctx.Sid
		if sid == "" {
			sid = flow.Sid
		}
		global := ctx.Global
		if global == nil {
			global = flow.Global
		}

		var c context.Context
		if ctx.Context != nil && *ctx.Context != nil {
			c = *ctx.Context
		}

		p, err := process.AcquireProcess(c, node.Process, args...)
		if err != nil {
			return nil, nil, err
		}

		p.WithGlobal(global).WithSID(sid)
		curSid := p.Sid
		resp = p.Run()

		// 仅记录到当前请求局部上下文，绝不踩踏全局指针！
		if ctx.Sid == "" && curSid != "" {
			ctx.Sid = curSid
		}
	}

	if node.Outs == nil || len(node.Outs) == 0 {
		res = resp
	} else {
		data["$out"] = resp
		for _, value := range node.Outs {
			outs = append(outs, helper.Bind(value, data))
		}
		res = outs
	}

	if node.Name != "" {
		ctx.Res[node.Name] = res
	}
	return resp, outs, nil
}
