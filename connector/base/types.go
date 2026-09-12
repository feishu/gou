package base

import (
	"errors"

	gouTypes "github.com/yaoapp/gou/types"
	"github.com/yaoapp/xun/dbal/query"
	"github.com/yaoapp/xun/dbal/schema"
)

var (
	// ErrNotSQLConnector 当非 SQL 连接器被请求 Query 或 Schema 能力时返回该错误
	ErrNotSQLConnector = errors.New("connector is not a SQL database connector")

	// ErrConnectorNotFound 当请求的连接器未找到时返回
	ErrConnectorNotFound = errors.New("connector not loaded")

	// ErrNilConnector 传入的连接器指针为 nil
	ErrNilConnector = errors.New("connector is nil")
)

// SQLCapability 声明 SQL 专属能力接缝
type SQLCapability interface {
	IsSQL() bool
	Query() (query.Query, error)
	Schema() (schema.Schema, error)
}

// NonSQL 提供非 SQL 连接器的默认 Query / Schema 实现。
// 嵌入该结构体的驱动无需手写哑桩，并在被非法调用时返回明确错误。
type NonSQL struct{}

// IsSQL 显式标记非 SQL 连接器
func (NonSQL) IsSQL() bool {
	return false
}

// Query 对非 SQL 连接器返回明确的 ErrNotSQLConnector 错误，防止返回 nil 导致下游空指针 panic
func (NonSQL) Query() (query.Query, error) {
	return nil, ErrNotSQLConnector
}

// Schema 对非 SQL 连接器返回明确的 ErrNotSQLConnector 错误
func (NonSQL) Schema() (schema.Schema, error) {
	return nil, ErrNotSQLConnector
}

// BaseInfo 定义连接器最小基础识别信息
type BaseInfo interface {
	ID() string
	Close() error
	Setting() map[string]interface{}
	GetMetaInfo() gouTypes.MetaInfo
}
