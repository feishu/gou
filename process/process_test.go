package process

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/kun/exception"
)

func TestRegister(t *testing.T) {
	prepare(t)
	keys := map[string]bool{}
	for key := range Handlers {
		keys[key] = true
	}
	checkHandlers(t)
}

func TestAlias(t *testing.T) {
	prepare(t)
	Alias("unit.test.prepare", "unit.test.alias")
	_, has := Handlers["unit.test.alias"]
	assert.True(t, has)
}

func TestNew(t *testing.T) {
	prepare(t)

	var p *Process = nil

	// unit.test.prepare
	assert.NotPanics(t, func() {
		p = New("unit.test.prepare", "foo", "bar")
	})
	assert.Equal(t, "unit.test.prepare", p.Name)
	assert.Equal(t, "unit", p.Group)
	assert.Equal(t, "", p.Method)
	assert.Equal(t, "", p.ID)
	assert.Equal(t, []interface{}{"foo", "bar"}, p.Args)

	// models.widget.Test
	assert.NotPanics(t, func() {
		p = New("models.widget.Test", "foo", "bar")
	})
	assert.Equal(t, "models.widget.Test", p.Name)
	assert.Equal(t, "models", p.Group)
	assert.Equal(t, "Test", p.Method)
	assert.Equal(t, "widget", p.ID)
	assert.Equal(t, []interface{}{"foo", "bar"}, p.Args)

	// models.widget.Id.Test
	assert.NotPanics(t, func() {
		p = New("models.widget.Id.Test", "foo", "bar")
	})
	assert.Equal(t, "models.widget.Id.Test", p.Name)
	assert.Equal(t, "models", p.Group)
	assert.Equal(t, "Test", p.Method)
	assert.Equal(t, "widget.id", p.ID)
	assert.Equal(t, []interface{}{"foo", "bar"}, p.Args)

	// flows.widget
	assert.NotPanics(t, func() {
		p = New("flows.widget", "foo", "bar")
	})
	assert.Equal(t, "flows.widget", p.Name)
	assert.Equal(t, "flows", p.Group)
	assert.Equal(t, "", p.Method)
	assert.Equal(t, "widget", p.ID)
	assert.Equal(t, []interface{}{"foo", "bar"}, p.Args)

	// flows.widget.Id
	assert.NotPanics(t, func() {
		p = New("flows.widget.Id", "foo", "bar")
	})
	assert.Equal(t, "flows.widget.Id", p.Name)
	assert.Equal(t, "flows", p.Group)
	assert.Equal(t, "", p.Method)
	assert.Equal(t, "widget.id", p.ID)
	assert.Equal(t, []interface{}{"foo", "bar"}, p.Args)

	// session.Get
	assert.NotPanics(t, func() {
		p = New("session.Get", "foo", "bar")
	})
	assert.Equal(t, "session.Get", p.Name)
	assert.Equal(t, "session", p.Group)
	assert.Equal(t, "Get", p.Method)
	assert.Equal(t, "", p.ID)
	assert.Equal(t, []interface{}{"foo", "bar"}, p.Args)

	// not_found
	assert.PanicsWithValue(t, *exception.New("not_found not found", 404), func() {
		p = New("not_found", "foo", "bar")
	})
}

func TestRun(t *testing.T) {

	prepare(t)
	var p *Process = nil

	// unit.test.prepare
	p = New("unit.test.prepare", "foo", "bar")
	assert.NotPanics(t, func() {
		res := p.Run()
		data, ok := res.(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "unit", data["group"])
		assert.Equal(t, "", data["method"])
		assert.Equal(t, "", data["id"])
		assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	})

	// models.widget.Test
	p = New("models.widget.Test", "foo", "bar")
	assert.NotPanics(t, func() {
		res := p.Run()
		data, ok := res.(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "models", data["group"])
		assert.Equal(t, "Test", data["method"])
		assert.Equal(t, "widget", data["id"])
		assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	})

	// flows.widget
	p = New("flows.widget", "foo", "bar")
	assert.NotPanics(t, func() {
		res := p.Run()
		data, ok := res.(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "flows", data["group"])
		assert.Equal(t, "", data["method"])
		assert.Equal(t, "widget", data["id"])
		assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	})

	// session.Get
	p = New("session.Get", "foo", "bar")
	assert.NotPanics(t, func() {
		res := p.Run()
		data, ok := res.(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "session", data["group"])
		assert.Equal(t, "Get", data["method"])
		assert.Equal(t, "", data["id"])
		assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	})

	// models.widget.Notfound
	p = New("models.widget.Notfound", "foo", "bar")
	assert.PanicsWithValue(t, *exception.New("models.widget.Notfound Handler -> models.notfound not found", 404), func() {
		p.Run()
	})
}

