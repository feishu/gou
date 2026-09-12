package process

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Interceptor 进程执行拦截器，支持挂载追踪、监控与安全包装
type Interceptor func(p *Process, next Handler) interface{}

// testScopeKey 上下文测试作用域专属键
type testScopeKeyType struct{}

var testScopeKey = testScopeKeyType{}

// Kernel 进程执行调度内核
type Kernel struct {
	mu           sync.RWMutex
	handlers     map[string]Handler
	interceptors []Interceptor
}

// defaultKernel 全局默认内核
var defaultKernel = NewKernel()

// NewKernel 创建一个独立的执行内核实例
func NewKernel() *Kernel {
	return &Kernel{
		handlers:     make(map[string]Handler),
		interceptors: make([]Interceptor, 0),
	}
}

// Register 向内核注册处理器
func (k *Kernel) Register(name string, handler Handler) {
	name = strings.ToLower(name)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.handlers[name] = handler
	Handlers[name] = handler // 保证存量外部直接引用 Handlers 兼容
}

// RegisterGroup 注册一组处理器
func (k *Kernel) RegisterGroup(name string, group map[string]Handler) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for method, handler := range group {
		id := fmt.Sprintf("%s.%s", strings.ToLower(name), strings.ToLower(method))
		k.handlers[id] = handler
		Handlers[id] = handler
	}
}

// Alias 别名绑定
func (k *Kernel) Alias(name string, alias string) error {
	name = strings.ToLower(name)
	alias = strings.ToLower(alias)

	k.mu.Lock()
	defer k.mu.Unlock()
	if handler, has := k.handlers[name]; has {
		k.handlers[alias] = handler
		Handlers[alias] = handler
		return nil
	}
	return fmt.Errorf("Process: %s does not exist", name)
}

// Exists 检查处理器是否存在
func (k *Kernel) Exists(name string) bool {
	if strings.HasPrefix(name, "scripts.") || strings.HasPrefix(name, "assistants.") || strings.HasPrefix(name, "agents.") || strings.HasPrefix(name, "ai.") || strings.HasPrefix(name, "services.") {
		return true
	}

	name = strings.ToLower(name)
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.handlers[name] != nil
}

// Lookup 获取处理器，优先使用 Context 中通过测试接缝挂载的 Mock
func (k *Kernel) Lookup(ctx context.Context, handlerName string) (Handler, bool) {
	// 1. 优先从 Context 测试接缝查找
	if ctx != nil {
		if scope, ok := ctx.Value(testScopeKey).(map[string]Handler); ok && scope != nil {
			if h, has := scope[handlerName]; has && h != nil {
				return h, true
			}
		}
	}

	// 2. 从内核全局注册表查找
	k.mu.RLock()
	defer k.mu.RUnlock()
	h, has := k.handlers[handlerName]
	return h, has
}

// Use 注册全局执行拦截器
func (k *Kernel) Use(interceptor Interceptor) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.interceptors = append(k.interceptors, interceptor)
}

// ApplyInterceptors 应用拦截器链
func (k *Kernel) ApplyInterceptors(base Handler) Handler {
	k.mu.RLock()
	count := len(k.interceptors)
	if count == 0 {
		k.mu.RUnlock()
		return base
	}
	interceptors := make([]Interceptor, count)
	copy(interceptors, k.interceptors)
	k.mu.RUnlock()

	chained := base
	for i := count - 1; i >= 0; i-- {
		fn := interceptors[i]
		next := chained
		chained = func(p *Process) interface{} {
			return fn(p, next)
		}
	}
	return chained
}

// WithTestScope 在当前 Context 中挂载临时测试接缝（Test Seam）
// 仅在当前 Context 请求链路中生效，不污染全局状态，支持并发测试安全隔离
func WithTestScope(ctx context.Context, mocks map[string]Handler) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	lowerMocks := make(map[string]Handler, len(mocks))
	for k, v := range mocks {
		lowerMocks[strings.ToLower(k)] = v
	}

	// 如果父 context 已存在测试作用域，合并覆盖
	if parentScope, ok := ctx.Value(testScopeKey).(map[string]Handler); ok && parentScope != nil {
		merged := make(map[string]Handler, len(parentScope)+len(lowerMocks))
		for k, v := range parentScope {
			merged[k] = v
		}
		for k, v := range lowerMocks {
			merged[k] = v
		}
		return context.WithValue(ctx, testScopeKey, merged)
	}

	return context.WithValue(ctx, testScopeKey, lowerMocks)
}

// Use 注册全局 Process 执行拦截器
func Use(interceptor Interceptor) {
	defaultKernel.Use(interceptor)
}
