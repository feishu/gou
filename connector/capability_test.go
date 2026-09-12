package connector

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/connector/database"
	"github.com/yaoapp/gou/connector/fastembed"
	"github.com/yaoapp/gou/connector/moapi"
	mongo "github.com/yaoapp/gou/connector/mongo"
	"github.com/yaoapp/gou/connector/openai"
	"github.com/yaoapp/gou/connector/redis"
)

func TestNonSQLConnectorsReturnErrorOnQueryAndSchema(t *testing.T) {
	connectors := []struct {
		name string
		conn Connector
	}{
		{name: "openai", conn: &openai.Connector{}},
		{name: "redis", conn: &redis.Connector{}},
		{name: "mongo", conn: &mongo.Connector{}},
		{name: "moapi", conn: &moapi.Connector{}},
		{name: "fastembed", conn: &fastembed.Connector{}},
	}

	for _, tc := range connectors {
		t.Run(tc.name, func(t *testing.T) {
			// 验证直接调用 Query 不再返回 nil, nil，而是返回明确的 ErrNotSQLConnector 错误
			qb, err := tc.conn.Query()
			assert.Nil(t, qb, "Query builder must be nil for non-SQL connector")
			assert.Error(t, err, "Query must return error for non-SQL connector")
			assert.True(t, errors.Is(err, ErrNotSQLConnector), "Error should match ErrNotSQLConnector")

			// 验证直接调用 Schema 明确返回错误
			sch, err := tc.conn.Schema()
			assert.Nil(t, sch, "Schema builder must be nil for non-SQL connector")
			assert.Error(t, err, "Schema must return error for non-SQL connector")
			assert.True(t, errors.Is(err, ErrNotSQLConnector), "Error should match ErrNotSQLConnector")

			// 验证能力萃取器拒绝非 SQL 连接器
			sqlConn, err := AsSQL(tc.conn)
			assert.Nil(t, sqlConn)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, ErrNotSQLConnector))
		})
	}
}

func TestSQLCapabilityAndAsSQL(t *testing.T) {
	// Xun 数据库 ORM 实现了 SQLCapability
	xun := &database.Xun{}
	assert.True(t, xun.IsSQL(), "Xun database driver must have IsSQL() == true")

	// 验证 AsSQL 成功断言并识别
	sqlConn, err := AsSQL(xun)
	assert.NoError(t, err)
	assert.NotNil(t, sqlConn)
	assert.True(t, sqlConn.IsSQL())

	// 验证 nil 保护
	_, err = AsSQL(nil)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNilConnector))
}

func TestExtractors(t *testing.T) {
	rConn := &redis.Connector{}
	mConn := &mongo.Connector{}
	oConn := &openai.Connector{}

	// AsRedis
	r, err := AsRedis(rConn)
	assert.NoError(t, err)
	assert.Equal(t, rConn, r)

	_, err = AsRedis(oConn)
	assert.Error(t, err)

	_, err = AsRedis(nil)
	assert.True(t, errors.Is(err, ErrNilConnector))

	// AsMongo
	m, err := AsMongo(mConn)
	assert.NoError(t, err)
	assert.Equal(t, mConn, m)

	_, err = AsMongo(rConn)
	assert.Error(t, err)

	_, err = AsMongo(nil)
	assert.True(t, errors.Is(err, ErrNilConnector))

	// AsOpenAI
	o, err := AsOpenAI(oConn)
	assert.NoError(t, err)
	assert.Equal(t, oConn, o)

	_, err = AsOpenAI(mConn)
	assert.Error(t, err)

	_, err = AsOpenAI(nil)
	assert.True(t, errors.Is(err, ErrNilConnector))
}


func TestConcurrentRegistryAccess(t *testing.T) {
	// 重置局部注册表以测试并发安全性
	rwlock.Lock()
	Connectors = map[string]Connector{
		"base-mysql":  &database.Xun{},
		"base-redis":  &redis.Connector{},
		"base-openai": &openai.Connector{},
	}
	rwlock.Unlock()

	var wg sync.WaitGroup
	workers := 30
	iterations := 100

	// 启动大量并发读取者
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Select
				c, err := Select("base-mysql")
				if err == nil && c != nil {
					_ = c.ID()
				}

				// Count
				cnt := Count()
				assert.True(t, cnt >= 0)

				// Range
				Range(func(key string, conn Connector) bool {
					_ = conn.ID()
					return true
				})
			}
		}(w)
	}

	// 启动并发写入与删除者
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			connID := fmt.Sprintf("dynamic-conn-%d", id)
			for i := 0; i < iterations; i++ {
				rwlock.Lock()
				Connectors[connID] = &openai.Connector{}
				rwlock.Unlock()

				_, _ = Select(connID)

				rwlock.Lock()
				delete(Connectors, connID)
				rwlock.Unlock()
			}
		}(w)
	}

	wg.Wait()
}
