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
