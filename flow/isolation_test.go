package flow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/any"
)

func init() {
	// 注册隔离性与并发测试专用的 Mock 处理器
	process.Register("mock.flow.user", func(p *process.Process) interface{} {
		p.ValidateArgNums(1)
		userID := p.ArgsString(0)
		return map[string]interface{}{
			"id":   userID,
			"role": "role-" + userID,
			"sid":  p.Sid,
		}
	})

	process.Register("mock.flow.menu", func(p *process.Process) interface{} {
		p.ValidateArgNums(1)
		role := p.ArgsString(0)
		// 模拟微小的调度时延，放大并发竞争条件
		time.Sleep(2 * time.Millisecond)
		return []interface{}{
			map[string]interface{}{"name": "Dashboard", "role": role, "sid": p.Sid},
			map[string]interface{}{"name": "Settings", "role": role, "sid": p.Sid},
		}
	})

	process.Register("mock.flow.slow", func(p *process.Process) interface{} {
		select {
		case <-time.After(100 * time.Millisecond):
			return "done"
		}
	})
}

// TestFlowIsolatedContext 验证单次调用上下文数据绑定与穿透
func TestFlowIsolatedContext(t *testing.T) {
	testFlow := &Flow{
		ID:   "test.isolation.single",
		Name: "test.isolation.single",
		Nodes: []Node{
			{
				Name:    "user_info",
				Process: "mock.flow.user",
				Args:    []interface{}{"{{$in.0}}"},
			},
			{
				Name:    "menus",
				Process: "mock.flow.menu",
				Args:    []interface{}{"{{$res.user_info.role}}"},
			},
		},
		Output: map[string]interface{}{
			"user":  "{{$res.user_info}}",
			"menus": "{{$res.menus}}",
			"sid":   "{{$global.sid}}",
		},
	}

	lock.Lock()
	Flows[testFlow.ID] = testFlow
	lock.Unlock()

	p, err := process.Of("flows.test.isolation.single", "user-001")
	assert.NoError(t, err)

	p = p.WithSID("session-token-001").WithGlobal(map[string]interface{}{
		"sid": "session-token-001",
	})

	res, err := p.Exec()
	assert.NoError(t, err)

	dot := any.Of(res).MapStr().Dot()
	assert.Equal(t, "user-001", dot.Get("user.id"))
	assert.Equal(t, "role-user-001", dot.Get("user.role"))
	assert.Equal(t, "session-token-001", dot.Get("user.sid"))
	assert.Equal(t, "session-token-001", dot.Get("sid"))
	assert.Equal(t, "Dashboard", dot.Get("menus[0].name"))
	assert.Equal(t, "session-token-001", dot.Get("menus[0].sid"))
}

