package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

// MaxBodySize 默认请求体上限 (32MB)
var MaxBodySize int64 = 32 << 20

func init() {
	if val := os.Getenv("YAO_MAX_BODY_SIZE"); val != "" {
		if size, err := strconv.ParseInt(val, 10, 64); err == nil && size >= 0 {
			MaxBodySize = size
		}
	} else if val := os.Getenv("GOU_MAX_BODY_SIZE"); val != "" {
		if size, err := strconv.ParseInt(val, 10, 64); err == nil && size >= 0 {
			MaxBodySize = size
		}
	}
}

// SetMaxBodySize 设置全局请求体上限（字节数，0表示不限制）
func SetMaxBodySize(size int64) {
	MaxBodySize = size
}

// ReadBodyBytes 安全读取请求体并缓存，受 MaxBodySize 保护
func ReadBodyBytes(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}

	if v, has := c.Get("__raw_body_bytes"); has {
		if b, ok := v.([]byte); ok {
			return b, nil
		}
	}

	var reader io.Reader = c.Request.Body
	if MaxBodySize > 0 {
		if c.Writer != nil {
			reader = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodySize)
		} else {
			reader = io.LimitReader(c.Request.Body, MaxBodySize+1)
		}
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	if MaxBodySize > 0 && int64(len(bodyBytes)) > MaxBodySize {
		return nil, &http.MaxBytesError{Limit: MaxBodySize}
	}

	c.Set("__raw_body_bytes", bodyBytes)
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	return bodyBytes, nil
}

// HTTPGuards 支持的中间件
var HTTPGuards = map[string]gin.HandlerFunc{}
var guardsLock sync.RWMutex

var registeredOptions = map[string]bool{}
var optionsLock sync.Mutex

// GetGuard 获取中间件（并发安全）
func GetGuard(name string) (gin.HandlerFunc, bool) {
	guardsLock.RLock()
	defer guardsLock.RUnlock()
	handler, has := HTTPGuards[name]
	return handler, has
}

// ProcessGuard guard process
func ProcessGuard(name string, cors ...gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body interface{}
		if c.Request.Body != nil {
			bodyBytes, err := ReadBodyBytes(c)
			if err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					c.JSON(http.StatusRequestEntityTooLarge, gin.H{
						"code":    http.StatusRequestEntityTooLarge,
						"message": fmt.Sprintf("Request entity too large, max body size is %d bytes", MaxBodySize),
					})
					c.Abort()
					return
				}
				c.JSON(http.StatusBadRequest, gin.H{
					"code":    http.StatusBadRequest,
					"message": err.Error(),
				})
				c.Abort()
				return
			}

			if bodyBytes != nil {
				if strings.HasPrefix(strings.ToLower(c.Request.Header.Get("Content-Type")), "application/json") {
					var parsed map[string]interface{}
					if err := jsoniter.Unmarshal(bodyBytes, &parsed); err == nil {
						body = parsed
						c.Set("__payloads", parsed)
					} else {
						body = string(bodyBytes)
					}
				} else {
					body = string(bodyBytes)
				}
			}
		}

		params := map[string]string{}
		for _, param := range c.Params {
			params[param.Key] = param.Value
		}

		args := []interface{}{
			c.FullPath(),          // api path
			params,                // api params
			c.Request.URL.Query(), // query string
			body,                  // payload
			c.Request.Header,      // Request headers
		}

		process, err := process.Of(name, args...)
		if err != nil {
			if len(cors) > 0 {
				cors[0](c)
			}
			ex := exception.Err(err, 500)
			c.JSON(ex.Code, gin.H{"code": ex.Code, "message": ex.Message})
			c.Abort()
			return
		}
		defer process.Release()

		if sid, has := c.Get("__sid"); has { // Set session id
			if sid, ok := sid.(string); ok {
				process.WithSID(sid)
			}
		}

		if global, has := c.Get("__global"); has { // Set global variables
			if global, ok := global.(map[string]interface{}); ok {
				process.WithGlobal(global)
			}
		}

		process.WithContext(c.Request.Context())

		err = process.Execute()
		if err != nil {
			if len(cors) > 0 {
				cors[0](c)
			}
			ex := exception.Err(err, 500)
			c.JSON(ex.Code, gin.H{"code": ex.Code, "message": ex.Message})
			c.Abort()
			return
		}

		v := process.Value()
		if data, ok := v.(map[string]interface{}); ok {
			if sid, ok := data["sid"].(string); ok {
				c.Set("__sid", sid)
			}

			if global, ok := data["__global"].(map[string]interface{}); ok {
				c.Set("__global", global)
			}
		}
	}
}

// get the origin
func getOrigin(c *gin.Context) string {
	referer := c.Request.Referer()
	origin := c.Request.Header.Get("Origin")
	if origin == "" {
		origin = referer
	}
	return origin
}

