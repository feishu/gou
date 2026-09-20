package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

func TestMaxBodySizePayloadTooLarge(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	oldLimit := MaxBodySize
	defer func() {
		SetMaxBodySize(oldLimit)
	}()

	// 设置最大 Body 为 100 字节
	SetMaxBodySize(100)

	// 测试 1: setPayload 拦截超限请求
	router := gin.New()
	p := Path{
		Path:   "/test/limit",
		Method: "POST",
		In:     []interface{}{":payload"},
		Out: Out{
			Status: 200,
			Type:   "application/json",
		},
		Process: "scripts.test.api.Echo",
	}
	router.POST("/test/limit", p.defaultHandler(func(c *gin.Context) []interface{} {
		return []interface{}{}
	}))

	// 构造超过 100 字节的 JSON
	largeJSON := `{"data":"` + strings.Repeat("a", 150) + `"}`
	req, _ := http.NewRequest("POST", "/test/limit", bytes.NewBufferString(largeJSON))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "Request entity too large")

	// 测试 2: 正常未超限请求不被拦截
	smallJSON := `{"data":"hello"}`
	reqSmall, _ := http.NewRequest("POST", "/test/limit", bytes.NewBufferString(smallJSON))
	reqSmall.Header.Set("Content-Type", "application/json")

	wSmall := httptest.NewRecorder()
	router.ServeHTTP(wSmall, reqSmall)
	assert.NotEqual(t, http.StatusRequestEntityTooLarge, wSmall.Code)
}

func TestMaxBodySizeProcessGuard(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	oldLimit := MaxBodySize
	defer func() {
		SetMaxBodySize(oldLimit)
	}()

	// 设置最大 Body 为 50 字节
	SetMaxBodySize(50)

	router := gin.New()
	router.POST("/test/guard", ProcessGuard("scripts.test.api.Guard"), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// 构造超过 50 字节的请求体
	largeBody := strings.Repeat("x", 80)
	req, _ := http.NewRequest("POST", "/test/guard", bytes.NewBufferString(largeBody))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "Request entity too large")
}

func TestReadBodyBytesLimit(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	oldLimit := MaxBodySize
	defer func() {
		SetMaxBodySize(oldLimit)
	}()

	SetMaxBodySize(20)

	// 模拟超过限制的读取
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/test", io.NopCloser(strings.NewReader(strings.Repeat("1", 30))))

	data, err := ReadBodyBytes(c)
	assert.Error(t, err)
	assert.Nil(t, data)
	var maxErr *http.MaxBytesError
	assert.ErrorAs(t, err, &maxErr)
}

func TestOpExtractBodyLimit(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	oldLimit := MaxBodySize
	defer func() {
		SetMaxBodySize(oldLimit)
	}()

	SetMaxBodySize(30)

	plan := CompileExtractorPlan([]interface{}{":body"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/test", io.NopCloser(strings.NewReader(strings.Repeat("a", 50))))

	assert.Panics(t, func() {
		plan.Execute(c)
	})
}

func TestProcessGuardExceptionStatusCode(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	process.Register("test.api.unauthorized_guard", func(p *process.Process) interface{} {
		exception.New("Unauthorized token expired", 401).Throw()
		return nil
	})

	router := gin.New()
	router.GET("/test/unauthorized", ProcessGuard("test.api.unauthorized_guard"), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req, _ := http.NewRequest("GET", "/test/unauthorized", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
	assert.Contains(t, w.Body.String(), "Unauthorized token expired")
	assert.Contains(t, w.Body.String(), `"code":401`)
}

