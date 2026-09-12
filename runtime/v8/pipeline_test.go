package v8

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithScriptMock(t *testing.T) {
	ctx := context.Background()

	// 1. 注入针对 scripts.user.Login 的 Mock
	ctx = WithScriptMock(ctx, "user", "Login", func(args []interface{}) (interface{}, error) {
		assert.Equal(t, 2, len(args))
		username := args[0].(string)
		password := args[1].(string)
		if username == "admin" && password == "secret" {
			return map[string]interface{}{"token": "mock-token-123"}, nil
		}
		return nil, errors.New("invalid credentials")
	})

	// 2. 调用已 Mock 的方法（成功路径）
	res, err := Call("user", "Login", []interface{}{"admin", "secret"}, CallOption{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"token": "mock-token-123"}, res)

	// 3. 调用已 Mock 的方法（失败路径）
	res, err = Call("scripts.user", "Login", []interface{}{"admin", "wrong"}, CallOption{Context: ctx})
	assert.Error(t, err)
	assert.Nil(t, res)
	assert.Equal(t, "invalid credentials", err.Error())

	// 4. 验证全局与未注入 Context 的隔离性（原始 context.Background() 没有此 Mock）
	_, err = Call("user", "Login", []interface{}{"admin", "secret"}, CallOption{Context: context.Background()})
	assert.Error(t, err, "Unmocked context should attempt real lookup and fail if script not loaded")
}

func TestScriptMockContextIsolation(t *testing.T) {
	var wg sync.WaitGroup
	workers := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			expectedVal := fmt.Sprintf("val-%d", workerID)
			ctx := WithScriptMock(context.Background(), "test.worker", "Get", func(args []interface{}) (interface{}, error) {
				return expectedVal, nil
			})

			for j := 0; j < 50; j++ {
				res, err := Call("test.worker", "Get", nil, CallOption{Context: ctx})
				assert.NoError(t, err)
				assert.Equal(t, expectedVal, res)
			}
		}(i)
	}

	wg.Wait()
}

func TestPipelineUninitializedDispatcher(t *testing.T) {
	// 如果脚本未载入且未 mock，应正常报错
	ctx := context.Background()
	_, err := Call("non.existent.script", "Method", nil, CallOption{Context: ctx})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not exists")
}

func TestPipelineTimeoutOption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 如果没有 Mock 且脚本存在，或者被 cancelled context 拦截
	opt := CallOption{
		Context: ctx,
		Timeout: 50 * time.Millisecond,
	}
	assert.NotNil(t, opt.Context)
	assert.Equal(t, 50*time.Millisecond, opt.Timeout)
}
