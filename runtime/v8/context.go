package v8

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/yaoapp/gou/runtime/v8/bridge"
	"github.com/yaoapp/gou/runtime/v8/objects/console"
	"github.com/yaoapp/kun/log"
	"rogchap.com/v8go"
)

var reFuncHead = regexp.MustCompile(`\s*function\s+(\w+)\s*\(([^)]*)\)\s*\{`)

// Call call the script function
func (context *Context) Call(method string, args ...interface{}) (interface{}, error) {

	// Performance Mode
	if runner, inv := context.runnerInvocation(method, args); runner != nil {
		return runnerCallResult(runner.ExecInvocation(inv))
	}

	// Set the global data
	global := context.Global()
	err := bridge.SetShareData(context.Context, global, &bridge.Share{
		Sid:    context.Sid,
		Root:   context.Root,
		Global: context.Data,
	})
	if err != nil {
		return nil, err
	}

	// console.log("foo", "bar", 1, 2, 3, 4)
	err = console.New(runtimeOption.ConsoleMode).Set("console", context.Context)
	if err != nil {
		return nil, err
	}

	// Run the method
	jsArgs, err := bridge.JsValues(context.Context, args)
	if err != nil {
		return nil, err
	}
	defer bridge.FreeJsValues(jsArgs)

	jsRes, err := global.MethodCall(method, bridge.Valuers(jsArgs)...)
	if err != nil {
		if e, ok := err.(*v8go.JSError); ok {
			PrintException(method, args, e, context.SourceRoots)
		}
		log.Error("%s.%s %s", context.ID, method, err.Error())
		return nil, err
	}

	goRes, err := bridge.GoValue(jsRes, context.Context)
	if err != nil {
		return nil, err
	}

	return goRes, nil
}

