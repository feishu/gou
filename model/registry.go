package model

import (
	"sync"
	"sync/atomic"
)

// ModelRegistry 不可变快照容器
type ModelRegistry struct {
	generation uint64
	models     map[string]*Model
}

var currentRegistry atomic.Pointer[ModelRegistry]
var registryMu sync.Mutex

func init() {
	initial := &ModelRegistry{
		generation: 1,
		models:     make(map[string]*Model),
	}
	currentRegistry.Store(initial)
}

// CurrentRegistry 获取当前活跃快照
func CurrentRegistry() *ModelRegistry {
	return currentRegistry.Load()
}

// SelectFast 高频查询模型（100% 纯无锁原子读）
func SelectFast(id string) (*Model, bool) {
	reg := currentRegistry.Load()
	mod, ok := reg.models[id]
	return mod, ok
}

// ListModels 快速获取所有模型切片快照（零读锁，纯无锁）
func ListModels() []*Model {
	reg := currentRegistry.Load()
	list := make([]*Model, 0, len(reg.models))
	for _, mod := range reg.models {
		list = append(list, mod)
	}
	return list
}

// SwapRegistry 原子切换模型快照并保持旧 Models map 兼容
func SwapRegistry(updater func(prev map[string]*Model) map[string]*Model) uint64 {
	registryMu.Lock()
	defer registryMu.Unlock()

	oldReg := currentRegistry.Load()
	newMap := updater(oldReg.models)

	newReg := &ModelRegistry{
		generation: oldReg.generation + 1,
		models:     newMap,
	}
	currentRegistry.Store(newReg)

	// 同步写回全局兼容变量 Models（在写锁保护下）
	rwlock.Lock()
	Models = newMap
	rwlock.Unlock()

	return newReg.generation
}

// SetModel 向注册表安全添加或替换模型 (COW)
func SetModel(id string, mod *Model) {
	SwapRegistry(func(prev map[string]*Model) map[string]*Model {
		next := make(map[string]*Model, len(prev)+1)
		for k, v := range prev {
			next[k] = v
		}
		next[id] = mod
		return next
	})
}

// UnsetModel 从注册表安全移除模型 (COW)
func UnsetModel(id string) {
	SwapRegistry(func(prev map[string]*Model) map[string]*Model {
		next := make(map[string]*Model, len(prev))
		for k, v := range prev {
			if k != id {
				next[k] = v
			}
		}
		return next
	})
}

// Generation 获取当前代际版本
func Generation() uint64 {
	return currentRegistry.Load().generation
}
