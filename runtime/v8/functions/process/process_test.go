package process

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/gou/runtime/v8/bridge"
	"rogchap.com/v8go"
)

func TestProcess(t *testing.T) {

	ctx := prepare(t, false, "", nil)
	defer close(ctx)

	jsRes, err := ctx.RunScript(`
		const test = () => {
			const result = Process("unit.test.process", "foo", 99, 0.618);
			return {
				...result,
				__yao_global:__yao_data["DATA"],
				__yao_sid:__yao_data["SID"],
				__YAO_SU_ROOT:__yao_data["ROOT"],
			}
		}
		test()
	`, "")
	if err != nil {
		t.Fatal(err)
	}

	goRes, err := bridge.GoValue(jsRes, ctx)
	if err != nil {
		t.Fatal(err)
	}

	res, ok := goRes.(map[string]interface{})
	if !ok {
		t.Fatal("result error")
	}

	assert.Equal(t, res["Sid"], res["__yao_sid"])
	assert.Equal(t, nil, res["__yao_global"])
	assert.Equal(t, false, res["__YAO_SU_ROOT"])
	assert.Equal(t, "unit.test.process", res["Name"])
	assert.Equal(t, []interface{}{"foo", float64(99), 0.618}, res["Args"])
}

func TestProcessWithData(t *testing.T) {

	ctx := prepare(t, true, "SID-0101", map[string]interface{}{"hello": "world"})
	defer close(ctx)

	jsRes, err := ctx.RunScript(`
		const test = () => {
			const result = Process("unit.test.process", "foo", 99, 0.618);
			return {
				...result,
				__yao_global:__yao_data["DATA"],
				__yao_sid:__yao_data["SID"],
				__YAO_SU_ROOT:__yao_data["ROOT"],
			}
		}
		test()
	`, "")
	if err != nil {
		t.Fatal(err)
	}

	goRes, err := bridge.GoValue(jsRes, ctx)
	if err != nil {
		t.Fatal(err)
	}

	res, ok := goRes.(map[string]interface{})
	if !ok {
		t.Fatal("result error")
	}

	assert.Equal(t, res["Sid"], res["__yao_sid"])
	assert.Equal(t, res["Global"], res["__yao_global"])
	assert.Equal(t, "SID-0101", res["__yao_sid"])
	assert.Equal(t, map[string]interface{}{"hello": "world"}, res["__yao_global"])
	assert.Equal(t, true, res["__YAO_SU_ROOT"])
	assert.Equal(t, "unit.test.process", res["Name"])
	assert.Equal(t, []interface{}{"foo", float64(99), 0.618}, res["Args"])
}

func TestProcessReturnsExceptionWhenErrorGlobalGetterFails(t *testing.T) {
	ctx := prepare(t, false, "", nil)
	defer close(ctx)

	jsRes, err := ctx.RunScript(`
		Object.defineProperty(globalThis, "__yao_data", {
			configurable: true,
			get() { throw "share data failure"; },
		});
		Object.defineProperty(globalThis, "Error", {
			configurable: true,
			get() { throw "error constructor failure"; },
		});

		let caught;
		try {
			Process("unit.test.process");
		} catch (err) {
			caught = err;
		}
		caught && caught.message;
	`, "")
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "share data failure", jsRes.String())
}

func TestProcessWithoutBoundContextKeepsExecBehavior(t *testing.T) {
	ctx := prepare(t, false, "", nil)
	defer close(ctx)

	process.Register("unit.test.no-bound-context", func(process *process.Process) interface{} {
		if process.Context != nil {
			return "unexpected context"
		}
		return "exec"
	})

	jsRes, err := ctx.RunScript(`Process("unit.test.no-bound-context")`, "")
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "exec", jsRes.String())
}

func TestProcessRetainedValuesZeroLeak(t *testing.T) {
	ctx := prepare(t, false, "", nil)
	defer close(ctx)

	process.Register("unit.test.ping", func(p *process.Process) interface{} {
		return "pong"
	})

	initialCount := ctx.RetainedValueCount()

	// 运行 200 次 Process 调用，每次调用产生 this + 2 个参数，但已由 defer info.Release() 释放
	// 并且 ShareData 中的 jsData 也已释放，仅留下返回值与脚本本身结果
	scriptVal, err := ctx.RunScript(`
		for (let i = 0; i < 200; i++) {
			Process("unit.test.ping", i, "hello");
		}
	`, "leak_test.js")
	assert.NoError(t, err)
	scriptVal.Release()

	// 验证入参及 ShareData 句柄已被彻底释放，没有出现 800 个句柄的严重泄漏
	retainedBeforeReset := ctx.RetainedValueCount()
	assert.Equal(t, initialCount+200, retainedBeforeReset, "Process 回调入参及全局数据句柄应完全释放")

	// 验证第二层防线：ResetRetainedValues 一键清空长周期上下文中的残留句柄
	ctx.ResetRetainedValues()
	assert.Equal(t, 0, ctx.RetainedValueCount(), "ResetRetainedValues 应彻底清空上下文所有持久句柄")
}

func close(ctx *v8go.Context) {
	ctx.Isolate().Dispose()
}

func prepare(t *testing.T, root bool, sid string, global map[string]interface{}) *v8go.Context {

	iso := v8go.NewIsolate()

	template := v8go.NewObjectTemplate(iso)
	template.Set("Process", ExportFunction(iso))

	ctx := v8go.NewContext(iso, template)

	var err error
	goData := map[string]interface{}{
		"SID":  sid,
		"ROOT": root,
		"DATA": global,
	}

	jsData, err := bridge.JsValue(ctx, goData)
	if err != nil {
		t.Fatal(err)
	}

	if err = ctx.Global().Set("__yao_data", jsData); err != nil {
		t.Fatal(err)
	}

	process.Register("unit.test.process", func(process *process.Process) interface{} {
		return process
	})
	return ctx
}
