package process

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fatih/color"
	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/kun/exception"
)

// Handlers ProcessHanlders
var Handlers = map[string]Handler{}

var processPool = sync.Pool{
	New: func() interface{} {
		return &Process{
			Args:     make([]interface{}, 0, 8),
			fromPool: true,
		}
	},
}

// AcquireProcess 从对象池获取并初始化 Process (零额外堆逃逸)
func AcquireProcess(ctx context.Context, name string, args ...interface{}) (*Process, error) {
	p := processPool.Get().(*Process)
	p.Reset()
	p.fromPool = true
	p.Name = name
	p.Args = append(p.Args, args...)
	p.Context = ctx
	if err := p.make(); err != nil {
		p.Release()
		return nil, err
	}
	return p, nil
}

// New make a new process
func New(name string, args ...interface{}) *Process {
	process, err := Of(name, args...)
	if err != nil {
		exception.New("%s", 500, err.Error()).Throw()
	}
	return process
}

// NewWithContext make a new process with context
func NewWithContext(ctx context.Context, name string, args ...interface{}) *Process {
	process, err := Of(name, args...)
	if err != nil {
		exception.New("%s", 500, err.Error()).Throw()
	}
	process.Context = ctx
	return process
}

// Of make a new process and return error
func Of(name string, args ...interface{}) (*Process, error) {
	process := &Process{Name: name, Args: args, Global: map[string]interface{}{}}
	err := process.make()
	if err != nil {
		return nil, err
	}
	return process, nil
}

// Execute execute the process in the current goroutine synchronously with cooperative cancellation.
// Eliminates short-lived goroutines, channel allocations, and select context switches.
func (process *Process) Execute() (err error) {
	if process.Context != nil {
		if err := process.Context.Err(); err != nil {
			return err
		}
	}

	var hd Handler
	hd, err = process.handler()
	if err != nil {
		return err
	}

	defer func() {
		recovered := recover()
		if recovered != nil {
			err = exception.Catch(recovered)
			if err != nil {
				exception.DebugPrint(err, "%s", process)
			}
		}
	}()

	value := hd(process)
	process._val = &value

	// Check if context was cancelled during execution
	if process.Context != nil {
		if ctxErr := process.Context.Err(); ctxErr != nil {
			if process.Runtime != nil {
				process.Runtime.Dispose()
			}
			return ctxErr
		}
	}
	return nil
}

// ExecuteSync execute the process synchronously in the current goroutine without creating sub-goroutines
// Retained for backward compatibility.
func (process *Process) ExecuteSync() (err error) {
	return process.Execute()
}

// Reset 重置 Process 状态
func (process *Process) Reset() {
	process.Name = ""
	process.Group = ""
	process.Method = ""
	process.ID = ""
	process.Handler = ""
	process.Sid = ""
	process.Context = nil
	if process.Runtime != nil {
		process.Runtime.Dispose()
		process.Runtime = nil
	}
	process.Callback = nil
	process._val = nil
	process.Args = process.Args[:0]
	if process.Global != nil {
		for k := range process.Global {
			delete(process.Global, k)
		}
	}
}

// Release the value of the process, and return to pool if fromPool is true
func (process *Process) Release() {
	if process == nil {
		return
	}
	process._val = nil
	if process.fromPool {
		process.fromPool = false
		process.Reset()
		processPool.Put(process)
	}
}

// Dispose the process after run success
func (process *Process) Dispose() {
	if process == nil {
		return
	}
	if process.Runtime != nil {
		process.Runtime.Dispose()
		process.Runtime = nil
	}
	process._val = nil
}

// Value get the result of the process
func (process *Process) Value() interface{} {
	if process._val != nil {
		return *process._val
	}
	return nil
}

// Run the process
func (process *Process) Run() interface{} {
	defer process.Release()
	err := process.Execute()
	if err != nil {
		exception.New("%s", 500, err.Error()).Throw()
		return nil
	}
	return process.Value()
}

// Exec execute the process and return value and error
func (process *Process) Exec() (value interface{}, err error) {
	defer process.Release()
	err = process.Execute()
	if err != nil {
		return nil, err
	}
	return process.Value(), nil
}

// Register register a process handler
func Register(name string, handler Handler) {
	defaultKernel.Register(name, handler)
}

// RegisterGroup register a process handler group
func RegisterGroup(name string, group map[string]Handler) {
	defaultKernel.RegisterGroup(name, group)
}

// Alias set an alias a process
func Alias(name string, alias string) {
	err := defaultKernel.Alias(name, alias)
	if err != nil {
		exception.New("Process: %s does not exist", 404, name).Throw()
	}
}

// Exists check if the process exists
func Exists(name string) bool {
	return defaultKernel.Exists(name)
}

// WithSID set the session id
func (process *Process) WithSID(sid string) *Process {
	process.Sid = sid
	return process
}

