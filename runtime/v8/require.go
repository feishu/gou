package v8

import (
	"fmt"

	"github.com/yaoapp/gou/runtime/v8/bridge"
	"rogchap.com/v8go"
)

// Require function template
func Require(iso *v8go.Isolate) *v8go.FunctionTemplate {
	return v8go.NewFunctionTemplate(iso, func(info *v8go.FunctionCallbackInfo) *v8go.Value {

		share, err := bridge.ShareData(info.Context())
		if err != nil {
			return bridge.JsException(info.Context(), err)
		}

		jsArgs := info.Args()
		if len(jsArgs) < 1 {
			return bridge.JsException(info.Context(), "missing parameters")
		}

		if !jsArgs[0].IsString() {
			return bridge.JsException(info.Context(), "the first parameter should be a string")
		}

		id := jsArgs[0].String()
		var script *Script
		if share.Root {
			script, err = SelectRoot(id)
		} else {
			script, err = Select(id)
		}
		if err != nil || script == nil {
			return bridge.JsException(info.Context(), fmt.Sprintf("script %s not found", id))
		}

		globalName := "require"
		info.Context().RunScript(Transform(script.Source, globalName), script.File)
		global, _ := info.Context().Global().Get(globalName)
		return global
	})
}
