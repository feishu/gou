package session

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"sync"
	"time"

	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
)

// Managers 已注册会话管理器
var Managers = map[string]Manager{}
var managersLock sync.RWMutex

// Timeout 默认有效时间
var Timeout time.Duration = 3600 * time.Second

// Name 默认为会话管理器
var Name string = "buntdb"

// 注册默认的会话管理器
func init() {
	db, _ := NewBuntDB(":memory:")
	Register("buntdb", db)
	Register("memory", db)
}

// Register 注册会话管理器
func Register(name string, manger Manager) {
	manger.Init()
	managersLock.Lock()
	defer managersLock.Unlock()
	Managers[name] = manger
}

// Global 全局会话
func Global() *Session {
	return Use(Name).Expire(Timeout)
}

// Use 选用会话管理器
func Use(name string) *Session {
	managersLock.RLock()
	manager, has := Managers[name]
	if !has {
		// 优先回退到默认 Name ("buntdb")，再到 "memory"
		if defaultMgr, ok := Managers[Name]; ok {
			manager = defaultMgr
			name = Name
		} else if memMgr, ok := Managers["memory"]; ok {
			manager = memMgr
			name = "memory"
		} else {
			for k, v := range Managers {
				manager = v
				name = k
				break
			}
		}
	}
	managersLock.RUnlock()

	if manager == nil {
		db, _ := NewBuntDB(":memory:")
		db.Init()
		manager = db
		name = "buntdb"
	}

	return &Session{Manager: manager, timeout: Timeout, name: name}
}

// ID 生成SessionID
func ID() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		log.Error("Can't create session id")
	}
	return base64.URLEncoding.EncodeToString(b)
}

// AsGlobal 设定为全局
func (session *Session) AsGlobal() *Session {
	Timeout = session.timeout
	Name = session.name
	return session
}

// Expire 设定过期时间
func (session *Session) Expire(expiredAt time.Duration) *Session {
	session.timeout = expiredAt
	return session
}

// ID 选择指定 Session ID
func (session *Session) ID(id string) *Session {
	session.id = id
	return session
}

// Make 生成新的 Session ID
func (session *Session) Make() *Session {
	session.id = ID()
	return session
}

// GetID 读取 Session ID
func (session *Session) GetID() string {
	return session.id
}

// Set 设置数值
func (session *Session) Set(key string, value interface{}) error {
	return session.Manager.Set(session.id, key, value, session.timeout)
}

// MustSet 设置数值
func (session *Session) MustSet(key string, value interface{}) {
	err := session.Set(key, value)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// SetWithEx 设置数值
func (session *Session) SetWithEx(key string, value interface{}, timeout time.Duration) error {
	log.Debug("SetWithEx ID: %s KEY: %s, VALUE: %v, TS: %#v", session.id, key, value, timeout)
	return session.Manager.Set(session.id, key, value, timeout)
}

// MustSetWithEx 设置数值
func (session *Session) MustSetWithEx(key string, value interface{}, timeout time.Duration) {
	err := session.SetWithEx(key, value, timeout)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// SetMany 设置多个数值
func (session *Session) SetMany(values map[string]interface{}) error {
	if bm, ok := session.Manager.(BatchManager); ok {
		return bm.SetMany(session.id, values, session.timeout)
	}
	for key, value := range values {
		if err := session.Manager.Set(session.id, key, value, session.timeout); err != nil {
			return err
		}
	}
	return nil
}

// MustSetMany 设置多个数值
func (session *Session) MustSetMany(values map[string]interface{}) {
	err := session.SetMany(values)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// SetManyWithEx 设置多个数值
func (session *Session) SetManyWithEx(values map[string]interface{}, timeout time.Duration) error {
	if bm, ok := session.Manager.(BatchManager); ok {
		return bm.SetMany(session.id, values, timeout)
	}
	for key, value := range values {
		if err := session.Manager.Set(session.id, key, value, timeout); err != nil {
			return err
		}
	}
	return nil
}

// MustSetManyWithEx 设置多个数值
func (session *Session) MustSetManyWithEx(values map[string]interface{}, timeout time.Duration) {
	err := session.SetManyWithEx(values, timeout)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// Get 读取数值
func (session *Session) Get(key string) (interface{}, error) {
	return session.Manager.Get(session.id, key)
}

// MustGet 读取数值
func (session *Session) MustGet(key string) interface{} {
	value, err := session.Get(key)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return value
}

// GetMany 批量读取多个数值
func (session *Session) GetMany(keys []string) (map[string]interface{}, error) {
	if bm, ok := session.Manager.(BatchManager); ok {
		return bm.GetMany(session.id, keys)
	}
	res := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		val, err := session.Manager.Get(session.id, key)
		if err != nil {
			return nil, err
		}
		res[key] = val
	}
	return res, nil
}

// MustGetMany 批量读取多个数值
func (session *Session) MustGetMany(keys []string) map[string]interface{} {
	values, err := session.GetMany(keys)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return values
}

// Del 删除数值
func (session *Session) Del(key string) error {
	return session.Manager.Del(session.id, key)
}

// MustDel 删除数值
func (session *Session) MustDel(key string) {
	err := session.Del(key)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// DelMany 批量删除数值
func (session *Session) DelMany(keys []string) error {
	if bm, ok := session.Manager.(BatchManager); ok {
		return bm.DelMany(session.id, keys)
	}
	for _, key := range keys {
		if err := session.Manager.Del(session.id, key); err != nil {
			return err
		}
	}
	return nil
}

// MustDelMany 批量删除数值
func (session *Session) MustDelMany(keys []string) {
	err := session.DelMany(keys)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// Dump 导出所有数据
func (session *Session) Dump() (map[string]interface{}, error) {
	return session.Manager.Dump(session.id)
}

// MustDump 导出所有数据
func (session *Session) MustDump() map[string]interface{} {
	value, err := session.Dump()
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return value
}

// // Cookie 从Cookie中读取 Session ID
// func (session *Session) Cookie(name string) {}

// // QueryString 从QueryString中读取 Session ID
// func (session *Session) QueryString(name string) {}

// // Header 从Header中读取
// func (session *Session) Header(name string) {}

// // Map 从Map中读取 SessionID
// func (session *Session) Map(name string) {}