// IsAllowed check if the referer is in allow list
func IsAllowed(c *gin.Context, allowsMap map[string]bool) bool {
	origin := getOrigin(c)
	if origin != "" {
		url, err := url.Parse(origin)
		if err != nil {
			return true
		}

		port := fmt.Sprintf(":%s", url.Port())
		if port == ":" || port == ":80" || port == ":443" {
			port = ""
		}
		host := fmt.Sprintf("%s%s", url.Hostname(), port)
		// fmt.Println(url, host, c.Request.Host)
		// fmt.Println(allowsMap)
		if host == c.Request.Host {
			return true
		}
		if _, has := allowsMap[host]; !has {
			return false
		}
	}
	return true

}

// Routes 配置转换为路由
func (http HTTP) Routes(router *gin.Engine, path string, allows ...string) {
	var group gin.IRoutes = router
	if http.Group != "" {
		path = filepath.Join(path, "/", http.Group)
	}
	group = router.Group(path)
	for _, path := range http.Paths {
		path.Method = strings.ToUpper(path.Method)
		http.Route(group, path, allows...)
	}
	optionsLock.Lock()
	registeredOptions = map[string]bool{}
	optionsLock.Unlock()
}

// Route 路径配置转换为路由
func (http HTTP) Route(router gin.IRoutes, path Path, allows ...string) {
	getArgs := http.parseIn(path.In)
	handlers := []gin.HandlerFunc{}

	// 跨域访问
	if allows != nil && len(allows) > 0 {
		allowsMap := map[string]bool{}
		for _, allow := range allows {
			allowsMap[allow] = true
		}

		// Cross domain
		http.setCorsOption(path.Path, allowsMap, router)
		handlers = append(handlers, func(c *gin.Context) {
			origin := getOrigin(c)
			if origin != "" {

				if !IsAllowed(c, allowsMap) {
					c.JSON(403, gin.H{"code": 403, "message": "referer is not allowed. allows: " + strings.Join(allows, ",")})
					c.Abort()
					return
				}

				// url parse
				url, _ := url.Parse(origin)
				origin = fmt.Sprintf("%s://%s", url.Scheme, url.Host)
				// fmt.Println("referer is:", referer)
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
				c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
				c.Writer.Header().Set("Access-Control-Allow-Methods", allowMethods)
			}
		})
	}

	// set middlewares
	http.guard(&handlers, path.Guard, http.Guard)

	// set http handler
	if path.Out.Redirect != nil {
		handlers = append(handlers, path.redirectHandler(getArgs))

	} else if path.SSE != nil {
		handlers = append(handlers, path.sseHandler())

	} else if path.ProcessHandler {
		handlers = append(handlers, path.processHandler())

	} else if strings.HasPrefix(path.Out.Type, "text/event-stream") {
		handlers = append(handlers, path.streamHandler(getArgs))

	} else {
		handlers = append(handlers, path.defaultHandler(getArgs))
	}

	http.method(path.Method, path.Path, router, handlers...)
}

// 加载特定中间件
func (http HTTP) guard(handlers *[]gin.HandlerFunc, guard string, defaults string) {

	if guard == "" {
		guard = defaults
	}

	if guard != "-" {
		guards := strings.Split(guard, ",")
		for _, name := range guards {
			name = strings.TrimSpace(name)
			if handler, has := GetGuard(name); has {
				*handlers = append(*handlers, handler)
			} else { // run process process
				*handlers = append(*handlers, ProcessGuard(name))
			}
		}
	}
}

// setCorsOption 跨域许可
func (http HTTP) setCorsOption(path string, allows map[string]bool, router gin.IRoutes) {
	key := fmt.Sprintf("%s.%s", http.Name, path)
	optionsLock.Lock()
	if registeredOptions[key] {
		optionsLock.Unlock()
		return
	}
	registeredOptions[key] = true
	optionsLock.Unlock()
	http.method("OPTIONS", path, router, func(c *gin.Context) {
		referer := c.Request.Referer()
		if referer != "" {
			if !IsAllowed(c, allows) {
				c.AbortWithStatus(403)
				return
			}

			// url parse
			url, _ := url.Parse(referer)
			referer = fmt.Sprintf("%s://%s", url.Scheme, url.Host)
			c.Writer.Header().Set("Access-Control-Allow-Origin", referer)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			c.Writer.Header().Set("Access-Control-Allow-Methods", allowMethods)
			c.AbortWithStatus(204)
		}
	})
}

// parseIn 接口传参解析 (通过预编译 ExtractorPlan 消除中间闭包与动态扩容)
func (http HTTP) parseIn(in []interface{}) func(c *gin.Context) []interface{} {
	plan := CompileExtractorPlan(in)
	return plan.Execute
}

// router 方法设定
func (http HTTP) method(name string, path string, router gin.IRoutes, handlers ...gin.HandlerFunc) {
	switch name {
	case "POST":
		router.POST(path, handlers...)
		return
	case "GET":
		router.GET(path, handlers...)
		return
	case "PUT":
		router.PUT(path, handlers...)
		return
	case "DELETE":
		router.DELETE(path, handlers...)
		return
	case "HEAD":
		router.HEAD(path, handlers...)
		return
	case "ANY":
		router.Any(path, handlers...)
		return
	case "OPTIONS":
		router.OPTIONS(path, handlers...)
		return
	}
}
