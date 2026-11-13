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
	mode string // production, development
}

// New create a new Console Object
func New(mode string) *Object {
	// validate mode
	if mode == "" || mode != "development" {
		mode = "production"
	}

	return &Object{
		mode: mode, // production, development
	}
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

	err = ctx.Global().Set(name, instance)
	if err != nil {
		return err
	}
	return nil
}

// ShouldLog checks if console.log should output in the given context
func (obj *Object) ShouldLog(ctx *v8go.Context) bool {
	// 1. In development mode, always output
	if obj.mode == "development" {
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
		"YAO_V8_CONSOLE",
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
	keys := []interface{}{
		DebugContextKey,
		"debug",
		"Debug",
		"DEBUG",
		"x-yao-debug",
		"X-Yao-Debug",
		"x-debug",
		"X-Debug",
	}
	for _, k := range keys {
		v := ctx.Value(k)
		if v == nil {
			continue
		}
		switch val := v.(type) {
		case bool:
			if val {
				return true
			}
		case int:
			if val > 0 {
				return true
			}
		case string:
			lower := strings.ToLower(strings.TrimSpace(val))
			if lower == "true" || lower == "1" || lower == "yes" || lower == "on" {
				return true
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
		for _, key := range []interface{}{TraceContextKey, "trace_id", "traceId", "request_id", "trace"} {
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
		if obj.mode == "development" {
			return obj.dump(info, helper.Dump)
		}
		return obj.dumpToLogger(info, log.DebugLevel)
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
		if obj.mode == "development" {
			return obj.dump(info, helper.DumpWarn)
		}
		return obj.dumpToLogger(info, log.WarnLevel)
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
		if obj.mode == "development" {
			return obj.dump(info, helper.DumpError)
		}
		return obj.dumpToLogger(info, log.ErrorLevel)
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
		if obj.mode == "development" {
			return obj.dump(info, helper.DumpInfo)
		}
		return obj.dumpToLogger(info, log.InfoLevel)
	})
}

/**
 * Dump (for development mode colorful output)
 *
 * @param args
 * @return *v8go.Value
 */
func (obj *Object) dump(info *v8go.FunctionCallbackInfo, method func(...interface{})) *v8go.Value {
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
	method(goArgs...)
	return v8go.Null(info.Context().Isolate())
}

/**
 * DumpToLogger (for production mode structured kun/log output)
 *
 * @param args
 * @return *v8go.Value
 */
func (obj *Object) dumpToLogger(info *v8go.FunctionCallbackInfo, level log.Level) *v8go.Value {
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

	prefix := getTracePrefix(info.Context())
	output := helper.ToString(goArgs...)

	switch level {
	case log.TraceLevel:
		log.Trace("%s %s", prefix, output)
	case log.DebugLevel:
		log.Debug("%s %s", prefix, output)
	case log.InfoLevel:
		log.Info("%s %s", prefix, output)
	case log.WarnLevel:
		log.Warn("%s %s", prefix, output)
	case log.ErrorLevel:
		log.Error("%s %s", prefix, output)
	default:
		log.Debug("%s %s", prefix, output)
	}

	return v8go.Null(info.Context().Isolate())
}
