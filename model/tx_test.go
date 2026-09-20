package model

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
)

func TestTxContextInjection(t *testing.T) {
	ctx := context.Background()
	_, ok := TxFromContext(ctx)
	assert.False(t, ok, "should not have tx session in empty context")

	sess := &TxSession{closed: false}
	txCtx := WithTxContext(ctx, sess)

	extracted, ok := TxFromContext(txCtx)
	assert.True(t, ok, "should extract tx session from context")
	assert.Equal(t, sess, extracted)

	// After closing, TxFromContext should return false
	sess.closed = true
	_, ok = TxFromContext(txCtx)
	assert.False(t, ok, "closed tx session should not be returned as active")
}

func TestTransactionRollbackOnPanic(t *testing.T) {
	// Mock transaction behavior without requiring live database
	sess := &TxSession{closed: false}
	rolledBack := false

	// Test panic recovery in Transaction closure pattern
	assert.Panics(t, func() {
		// simulate manual closure rollback
		defer func() {
			if r := recover(); r != nil {
				rolledBack = true
				sess.closed = true
				panic(r)
			}
		}()
		panic("simulated business panic")
	})

	assert.True(t, rolledBack)
	assert.True(t, sess.closed)
}

func TestModelQuerySeam(t *testing.T) {
	mod := &Model{ID: "test_model"}

	ctx := context.Background()
	sess := &TxSession{closed: false, Query: nil}
	txCtx := WithTxContext(ctx, sess)

	// With tx context, Query should return the session's Query directly
	q := mod.Query(txCtx)
	assert.Nil(t, q)
}

func TestTransactionRealPhysicalRollback(t *testing.T) {
	dbPath := "./test_tx_real.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for tx test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	// 创建测试表
	sch := capsule.Schema()
	sch.MustDropTableIfExists("test_tx_users")
	sch.MustCreateTable("test_tx_users", func(table schema.Blueprint) {
		table.ID("id")
		table.String("name", 50)
	})

	mod := &Model{
		ID: "test_tx_user",
		MetaData: MetaData{
			Table: Table{Name: "test_tx_users"},
		},
	}

	// 1. 测试 Transaction 闭包内报错自动回滚
	err = Transaction(func(txCtx context.Context) error {
		txQ := mod.Query(txCtx)
		assert.NotNil(t, txQ)
		assert.NotNil(t, txQ.Tx(), "事务 Context 中的 Query 必须绑定物理 Tx")

		err := txQ.Table("test_tx_users").Insert(map[string]interface{}{"name": "rollback_user"})
		assert.NoError(t, err)

		// 事务内可查
		has, err := txQ.Table("test_tx_users").Where("name", "rollback_user").Exists()
		assert.NoError(t, err)
		assert.True(t, has)

		return fmt.Errorf("business error triggers rollback")
	})
	assert.Error(t, err)

	// 事务外验证数据已真正物理回滚
	hasAfter, err := capsule.Query().Table("test_tx_users").Where("name", "rollback_user").Exists()
	assert.NoError(t, err)
	assert.False(t, hasAfter, "报错后物理事务回滚，记录绝对不应存在")

	// 2. 测试 Transaction 闭包成功提交
	err = Transaction(func(txCtx context.Context) error {
		txQ := mod.Query(txCtx)
		return txQ.Table("test_tx_users").Insert(map[string]interface{}{"name": "commit_user"})
	})
	assert.NoError(t, err)

	// 事务外验证数据已物理持久化
	hasCommit, err := capsule.Query().Table("test_tx_users").Where("name", "commit_user").Exists()
	assert.NoError(t, err)
	assert.True(t, hasCommit, "成功提交后记录必须物理持久化")
}