// CallAnonymous call the script function with anonymous function
func (context *Context) CallAnonymous(source string, args ...interface{}) (interface{}, error) {

	// Remove the function name from the source, if it exists regex
	source = reFuncHead.ReplaceAllString(source, "")
	name := fmt.Sprintf("__anonymous_%s", uuid.New().String())

	iso := context.v8Isolate()
	script, err := iso.CompileUnboundScript(source, name, v8go.CompileOptions{})
	if err != nil {
		return nil, err
	}

	fn, err := script.Run(context.Context)
	if err != nil {
		return nil, err
	}
	defer fn.Release()

	global := context.Global()
	global.Set(name, fn)
	defer global.Delete(name)

	res, err := context.Call(name, args...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CallAnonymousWith call the script function with anonymous function
func (context *Context) CallAnonymousWith(ctx context.Context, source string, args ...interface{}) (interface{}, error) {

	source = reFuncHead.ReplaceAllString(source, "($2) => {")
	name := fmt.Sprintf("__anonymous_%s", uuid.New().String())

	iso := context.v8Isolate()
	script, err := iso.CompileUnboundScript(source, name, v8go.CompileOptions{})
	if err != nil {
		return nil, err
	}

	fn, err := script.Run(context.Context)
	if err != nil {
		return nil, err
	}
	defer fn.Release()

	global := context.Global()
	global.Set(name, fn)
	defer global.Delete(name)

	res, err := context.CallWith(ctx, name, args...)
	if err != nil {
		color.White("Source:\n")
		lines := strings.Split(source, "\n")
		total := fmt.Sprintf("%d", len(lines))
		for i, line := range lines {
			num := fmt.Sprintf("%d", i+1)
			num = strings.Repeat(" ", len(total)-len(num)) + num
			fmt.Printf("%s: %s\n", num, line)
		}
		return nil, err
	}
	return res, nil
}

// CallWith call the script function
func (context *Context) CallWith(ctx context.Context, method string, args ...interface{}) (interface{}, error) {

	// Performance Mode
	if runner, inv := context.runnerInvocation(method, args); runner != nil {
		inv.ctx = ctx
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

	unbindGoContext := bridge.BindGoContext(context.Context, ctx)
	defer unbindGoContext()

	// Set the global data
	global := context.Global()
	err := bridge.SetShareData(context.Context, global, &bridge.Share{
		Sid:    context.Sid,
		Root:   context.Root,
		Global: context.Data,
	})
	if err != nil {
		return nil, err
	}

	// console.log("foo", "bar", 1, 2, 3, 4)
	err = console.New(runtimeOption.ConsoleMode).Set("console", context.Context)
	if err != nil {
		return nil, err
	}

	// Run the method
	jsArgs, err := bridge.JsValues(context.Context, args)
	if err != nil {
		return nil, err
	}
	defer bridge.FreeJsValues(jsArgs)

	doneChan := make(chan struct{})
	resChan := make(chan interface{}, 1)
	errChan := make(chan error, 1)

	go func() {
		defer close(doneChan)

		jsRes, err := global.MethodCall(method, bridge.Valuers(jsArgs)...)
		if err != nil {
			if e, ok := err.(*v8go.JSError); ok {
				PrintException(method, args, e, context.SourceRoots)
			}
			errChan <- err
			return
		}

		goRes, err := bridge.GoValue(jsRes, context.Context)
		if err != nil {
			errChan <- err
			return
		}

		resChan <- goRes
	}()

	select {
	case <-ctx.Done():
		if iso := context.v8Isolate(); iso != nil {
			iso.TerminateExecution()
		}
		<-doneChan
		return nil, ctx.Err()

	case err := <-errChan:
		<-doneChan
		log.Error("%s.%s %v", context.ID, method, err)
		return nil, err

	case goRes := <-resChan:
		<-doneChan
		return goRes, nil
	}
}

// WithFunction add a function to the context
func (context *Context) WithFunction(name string, cb v8go.FunctionCallback) {
	tmpl := v8go.NewFunctionTemplate(context.v8Isolate(), cb)
	context.Global().Set(name, tmpl.GetFunction(context.Context))
}

// WithGlobal add a global variable to the context
func (context *Context) WithGlobal(name string, value interface{}) error {
	switch value.(type) {
	case v8go.Valuer:
		context.Global().Set(name, value)
	default:
		jsValue, err := bridge.JsValue(context.Context, value)
		if err != nil {
			return err
		}
		context.Global().Set(name, jsValue)
	}
	return nil
}

// Close Context
func (context *Context) Close() error {
	if context == nil {
		return nil
	}

	context.mu.Lock()
	if context.closed {
		context.mu.Unlock()
		return nil
	}
	context.closed = true
	runner := context.Runner
	runnerUsed := context.runnerUsed
	v8ctx := context.Context
	iso := context.Isolate
	context.Context = nil
	context.UnboundScript = nil
	context.Data = nil
	context.Runner = nil
	context.Isolate = nil
	context.mu.Unlock()

	if runner != nil {
		if !runnerUsed {
			runner.Reset()
		}
		return nil
	}

	if v8ctx != nil {
		v8ctx.Close()
	}
	if iso != nil {
		iso.Dispose()
	}
	return nil
}

func (context *Context) runnerInvocation(method string, args []interface{}) (*Runner, runnerInvocation) {
	context.mu.Lock()
	defer context.mu.Unlock()

	runner := context.Runner
	if runner != nil {
		context.runnerUsed = true
	}
	inv := runnerInvocation{
		script: context.script,
		method: method,
		args:   args,
		sid:    context.Sid,
		global: context.Data,
	}
	return runner, inv
}

func (context *Context) v8Isolate() *v8go.Isolate {
	if context == nil {
		return nil
	}
	if context.Isolate != nil && context.Isolate.Isolate != nil {
		return context.Isolate.Isolate
	}
	if context.Context != nil {
		return context.Context.Isolate()
	}
	return nil
}

func runnerCallResult(res interface{}) (interface{}, error) {
	if err, ok := res.(error); ok {
		return nil, err
	}
	return res, nil
}
