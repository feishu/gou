package bridge

import (
	"context"
	"sync"

	"rogchap.com/v8go"
)

var goContextBindings sync.Map

// BindGoContext 在一次脚本调用期间把 Go context 绑定到当前 v8go Context。
func BindGoContext(ctx *v8go.Context, goctx context.Context) func() {
	if ctx == nil || goctx == nil {
		return func() {}
	}
	goContextBindings.Store(ctx, goctx)
	return func() {
		goContextBindings.Delete(ctx)
	}
}

// GoContext 返回当前 v8go Context 已绑定的 Go context。
func GoContext(ctx *v8go.Context) context.Context {
	if ctx == nil {
		return nil
	}
	goctx, ok := goContextBindings.Load(ctx)
	if !ok {
		return nil
	}
	ctxValue, ok := goctx.(context.Context)
	if !ok {
		return nil
	}
	return ctxValue
}
