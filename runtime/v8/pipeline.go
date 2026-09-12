package v8

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CallOption 统一脚本执行选项
type CallOption struct {
	Context context.Context        // 上下文，支持外部取消与超时传播
	Sid     string                 // 会话标识
	Global  map[string]interface{} // 全局共享变量
	Root    bool                   // 是否为 studio root 脚本
	Timeout time.Duration          // 执行超时时间，若为 0 则使用脚本配置或全局默认
}

// scriptMockKey 上下文中用于存储脚本 mock 映射的私有 key 类型
type scriptMockKey struct{}

// ScriptMockFn 脚本 mock 执行函数签名
type ScriptMockFn func(args []interface{}) (interface{}, error)

// WithScriptMock 向 context 注入脚本 mock 处理器，用于轻量化单测或无 V8 环境验证（测试接缝）
func WithScriptMock(ctx context.Context, scriptID, method string, fn ScriptMockFn) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	mocks, _ := ctx.Value(scriptMockKey{}).(map[string]ScriptMockFn)
	newMocks := make(map[string]ScriptMockFn, len(mocks)+1)
	for k, v := range mocks {
		newMocks[k] = v
	}
	cleanID := strings.TrimPrefix(scriptID, "scripts.")
	key := fmt.Sprintf("%s.%s", cleanID, method)
	newMocks[key] = fn
	return context.WithValue(ctx, scriptMockKey{}, newMocks)
}

func getScriptMock(ctx context.Context, scriptID, method string) (ScriptMockFn, bool) {
	if ctx == nil {
		return nil, false
	}
	mocks, ok := ctx.Value(scriptMockKey{}).(map[string]ScriptMockFn)
	if !ok {
		return nil, false
	}
	cleanID := strings.TrimPrefix(scriptID, "scripts.")
	key := fmt.Sprintf("%s.%s", cleanID, method)
	fn, has := mocks[key]
	return fn, has
}

// Call 统一执行指定脚本的方法，自动完成调度租借、上下文超时熔断、测试接缝拦截与 Runner 资源安全复位
func Call(scriptID string, method string, args []interface{}, opts ...CallOption) (interface{}, error) {
	var opt CallOption
	if len(opts) > 0 {
		opt = opts[0]
	}

	ctx := opt.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// 1. 优先检查上下文中的测试接缝 (Test Seam)
	if mockFn, ok := getScriptMock(ctx, scriptID, method); ok {
		return mockFn(args)
	}

	// 2. 选择目标脚本
	var script *Script
	var err error
	if opt.Root {
		script, err = SelectRoot(scriptID)
	} else {
		cleanID := strings.TrimPrefix(scriptID, "scripts.")
		script, err = Select(cleanID)
	}
	if err != nil {
		return nil, err
	}

	// 3. 执行管道
	return script.CallWithOption(ctx, method, args, opt)
}

// Call 在脚本实例上执行指定方法，自动闭环管理上下文与 Runner 生命周期
func (script *Script) Call(ctx context.Context, method string, args []interface{}, sid string, global map[string]interface{}) (interface{}, error) {
	return script.CallWithOption(ctx, method, args, CallOption{
		Context: ctx,
		Sid:     sid,
		Global:  global,
	})
}

// CallWithOption 在脚本实例上使用自定义选项执行方法
func (script *Script) CallWithOption(ctx context.Context, method string, args []interface{}, opt CallOption) (interface{}, error) {
	if script == nil {
		return nil, fmt.Errorf("script is nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// 1. 检查测试接缝
	if mockFn, ok := getScriptMock(ctx, script.ID, method); ok {
		return mockFn(args)
	}

	// 2. 确定超时控制
	timeout := opt.Timeout
	if timeout == 0 {
		timeout = script.ContextTimeout()
	}
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// 3. 检查调度器有效性
	if dispatcher == nil {
		return nil, fmt.Errorf("v8 runtime dispatcher is not initialized")
	}

	selectTimeout := time.Duration(runtimeOption.DefaultTimeout) * time.Millisecond
	if selectTimeout <= 0 {
		selectTimeout = 200 * time.Millisecond
	}

	runner, err := dispatcher.Select(selectTimeout)
	if err != nil {
		return nil, fmt.Errorf("scripts.%s.%s select runner error: %w", script.ID, method, err)
	}

	inv := runnerInvocation{
		ctx:    ctx,
		script: script,
		method: method,
		args:   args,
		sid:    opt.Sid,
		global: opt.Global,
	}

	done := make(chan interface{}, 1)
	go func() {
		done <- runner.ExecInvocation(inv)
	}()

	select {
	case res := <-done:
		return runnerCallResult(res)

	case <-ctx.Done():
		runner.retireCurrentExecution()
		return nil, ctx.Err()
	}
}
