package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// SSEHandlerFactory 创建 SSE 路由处理函数
type SSEHandlerFactory func(Path) gin.HandlerFunc

var sseHandlerFactoryMu sync.RWMutex
var sseHandlerFactory SSEHandlerFactory

// SetSSEHandlerFactory 设置 SSE 路由处理函数工厂
func SetSSEHandlerFactory(factory SSEHandlerFactory) {
	sseHandlerFactoryMu.Lock()
	defer sseHandlerFactoryMu.Unlock()

	sseHandlerFactory = factory
}

// ResetSSEHandlerFactoryForTest 重置测试用 SSE 路由处理函数工厂
func ResetSSEHandlerFactoryForTest() {
	SetSSEHandlerFactory(nil)
}

func (path Path) sseHandler() gin.HandlerFunc {
	sseHandlerFactoryMu.RLock()
	factory := sseHandlerFactory
	sseHandlerFactoryMu.RUnlock()

	if factory == nil {
		return unavailableSSEHandler
	}

	handler := factory(path)
	if handler == nil {
		return unavailableSSEHandler
	}

	return handler
}

func unavailableSSEHandler(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"code":    http.StatusServiceUnavailable,
		"message": "authenticated sse is unavailable",
	})
	c.Abort()
}
