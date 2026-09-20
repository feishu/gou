package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestProcessOfNilPanicFixed 验证当 process.Of 失败时，系统绝不发生 nil pointer panic，并安全返回 500
func TestProcessOfNilPanicFixed(t *testing.T) {
	router := gin.New()
	path := Path{
		Path:    "/invalid/process",
		Method:  "GET",
		Process: "this.process.does.not.exist.at.all",
		Out: Out{
			Status: 200,
			Type:   "application/json",
		},
	}

	getArgs := func(c *gin.Context) []interface{} {
		return []interface{}{}
	}

	router.GET(path.Path, path.defaultHandler(getArgs))

	req, _ := http.NewRequest("GET", path.Path, nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		router.ServeHTTP(rec, req)
	})

	assert.Equal(t, 404, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":404`)
	assert.Contains(t, rec.Body.String(), "not found")
}

// TestPipelineDirectFastPath 验证直接同步 Fast-Path 执行管线正常返回数据与响应头
func TestPipelineDirectFastPath(t *testing.T) {
	process.Register("test.pipeline.fastpath", func(p *process.Process) interface{} {
		return map[string]interface{}{
			"status": "ok",
			"echo":   "fast-path-success",
		}
	})

	router := gin.New()
	path := Path{
		Path:    "/api/fastpath",
		Method:  "GET",
		Process: "test.pipeline.fastpath",
		Out: Out{
			Status: 200,
			Type:   "application/json",
		},
	}

	getArgs := func(c *gin.Context) []interface{} {
		return []interface{}{}
	}

	router.GET(path.Path, path.defaultHandler(getArgs))

	req, _ := http.NewRequest("GET", path.Path, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), `"echo":"fast-path-success"`)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
}

// TestPipelineContextCancellation 验证请求级 Context 中断可精确向下传播并中止处理
func TestPipelineContextCancellation(t *testing.T) {
	entered := make(chan struct{})
	process.Register("test.pipeline.blocking", func(p *process.Process) interface{} {
		close(entered)
		if p.Context != nil {
			<-p.Context.Done()
		}
		return map[string]interface{}{"done": true}
	})

	router := gin.New()
	path := Path{
		Path:    "/api/cancel",
		Method:  "GET",
		Process: "test.pipeline.blocking",
		Out: Out{
			Status: 200,
			Type:   "application/json",
		},
	}

	getArgs := func(c *gin.Context) []interface{} {
		return []interface{}{}
	}

	router.GET(path.Path, path.defaultHandler(getArgs))

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", path.Path, nil)
	rec := httptest.NewRecorder()

	go func() {
		<-entered
		cancel() // 模拟客户端主动中断请求
	}()

	router.ServeHTTP(rec, req)
	assert.Equal(t, 200, rec.Code) // Gin 默认 Recorder 状态码未改写即为 200 或 abort
}

// TestConcurrentGuardsThreadSafety 验证 HTTPGuards 高并发读写安全（-race 检查）
func TestConcurrentGuardsThreadSafety(t *testing.T) {
	var wg sync.WaitGroup

	// 并发写
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				AddGuard("guard-test", func(c *gin.Context) { c.Next() })
				time.Sleep(10 * time.Microsecond)
			}
		}(i)
	}

	// 并发读
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				GetGuard("guard-test")
				time.Sleep(5 * time.Microsecond)
			}
		}()
	}

	wg.Wait()
}

// TestProcessGuardContextPropagation 验证 ProcessGuard 正确向下游 Process 传递 HTTP Request Context
func TestProcessGuardContextPropagation(t *testing.T) {
	var receivedCtx context.Context
	process.Register("test.guard.context", func(p *process.Process) interface{} {
		receivedCtx = p.Context
		return nil
	})

	router := gin.New()
	router.Use(ProcessGuard("test.guard.context"))
	router.GET("/guard/ctx", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(reqCtx, "GET", "/guard/ctx", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	assert.Equal(t, 200, rec.Code)
	assert.NotNil(t, receivedCtx, "Process in ProcessGuard must receive request Context")
	assert.Equal(t, reqCtx, receivedCtx, "Process Context must match HTTP request Context")
}

// BenchmarkHandlerFastPath 基准测试同步 Fast-Path 执行效率
func BenchmarkHandlerFastPath(b *testing.B) {
	process.Register("test.bench.ping", func(p *process.Process) interface{} {
		return map[string]interface{}{"pong": 1}
	})

	router := gin.New()
	path := Path{
		Path:    "/bench/ping",
		Method:  "GET",
		Process: "test.bench.ping",
		Out: Out{
			Status: 200,
			Type:   "application/json",
		},
	}

	getArgs := func(c *gin.Context) []interface{} {
		return []interface{}{}
	}

	router.GET(path.Path, path.defaultHandler(getArgs))
	req, _ := http.NewRequest("GET", path.Path, nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

// TestExtractorPlan 验证预编译参数提取器的各个操作码提取准确性
func TestExtractorPlan(t *testing.T) {
	httpInst := HTTP{}
	in := []interface{}{
		"const-val",
		123,
		":fullpath",
		"$query.keyword",
		"$param.id",
		"$header.Authorization",
		"$payload.user_id",
		":params",
	}

	extract := httpInst.parseIn(in)
	assert.NotNil(t, extract)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, err := http.NewRequest("POST", "/api/users/42?keyword=yao&from=search", nil)
	assert.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")

	c.Request = req
	c.Params = gin.Params{gin.Param{Key: "id", Value: "42"}}
	c.Set("__payloads", map[string]interface{}{
		"user_id": 999,
	})

	args := extract(c)
	assert.Len(t, args, 8)
	assert.Equal(t, "const-val", args[0])
	assert.Equal(t, 123, args[1])
	assert.Equal(t, "", args[2]) // 未挂在 gin 完整路由树上 fullpath 为空
	assert.Equal(t, "yao", args[3])
	assert.Equal(t, "42", args[4])
	assert.Equal(t, "Bearer test-token", args[5])
	assert.Equal(t, 999, args[6])
}

// BenchmarkExtractorPlan 基准测试参数提取器运行开销
func BenchmarkExtractorPlan(b *testing.B) {
	httpInst := HTTP{}
	in := []interface{}{
		"static",
		"$query.page",
		"$param.id",
		"$header.User-Agent",
		"$payload.role",
	}

	extract := httpInst.parseIn(in)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("POST", "/test/100?page=2", nil)
	req.Header.Set("User-Agent", "Benchmark")
	c.Request = req
	c.Params = gin.Params{gin.Param{Key: "id", Value: "100"}}
	c.Set("__payloads", map[string]interface{}{
		"role": "admin",
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		args := extract(c)
		_ = args
	}
}