func TestExec(t *testing.T) {

	prepare(t)
	var p *Process = nil

	// unit.test.prepare
	p = New("unit.test.prepare", "foo", "bar")
	res, err := p.Exec()
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "unit", data["group"])
	assert.Equal(t, "", data["method"])
	assert.Equal(t, "", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])

	// models.widget.Test
	p = New("models.widget.Test", "foo", "bar")
	res, err = p.Exec()
	data, ok = res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "models", data["group"])
	assert.Equal(t, "Test", data["method"])
	assert.Equal(t, "widget", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])

	// flows.widget
	p = New("flows.widget", "foo", "bar")
	res, err = p.Exec()
	data, ok = res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "flows", data["group"])
	assert.Equal(t, "", data["method"])
	assert.Equal(t, "widget", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])

	// session.Get
	p = New("session.Get", "foo", "bar")
	res, err = p.Exec()
	data, ok = res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "session", data["group"])
	assert.Equal(t, "Get", data["method"])
	assert.Equal(t, "", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])

	// models.widget.Notfound
	p = New("models.widget.Notfound", "foo", "bar")
	res, err = p.Exec()
	assert.Equal(t, nil, res)
	assert.Equal(t, "Exception|404:models.widget.Notfound Handler -> models.notfound not found", err.Error())
}

func TestExectueAndRelease(t *testing.T) {

	prepare(t)
	var p *Process = nil

	// unit.test.prepare
	p = New("unit.test.prepare", "foo", "bar")
	err := p.Execute()
	if err != nil {
		t.Fatal(err)
	}
	res := p.Value()
	data, ok := res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "unit", data["group"])
	assert.Equal(t, "", data["method"])
	assert.Equal(t, "", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	p.Release()
	assert.Equal(t, nil, p.Value())

	// models.widget.Test
	p = New("models.widget.Test", "foo", "bar")
	err = p.Execute()
	res = p.Value()
	data, ok = res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "models", data["group"])
	assert.Equal(t, "Test", data["method"])
	assert.Equal(t, "widget", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	p.Release()
	assert.Equal(t, nil, p.Value())

	// flows.widget
	p = New("flows.widget", "foo", "bar")
	err = p.Execute()
	res = p.Value()
	data, ok = res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "flows", data["group"])
	assert.Equal(t, "", data["method"])
	assert.Equal(t, "widget", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	p.Release()
	assert.Equal(t, nil, p.Value())

	// session.Get
	p = New("session.Get", "foo", "bar")
	err = p.Execute()
	res = p.Value()
	data, ok = res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "session", data["group"])
	assert.Equal(t, "Get", data["method"])
	assert.Equal(t, "", data["id"])
	assert.Equal(t, []interface{}{"foo", "bar"}, data["args"])
	p.Release()
	assert.Equal(t, nil, p.Value())

	// models.widget.Notfound
	p = New("models.widget.Notfound", "foo", "bar")
	err = p.Execute()
	res = p.Value()
	assert.Equal(t, nil, res)
	assert.Equal(t, "Exception|404:models.widget.Notfound Handler -> models.notfound not found", err.Error())
	p.Release()
	assert.Equal(t, nil, p.Value())
}

func TestWithSID(t *testing.T) {

	prepare(t)
	var p *Process = nil

	// unit.test.prepare
	p = New("unit.test.prepare", "foo", "bar").WithSID("101")
	res, err := p.Exec()
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "101", data["sid"])
}

func TestWithGlobal(t *testing.T) {
	prepare(t)
	var p *Process = nil

	// unit.test.prepare
	p = New("unit.test.prepare", "foo", "bar").WithGlobal(map[string]interface{}{"hello": "world"})
	res, err := p.Exec()
	if err != nil {
		t.Fatal(err)
	}
	data, ok := res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, map[string]interface{}{"hello": "world"}, data["global"])
}

func prepare(t *testing.T) {
	Register("unit.test.prepare", processTest)
	Register("flows", processTest)
	RegisterGroup("models", map[string]Handler{"Test": processTest})
	RegisterGroup("session", map[string]Handler{"Get": processTest})
}

func processTest(process *Process) interface{} {
	return map[string]interface{}{
		"group":  process.Group,
		"method": process.Method,
		"id":     process.ID,
		"args":   process.Args,
		"sid":    process.Sid,
		"global": process.Global,
	}
}

func checkHandlers(t *testing.T) {
	keys := map[string]bool{}
	for key := range Handlers {
		keys[key] = true
	}
	assert.True(t, keys["flows"])
	assert.True(t, keys["models.test"])
	assert.True(t, keys["session.get"])
	assert.True(t, keys["unit.test.prepare"])
}

