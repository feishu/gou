package process

import (
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/gou/runtime/v8/bridge"
	"rogchap.com/v8go"
)

// ExportFunction function template
func ExportFunction(iso *v8go.Isolate) *v8go.FunctionTemplate {
	return v8go.NewFunctionTemplate(iso, exec)
}

// exec
func exec(info *v8go.FunctionCallbackInfo) *v8go.Value {

	jsArgs := info.Args()
	if len(jsArgs) < 1 {
		return bridge.JsException(info.Context(), "missing parameters")
	}

	if !jsArgs[0].IsString() {
		return bridge.JsException(info.Context(), "the first parameter should be a string")
	}

	share, err := bridge.ShareData(info.Context())
	if err != nil {
		return bridge.JsException(info.Context(), err)
	}

	goArgs := []interface{}{}
	if len(jsArgs) > 1 {
		for _, arg := range jsArgs[1:] {
			v, err := bridge.GoValue(arg, info.Context())
			if err != nil {
				return bridge.JsException(info.Context(), err)
			}
			goArgs = append(goArgs, v)
		}
	}

	proc := process.New(jsArgs[0].String(), goArgs...).
		WithGlobal(share.Global).
		WithSID(share.Sid)

	var goRes interface{}
	if goctx := bridge.GoContext(info.Context()); goctx != nil {
		err = proc.WithContext(goctx).Execute()
		if err == nil {
			goRes = proc.Value()
			proc.Release()
		}
	} else {
		goRes, err = proc.Exec()
	}

	if err != nil {
		return bridge.JsException(info.Context(), err)
	}

	jsRes, err := bridge.JsValue(info.Context(), goRes)
	if err != nil {
		return bridge.JsException(info.Context(), err)
	}

	return jsRes
}
