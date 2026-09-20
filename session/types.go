package session

import "time"

// Manager Session 管理器
type Manager interface {
	Init()
	Set(id string, key string, value interface{}, expired time.Duration) error
	Get(id string, key string) (interface{}, error)
	Del(id string, key string) error
	Dump(id string) (map[string]interface{}, error)
}

// BatchManager 支持批量与聚合操作的高性能会话管理器
type BatchManager interface {
	Manager
	GetMany(id string, keys []string) (map[string]interface{}, error)
	SetMany(id string, values map[string]interface{}, expired time.Duration) error
	DelMany(id string, keys []string) error
}

// Session 数据结构
type Session struct {
	id      string
	name    string
	timeout time.Duration
	Manager Manager
}