func TestProcessExecuteSlowPathTimeout(t *testing.T) {
	prepare(t)
	Register("unit.test.slow", func(p *Process) interface{} {
		time.Sleep(100 * time.Millisecond)
		return "done"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	p := New("unit.test.slow").WithContext(ctx)
	err := p.Execute()
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestConcurrentHandlersAccess(t *testing.T) {
	prepare(t)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(3)
		idx := i

		// 并发注册
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("unit.test.concurrent_%d", idx)
			Register(name, func(p *Process) interface{} {
				return idx
			})
		}()

		// 并发查询存在性
		go func() {
			defer wg.Done()
			_ = Exists("unit.test.prepare")
			_ = Exists(fmt.Sprintf("unit.test.concurrent_%d", idx))
		}()

		// 并发调用执行
		go func() {
			defer wg.Done()
			p := New("unit.test.prepare", "test")
			_ = p.Execute()
			_ = p.Value()
		}()
	}

	wg.Wait()
}

func TestContextTestScopeMockIsolation(t *testing.T) {
	prepare(t)

	// 全局注册原实现
	Register("unit.test.user_service", func(p *Process) interface{} {
		return "real_production_user"
	})

	// 1. 无 context 默认调用返回真实实现
	pNormal := New("unit.test.user_service")
	assert.NoError(t, pNormal.Execute())
	assert.Equal(t, "real_production_user", pNormal.Value())

	// 2. 通过 WithTestScope 注入局部 Mock
	mockCtx := WithTestScope(context.Background(), map[string]Handler{
		"unit.test.user_service": func(p *Process) interface{} {
			return "mocked_test_user"
		},
	})

	pMocked := NewWithContext(mockCtx, "unit.test.user_service")
	assert.NoError(t, pMocked.Execute())
	assert.Equal(t, "mocked_test_user", pMocked.Value())

	// 3. 验证全局状态未受污染
	pVerify := New("unit.test.user_service")
	assert.NoError(t, pVerify.Execute())
	assert.Equal(t, "real_production_user", pVerify.Value(), "全局 Handler 必须不受 TestScope 污染")

	// 4. 并发协程隔离验证：协程 A 跑 mock，协程 B 跑真实实现
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p := NewWithContext(mockCtx, "unit.test.user_service")
			assert.NoError(t, p.Execute())
			assert.Equal(t, "mocked_test_user", p.Value())
		}()
		go func() {
			defer wg.Done()
			p := New("unit.test.user_service")
			assert.NoError(t, p.Execute())
			assert.Equal(t, "real_production_user", p.Value())
		}()
	}
	wg.Wait()
}

func TestProcessInterceptorPipeline(t *testing.T) {
	prepare(t)
	kernel := NewKernel()

	var order []string
	kernel.Use(func(p *Process, next Handler) interface{} {
		order = append(order, "before_1")
		res := next(p)
		order = append(order, "after_1")
		return res
	})

	kernel.Use(func(p *Process, next Handler) interface{} {
		order = append(order, "before_2")
		res := next(p)
		order = append(order, "after_2")
		return res
	})

	baseHandler := func(p *Process) interface{} {
		order = append(order, "core")
		return "result"
	}

	chained := kernel.ApplyInterceptors(baseHandler)
	val := chained(&Process{Name: "test"})

	assert.Equal(t, "result", val)
	assert.Equal(t, []string{"before_1", "before_2", "core", "after_2", "after_1"}, order)
}

func TestProcessRouteCache(t *testing.T) {
	prepare(t)
	p1, err := Of("models.user.pet.Find", 1)
	assert.NoError(t, err)
	assert.Equal(t, "models", p1.Group)
	assert.Equal(t, "user.pet", p1.ID)
	assert.Equal(t, "Find", p1.Method)
	assert.Equal(t, "models.find", p1.Handler)

	// 第二次调用命中 routeCache
	p2, err := Of("models.user.pet.Find", 2)
	assert.NoError(t, err)
	assert.Equal(t, p1.Group, p2.Group)
	assert.Equal(t, p1.ID, p2.ID)
	assert.Equal(t, p1.Method, p2.Method)
	assert.Equal(t, p1.Handler, p2.Handler)
}

func TestAcquireProcessPool(t *testing.T) {
	prepare(t)
	Register("test.pool.echo", func(p *Process) interface{} {
		return p.Args[0]
	})

	ctx := context.Background()
	p, err := AcquireProcess(ctx, "test.pool.echo", "hello-pooled")
	assert.NoError(t, err)
	assert.True(t, p.fromPool)

	err = p.Execute()
	assert.NoError(t, err)
	assert.Equal(t, "hello-pooled", p.Value())

	p.Release()
	assert.Nil(t, p.Context)
	assert.Equal(t, 0, len(p.Args))
	assert.Equal(t, "", p.Name)
}

func BenchmarkProcessAcquireExecute(b *testing.B) {
	Register("test.bench.pooled", func(p *Process) interface{} {
		return "ok"
	})

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p, err := AcquireProcess(ctx, "test.bench.pooled", "arg1")
		if err != nil {
			b.Fatal(err)
		}
		_ = p.Execute()
		p.Release()
	}
}

