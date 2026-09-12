package connector

import (
	"fmt"

	"github.com/yaoapp/gou/connector/base"
	mongo "github.com/yaoapp/gou/connector/mongo"
	"github.com/yaoapp/gou/connector/openai"
	"github.com/yaoapp/gou/connector/redis"
	gouTypes "github.com/yaoapp/gou/types"
	"github.com/yaoapp/xun/dbal/query"
	"github.com/yaoapp/xun/dbal/schema"
)

const (
	// DATABASE the database connector (mysql, pgsql, oracle, sqlite ... )
	DATABASE = iota + 1

	// REDIS the redis connector
	REDIS

	// MONGO the mongodb connector
	MONGO

	// ELASTICSEARCH the elasticsearch connector
	ELASTICSEARCH

	// KAFKA the kafka connector
	KAFKA

	// OPENAI the openai connector
	OPENAI

	// WEAVIATE the weaviate connector
	WEAVIATE

	// MOAPI the moapi connector
	MOAPI

	// FASTEMBED the fastembed connector
	FASTEMBED

	// SCRIPT ? the script connector ( difference with widget ?)
	SCRIPT
)

var types = map[string]int{
	"mysql":         DATABASE,
	"sqlite":        DATABASE,
	"sqlite3":       DATABASE,
	"postgres":      DATABASE,
	"oracle":        DATABASE,
	"redis":         REDIS,
	"mongo":         MONGO,
	"elasticsearch": ELASTICSEARCH,
	"es":            ELASTICSEARCH,
	"kafka":         KAFKA,
	"openai":        OPENAI,
	"weaviate":      WEAVIATE,
	"script":        SCRIPT, // ?
	"moapi":         MOAPI,
	"fastembed":     FASTEMBED,
}

// 导出底层基础能力接口与错误哨兵，保持 100% 透明兼容
type (
	// SQLCapability 声明 SQL 专属能力接缝
	SQLCapability = base.SQLCapability
	// NonSQL 提供非 SQL 连接器的默认 Query / Schema 报错实现，杜绝隐式 nil pointer dereference
	NonSQL = base.NonSQL
	// BaseInfo 定义连接器最小基础识别信息
	BaseInfo = base.BaseInfo
)

var (
	// ErrNotSQLConnector 当非 SQL 连接器被请求 Query 或 Schema 能力时返回
	ErrNotSQLConnector = base.ErrNotSQLConnector
	// ErrConnectorNotFound 当连接器未加载时返回
	ErrConnectorNotFound = base.ErrConnectorNotFound
	// ErrNilConnector 传入的连接器指针为 nil 时返回
	ErrNilConnector = base.ErrNilConnector
)

// SQLConnector 具备完整连接器基础与 SQL 操作能力的复合接缝
type SQLConnector interface {
	Connector
	base.SQLCapability
}

// Connector the connector interface
type Connector interface {
	Register(file string, id string, dsl []byte) error
	Query() (query.Query, error)
	Schema() (schema.Schema, error)
	Close() error
	ID() string
	Is(int) bool
	Setting() map[string]interface{}
	GetMetaInfo() gouTypes.MetaInfo
}

// AsSQL 尝试将通用 Connector 安全转换为具备 SQL 能力的 SQLConnector 接缝。
// 若连接器不是 SQL 数据库连接器，返回明确的 ErrNotSQLConnector 错误，防止下游发生空指针崩溃。
func AsSQL(c Connector) (SQLConnector, error) {
	if c == nil {
		return nil, ErrNilConnector
	}
	if sqlConn, ok := c.(SQLConnector); ok && sqlConn.IsSQL() {
		return sqlConn, nil
	}
	// 向下兼容：如果驱动仅实现了 c.Is(DATABASE)，且具备有效方法
	if c.Is(DATABASE) {
		if sqlConn, ok := c.(SQLConnector); ok {
			return sqlConn, nil
		}
	}
	return nil, fmt.Errorf("connector %q (%T) is not a SQL database connector: %w", c.ID(), c, ErrNotSQLConnector)
}

// AsRedis 尝试将通用 Connector 安全转换为 Redis 连接器
func AsRedis(c Connector) (*redis.Connector, error) {
	if c == nil {
		return nil, ErrNilConnector
	}
	if r, ok := c.(*redis.Connector); ok {
		return r, nil
	}
	return nil, fmt.Errorf("connector %q (%T) is not a Redis connector", c.ID(), c)
}

// AsMongo 尝试将通用 Connector 安全转换为 Mongo 连接器
func AsMongo(c Connector) (*mongo.Connector, error) {
	if c == nil {
		return nil, ErrNilConnector
	}
	if m, ok := c.(*mongo.Connector); ok {
		return m, nil
	}
	return nil, fmt.Errorf("connector %q (%T) is not a Mongo connector", c.ID(), c)
}

// AsOpenAI 尝试将通用 Connector 安全转换为 OpenAI 连接器
func AsOpenAI(c Connector) (*openai.Connector, error) {
	if c == nil {
		return nil, ErrNilConnector
	}
	if o, ok := c.(*openai.Connector); ok {
		return o, nil
	}
	return nil, fmt.Errorf("connector %q (%T) is not an OpenAI connector", c.ID(), c)
}


// Option the option interface
type Option struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value,omitempty"`
}

// DSL the connector DSL
type DSL struct {
	gouTypes.MetaInfo
	ID   string `json:"-"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	// Label   string                 `json:"label,omitempty"`
	Version string                 `json:"version,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}
