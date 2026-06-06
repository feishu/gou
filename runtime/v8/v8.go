package v8

import (
	"github.com/yaoapp/gou/runtime/v8/store"
	"rogchap.com/v8go"
)

var runtimeOption = &Option{}

// Start v8 runtime
func Start(option *Option) error {
	option.Validate()
	runtimeOption = option
	return initialize()
}

// Stop v8 runtime
func Stop() {
	release()
	store.Isolates.Range(func(iso store.IStore) bool {
		key := iso.Key()
		store.CleanIsolateCache(key)
		store.Isolates.Remove(key)
		return true
	})
	v8go.YaoDispose()
}