// WithGlobal set the global vars
func (process *Process) WithGlobal(global map[string]interface{}) *Process {
	process.Global = global
	return process
}

// WithContext set the context
func (process *Process) WithContext(ctx context.Context) *Process {
	process.Context = ctx
	return process
}

// WithRuntime set the runtime interface
func (process *Process) WithRuntime(runtime Runtime) *Process {
	process.Runtime = runtime
	return process
}

// WithCallback set the callback function
func (process *Process) WithCallback(callback CallbackFunc) *Process {
	process.Callback = callback
	return process
}

// String the process as string
func (process Process) String() string {
	args, _ := jsoniter.MarshalToString(process.Args)
	global, _ := jsoniter.MarshalToString(process.Global)
	return fmt.Sprintf("%s%s\n%s%s\n%s%s\n%s%s\n",
		color.YellowString("Process: "),
		color.WhiteString(process.Name),
		color.YellowString("Sid: "),
		color.WhiteString(process.Sid),
		color.YellowString("Args: \n"),
		color.WhiteString(args),
		color.YellowString("Global: \n"),
		color.WhiteString(global),
	)
}

// handler get the process handler
func (process *Process) handler() (Handler, error) {
	if hd, has := defaultKernel.Lookup(process.Context, process.Handler); has && hd != nil {
		return defaultKernel.ApplyInterceptors(hd), nil
	}
	return nil, fmt.Errorf("Exception|404:%s Handler -> %s not found", process.Name, process.Handler)
}

// Route 静态进程路由元数据
type Route struct {
	Group   string
	Method  string
	ID      string
	Handler string
}

var routeCache sync.Map // map[string]Route

// parseRoute 解析进程路由信息
func parseRoute(name string) (Route, error) {
	fields := strings.Split(name, ".")
	if len(fields) < 2 {
		return Route{}, fmt.Errorf("Exception|404:%s not found", name)
	}

	route := Route{Group: fields[0]}
	switch route.Group {
	case "models", "schemas", "stores", "fs", "tasks", "schedules":
		route.Method = fields[len(fields)-1]
		route.ID = strings.ToLower(strings.Join(fields[1:len(fields)-1], "."))
		route.Handler = strings.ToLower(fmt.Sprintf("%s.%s", route.Group, route.Method))

	case "flows", "pipes":
		route.Handler = route.Group
		route.ID = strings.ToLower(strings.Join(fields[1:], "."))

	case "aigcs":
		if len(fields) < 2 {
			return Route{}, fmt.Errorf("Exception|404:%s not found", name)
		}
		route.Handler = strings.ToLower(route.Group)
		route.ID = strings.ToLower(strings.Join(fields[1:], "."))

	case "services":
		if len(fields) < 3 {
			return Route{}, fmt.Errorf("Exception|404:%s not found", name)
		}
		f := append([]string{"scripts", "__yao_service"}, fields[1:]...)
		route.Group = "scripts"
		route.Handler = "scripts"
		route.ID = strings.ToLower(strings.Join(f[1:len(f)-1], "."))
		route.Method = f[len(f)-1]

	case "agents", "assistants", "ai":
		if len(fields) < 3 {
			return Route{}, fmt.Errorf("Exception|404:%s not found", name)
		}
		f := append([]string{"scripts", "assistants"}, fields[1:]...)
		route.Group = "scripts"
		route.Handler = "scripts"
		route.ID = strings.ToLower(strings.Join(f[1:len(f)-1], "."))
		route.Method = f[len(f)-1]

	case "scripts", "studio", "plugins":
		if len(fields) < 3 {
			return Route{}, fmt.Errorf("Exception|404:%s not found", name)
		}
		route.Handler = strings.ToLower(route.Group)
		route.ID = strings.ToLower(strings.Join(fields[1:len(fields)-1], "."))
		route.Method = fields[len(fields)-1]

	case "session", "http":
		route.Method = fields[len(fields)-1]
		route.Handler = strings.ToLower(fmt.Sprintf("%s.%s", route.Group, route.Method))

	case "widgets":
		route.Method = fields[len(fields)-1]
		route.ID = strings.ToLower(strings.Join(fields[1:len(fields)-1], "."))
		route.Handler = strings.ToLower(fmt.Sprintf("widgets.%s.%s", route.ID, route.Method))

	default:
		route.Handler = strings.ToLower(name)
	}

	return route, nil
}

// make parse the process (使用并发安全缓存，消灭高频字符串拆分与拼装)
func (process *Process) make() error {
	if cached, ok := routeCache.Load(process.Name); ok {
		route := cached.(Route)
		process.Group = route.Group
		process.Method = route.Method
		process.ID = route.ID
		process.Handler = route.Handler
		return nil
	}

	route, err := parseRoute(process.Name)
	if err != nil {
		return err
	}
	routeCache.Store(process.Name, route)

	process.Group = route.Group
	process.Method = route.Method
	process.ID = route.ID
	process.Handler = route.Handler
	return nil
}
