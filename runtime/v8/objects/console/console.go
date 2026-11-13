package console

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yaoapp/gou/helper"
	"github.com/yaoapp/gou/runtime/v8/bridge"
	"github.com/yaoapp/kun/log"
	"rogchap.com/v8go"
)

// ContextKey represents a context key type
type ContextKey string

const (
	// DebugContextKey context key for enabling console logging
	DebugContextKey ContextKey = "debug"
	// TraceContextKey context key for trace id
	TraceContextKey ContextKey = "trace"
)

// Object Javascript API
type Object struct {
	mode  string // production, development
	debug bool   // whether global debug is enabled
}

// New create a new Console Object
func New(mode string) *Object {
	if mode == "" || mode != "development" {
		mode = "production"
	}

	return &Object{
		mode: mode,
	}
}

// WithDebug sets whether debug mode is enabled globally
func (obj *Object) WithDebug(debug bool) *Object {
	obj.debug = debug
	return obj
}

// ExportObject Export as a Console Object
// console.log("name", {"foo":"bar"} )
func (obj *Object) ExportObject(iso *v8go.Isolate) *v8go.ObjectTemplate {
	tmpl := v8go.NewObjectTemplate(iso)
	tmpl.Set("log", obj.log(iso))
	tmpl.Set("info", obj.info(iso))
	tmpl.Set("warn", obj.warn(iso))
	tmpl.Set("error", obj.error(iso))
	return tmpl
}

// Set new obj instance
func (obj *Object) Set(name string, ctx *v8go.Context) error {
	tmpl := v8go.NewObjectTemplate(ctx.Isolate())
	tmpl.Set("log", obj.log(ctx.Isolate()))
	tmpl.Set("info", obj.info(ctx.Isolate()))
	tmpl.Set("warn", obj.warn(ctx.Isolate()))
	tmpl.Set("error", obj.error(ctx.Isolate()))

	instance, err := tmpl.NewInstance(ctx)
	if err != nil {
		return err
	}

	return ctx.Global().Set(name, instance)
}

// ShouldLog checks if console.log or console.info should output in the given context
func (obj *Object) ShouldLog(ctx *v8go.Context) bool {
	// 1. In development mode or global debug enabled, always output
	if obj.mode == "development" || obj.debug {
		return true
	}

	// 2. Check independent environment variable (YAO_V8_CONSOLE_LOG or YAO_CONSOLE_LOG)
	if isEnvConsoleEnabled() {
		return true
	}

	// 3. Check request-level Go context dyeing
	if ctx != nil {
		if goctx := bridge.GoContext(ctx); goctx != nil {
			if isContextDebugEnabled(goctx) {
				return true
			}
		}
	}

	return false
}

func isEnvConsoleEnabled() bool {
	keys := []string{
		"YAO_V8_CONSOLE_LOG",
		"YAO_CONSOLE_LOG",
	}
	for _, key := range keys {
		val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if val == "true" || val == "1" || val == "yes" || val == "on" || val == "development" {
			return true
		}
	}
	return false
}

func isContextDebugEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	for _, k := range []interface{}{DebugContextKey, "debug", "x-yao-debug"} {
		if v := ctx.Value(k); v != nil {
			switch val := v.(type) {
			case bool:
				if val {
					return true
				}
			case string:
				if strings.EqualFold(val, "true") || val == "1" {
					return true
				}
			}
		}
	}
	return false
}

func getTracePrefix(ctx *v8go.Context) string {
	if ctx == nil {
		return "[V8 Console]"
	}
	if goctx := bridge.GoContext(ctx); goctx != nil {
		for _, key := range []interface{}{TraceContextKey, "trace_id", "traceId", "request_id"} {
			if v := goctx.Value(key); v != nil {
				if str, ok := v.(string); ok && str != "" {
					return fmt.Sprintf("[V8 Console] [%s]", str)
				}
			}
		}
	}
	return "[V8 Console]"
}

/**
 * Log
 *
 * @param args
 * @return *v8go.Value
 */
func (obj *Object) log(iso *v8go.Isolate) *v8go.FunctionTemplate {
	return v8go.NewFunctionTemplate(iso, func(info *v8go.FunctionCallbackInfo) *v8go.Value {
		if !obj.ShouldLog(info.Context()) {
			info.Release()
			return v8go.Null(iso)
		}
		return obj.handle(info, helper.Dump, log.Debug)
	})
}

/**
 * Info
 *
 * @param args
 * @return *v8go.Value
 */
func (obj *Object) info(iso *v8go.Isolate) *v8go.FunctionTemplate {
	return v8go.NewFunctionTemplate(iso, func(info *v8go.FunctionCallbackInfo) *v8go.Value {
		if !obj.ShouldLog(info.Context()) {
			info.Release()
			return v8go.Null(iso)
		}
		return obj.handle(info, helper.DumpInfo, log.Info)
	})
}

/**
 * Warn
 *
 * @param args
 * @return *v8go.Value
 */
func (obj *Object) warn(iso *v8go.Isolate) *v8go.FunctionTemplate {
	return v8go.NewFunctionTemplate(iso, func(info *v8go.FunctionCallbackInfo) *v8go.Value {
		return obj.handle(info, helper.DumpWarn, log.Warn)
	})
}

/**
 * Error
 *
 * @param args
 * @return *v8go.Value
 */
func (obj *Object) error(iso *v8go.Isolate) *v8go.FunctionTemplate {
	return v8go.NewFunctionTemplate(iso, func(info *v8go.FunctionCallbackInfo) *v8go.Value {
		return obj.handle(info, helper.DumpError, log.Error)
	})
}

/**
 * handle processes callback args and dispatches to development or production logger
 */
func (obj *Object) handle(info *v8go.FunctionCallbackInfo, devMethod func(...interface{}), prodLogger func(string, ...interface{})) *v8go.Value {
	defer info.Release()
	args := info.Args()
	if len(args) < 1 {
		msg := "console: Missing parameters"
		log.Error("%s", msg)
		return bridge.JsException(info.Context(), msg)
	}

	goArgs, err := bridge.GoValues(args, info.Context())
	if err != nil {
		msg := fmt.Sprintf("console: %s", err.Error())
		log.Error("%s", msg)
		return bridge.JsException(info.Context(), msg)
	}

	if obj.mode == "development" {
		devMethod(goArgs...)
	} else {
		prefix := getTracePrefix(info.Context())
		output := helper.ToString(goArgs...)
		prodLogger("%s %s", prefix, output)
	}

	return v8go.Null(info.Context().Isolate())
}
