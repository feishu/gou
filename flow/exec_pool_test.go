package flow

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
)

func TestFlowRunProcessPool(t *testing.T) {
	// 注册一个无需外部依赖的进程
	process.Register("test.mock.echo", func(p *process.Process) interface{} {
		if len(p.Args) > 0 {
			return p.Args[0]
		}
		return nil
	})

	testFlow := &Flow{
		ID:   "test_pool_flow",
		Name: "Test Pool Flow",
		Nodes: []Node{
			{
				Name:    "step1",
				Process: "test.mock.echo",
				Args:    []interface{}{"hello_pool"},
			},
		},
		Output: map[string]interface{}{
			"result": "{{$res.step1}}",
		},
	}

	ctx := context.Background()
	res, err := testFlow.ExecWithContext(ctx, "test_sid", nil)
	assert.NoError(t, err)

	resMap, ok := res.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "hello_pool", resMap["result"])

	// 并发执行验证池化复用与竞态
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r, e := testFlow.ExecWithContext(ctx, "test_sid", nil)
			assert.NoError(t, e)
			rm, ok := r.(map[string]interface{})
			assert.True(t, ok)
			assert.Equal(t, "hello_pool", rm["result"])
		}(i)
	}
	wg.Wait()
}