// TestFlowHighConcurrencyZeroRace 核心验证：50+ 并发 Goroutine 共享同一个全局注册 Flow，完全零 Session 串扰与零 Data Race
func TestFlowHighConcurrencyZeroRace(t *testing.T) {
	sharedFlow := &Flow{
		ID:   "test.isolation.concurrent",
		Name: "test.isolation.concurrent",
		Nodes: []Node{
			{
				Name:    "user_info",
				Process: "mock.flow.user",
				Args:    []interface{}{"{{$in.0}}"},
			},
			{
				Name:    "menus",
				Process: "mock.flow.menu",
				Args:    []interface{}{"{{$res.user_info.role}}"},
			},
		},
		Output: map[string]interface{}{
			"user":   "{{$res.user_info}}",
			"menus":  "{{$res.menus}}",
			"sid":    "{{$global.sid}}",
			"tenant": "{{$global.tenant}}",
		},
	}

	lock.Lock()
	Flows[sharedFlow.ID] = sharedFlow
	lock.Unlock()

	concurrentCount := 60
	var wg sync.WaitGroup
	wg.Add(concurrentCount)

	type resultData struct {
		userID string
		res    interface{}
		err    error
	}
	results := make([]resultData, concurrentCount)

	for i := 0; i < concurrentCount; i++ {
		go func(idx int) {
			defer wg.Done()

			uid := fmt.Sprintf("usr-%d", idx)
			expectedSid := fmt.Sprintf("sid-secret-%d", idx)
			expectedTenant := fmt.Sprintf("tenant-%d", idx)

			p, err := process.Of("flows.test.isolation.concurrent", uid)
			if err != nil {
				results[idx] = resultData{userID: uid, err: err}
				return
			}

			p = p.WithSID(expectedSid).WithGlobal(map[string]interface{}{
				"sid":    expectedSid,
				"tenant": expectedTenant,
			})

			res, err := p.Exec()
			results[idx] = resultData{
				userID: uid,
				res:    res,
				err:    err,
			}
		}(i)
	}

	wg.Wait()

	// 逐一精确断言每一个并发调用的返回值，证明 Session 与全局变量 100% 隔离
	for i := 0; i < concurrentCount; i++ {
		r := results[i]
		assert.NoError(t, r.err, "idx %d execution error", i)
		assert.NotNil(t, r.res, "idx %d result should not be nil", i)

		dot := any.Of(r.res).MapStr().Dot()
		expectedUID := fmt.Sprintf("usr-%d", i)
		expectedRole := fmt.Sprintf("role-usr-%d", i)
		expectedSid := fmt.Sprintf("sid-secret-%d", i)
		expectedTenant := fmt.Sprintf("tenant-%d", i)

		assert.Equal(t, expectedUID, dot.Get("user.id"), "user.id should match idx %d", i)
		assert.Equal(t, expectedRole, dot.Get("user.role"), "user.role should match idx %d", i)
		assert.Equal(t, expectedSid, dot.Get("user.sid"), "user.sid should match idx %d without contamination", i)
		assert.Equal(t, expectedSid, dot.Get("sid"), "output.sid should match idx %d without contamination", i)
		assert.Equal(t, expectedTenant, dot.Get("tenant"), "output.tenant should match idx %d", i)
		assert.Equal(t, expectedSid, dot.Get("menus[0].sid"), "menus[0].sid should match idx %d without contamination", i)
	}
}

// TestFlowContextCancellation 验证超时熔断与取消传播
func TestFlowContextCancellation(t *testing.T) {
	slowFlow := &Flow{
		ID:   "test.isolation.slow",
		Name: "test.isolation.slow",
		Nodes: []Node{
			{
				Name:    "slow_step",
				Process: "mock.flow.slow",
			},
			{
				Name:    "never_run",
				Process: "mock.flow.user",
				Args:    []interface{}{"ghost"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消上下文

	_, err := slowFlow.ExecWithContext(ctx, "test-sid", nil)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// TestFlowImmutableWithMethods 验证 WithSID 和 WithGlobal 不篡改原始 Flow 指针
func TestFlowImmutableWithMethods(t *testing.T) {
	original := &Flow{
		ID:  "flow.orig",
		Sid: "initial-sid",
		Global: map[string]interface{}{
			"foo": "bar",
		},
	}

	copied := original.WithSID("new-sid").WithGlobal(map[string]interface{}{
		"foo": "baz",
	})

	assert.Equal(t, "initial-sid", original.Sid)
	assert.Equal(t, "bar", original.Global["foo"])

	assert.Equal(t, "new-sid", copied.Sid)
	assert.Equal(t, "baz", copied.Global["foo"])
	assert.False(t, original == copied, "WithSID/WithGlobal must return a fresh copy")
}

// BenchmarkFlowExecution 压测包含复杂对象列表的 Flow 多节点流转开销
func BenchmarkFlowExecution(b *testing.B) {
	process.Register("mock.flow.bench.menu", func(p *process.Process) interface{} {
		role := p.ArgsString(0)
		return []interface{}{
			map[string]interface{}{"name": "Dashboard", "role": role},
			map[string]interface{}{"name": "Settings", "role": role},
		}
	})

	benchFlow := &Flow{
		ID:   "flow.bench.multi",
		Name: "flow.bench.multi",
		Nodes: []Node{
			{
				Name:    "user_info",
				Process: "mock.flow.user",
				Args:    []interface{}{"{{$in.0}}"},
			},
			{
				Name:    "menus",
				Process: "mock.flow.bench.menu",
				Args:    []interface{}{"{{$res.user_info.role}}"},
			},
		},
		Output: map[string]interface{}{
			"user_id":  "{{$res.user_info.id}}",
			"menu_one": "{{$res.menus[0].name}}",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := benchFlow.Exec("user-100")
		if err != nil {
			b.Fatal(err)
		}
	}
}

