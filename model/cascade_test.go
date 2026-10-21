package model

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
)

func TestSafeCascadeDeleteBatch(t *testing.T) {
	dbPath := "./test_cascade.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for cascade test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	sch := capsule.Schema()
	sch.MustDropTableIfExists("test_orders")
	sch.MustDropTableIfExists("test_order_items")
	sch.MustDropTableIfExists("test_order_logs")
	sch.MustDropTableIfExists("test_protected_users")

	sch.MustCreateTable("test_orders", func(table schema.Blueprint) {
		table.ID("id")
		table.String("order_no", 50)
	})

	sch.MustCreateTable("test_order_items", func(table schema.Blueprint) {
		table.ID("id")
		table.Integer("order_id").Null()
		table.String("item_name", 50)
	})

	sch.MustCreateTable("test_order_logs", func(table schema.Blueprint) {
		table.ID("id")
		table.Integer("order_id").Null()
		table.String("log_msg", 100)
	})

	sch.MustCreateTable("test_protected_users", func(table schema.Blueprint) {
		table.ID("id")
		table.String("username", 50)
	})

	// 注册模型
	itemMod := &Model{
		ID:     "test.order_item",
		Name:   "test.order_item",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_order_items"},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":        {Name: "id", Type: "id"},
			"order_id":  {Name: "order_id", Type: "integer"},
			"item_name": {Name: "item_name", Type: "string"},
		},
	}
	SetModel(itemMod.ID, itemMod)

	logMod := &Model{
		ID:     "test.order_log",
		Name:   "test.order_log",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_order_logs"},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":       {Name: "id", Type: "id"},
			"order_id": {Name: "order_id", Type: "integer"},
			"log_msg":  {Name: "log_msg", Type: "string"},
		},
	}
	SetModel(logMod.ID, logMod)

	orderMod := &Model{
		ID:     "test.order",
		Name:   "test.order",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_orders"},
			Relations: map[string]Relation{
				"items": {
					Type:    "hasMany",
					Model:   "test.order_item",
					Key:     "id",
					Foreign: "order_id",
					Cascade: true, // 开启静态级联
				},
				"logs": {
					Type:    "hasMany",
					Model:   "test.order_log",
					Key:     "id",
					Foreign: "order_id",
					Cascade: false, // 严禁级联删除（反向级联防护）
				},
			},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":       {Name: "id", Type: "id"},
			"order_no": {Name: "order_no", Type: "string"},
		},
	}
	SetModel(orderMod.ID, orderMod)

	protectedMod := &Model{
		ID:        "test.protected_user",
		Name:      "test.protected_user",
		Driver:    "sqlite3",
		Protected: true, // 核心受保护模型
		MetaData: MetaData{
			Table:     Table{Name: "test_protected_users"},
			Protected: true,
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":       {Name: "id", Type: "id"},
			"username": {Name: "username", Type: "string"},
		},
	}
	SetModel(protectedMod.ID, protectedMod)

	// 1. 初始化测试数据: order 1 有 2 个 items, 1 个 log
	err = capsule.Query().Table("test_orders").Insert(map[string]interface{}{"id": 1, "order_no": "ORD001"})
	assert.NoError(t, err)
	err = capsule.Query().Table("test_order_items").Insert([]map[string]interface{}{
		{"id": 101, "order_id": 1, "item_name": "Apple"},
		{"id": 102, "order_id": 1, "item_name": "Banana"},
	})
	assert.NoError(t, err)
	err = capsule.Query().Table("test_order_logs").Insert(map[string]interface{}{
		"id": 201, "order_id": 1, "log_msg": "Order created",
	})
	assert.NoError(t, err)

	ctx := context.Background()

	// 2. 测试受保护模型防误删: protectedMod 执行级联或删除必报 ErrModelProtected
	err = protectedMod.ExecuteSafeCascadeDeleteBatch(ctx, 1, DeleteOptions{}, 1)
	assert.ErrorIs(t, err, ErrModelProtected)
	err = protectedMod.SecureDeleteByID(ctx, nil, 1)
	assert.ErrorIs(t, err, ErrModelProtected)

	// 3. 测试级联深度超限熔断
	err = orderMod.ExecuteSafeCascadeDeleteBatch(ctx, 1, DeleteOptions{}, MaxCascadeDepth+1)
	assert.ErrorIs(t, err, ErrCascadeDepthExceeded)

	// 4. 测试动态抑制级联: opts.Cascade = false 时，即便 DSL 声明 cascade: true 也不级联
	optsSuppressed := DeleteOptions{
		Cascade: false,
	}
	err = orderMod.ExecuteSafeCascadeDeleteBatch(ctx, 1, optsSuppressed, 1)
	assert.NoError(t, err)
	itemCount, _ := capsule.Query().Table("test_order_items").Where("order_id", 1).Count()
	assert.Equal(t, int64(2), itemCount, "Items must not be deleted when cascade is suppressed")

	// 5. 测试外键置空: logs 声明 cascade: false，但启用 ClearForeignKeys: true
	optsClearFK := DeleteOptions{
		ClearForeignKeys: true,
	}
	err = orderMod.ExecuteSafeCascadeDeleteBatch(ctx, 1, optsClearFK, 1)
	assert.NoError(t, err)
	// items (cascade: true) 会被删除
	itemCountAfter, _ := capsule.Query().Table("test_order_items").Where("order_id", 1).Count()
	assert.Equal(t, int64(0), itemCountAfter, "Items with cascade: true should be deleted")
	// logs (cascade: false) 不会被删除，但 order_id 应该被置空为 null
	logRow, err := capsule.Query().Table("test_order_logs").Where("id", 201).First()
	assert.NoError(t, err)
	assert.Nil(t, logRow["order_id"], "order_id on logs should be cleared to NULL")
}

func TestCascadeFalseSuppression(t *testing.T) {
	dbPath := "./test_cascade_false.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for cascade test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	sch := capsule.Schema()
	sch.MustDropTableIfExists("test_doc_parents")
	sch.MustDropTableIfExists("test_doc_children")

	sch.MustCreateTable("test_doc_parents", func(table schema.Blueprint) {
		table.ID("id")
		table.String("name", 50)
	})

	sch.MustCreateTable("test_doc_children", func(table schema.Blueprint) {
		table.ID("id")
		table.Integer("parent_id")
		table.String("title", 50)
	})

	childMod := &Model{
		ID:     "test.doc_child",
		Name:   "test.doc_child",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_doc_children"},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":        {Name: "id", Type: "id"},
			"parent_id": {Name: "parent_id", Type: "integer"},
			"title":     {Name: "title", Type: "string"},
		},
	}
	SetModel(childMod.ID, childMod)

	parentMod := &Model{
		ID:     "test.doc_parent",
		Name:   "test.doc_parent",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_doc_parents"},
			Relations: map[string]Relation{
				"children": {
					Type:    "hasMany",
					Model:   "test.doc_child",
					Key:     "id",
					Foreign: "parent_id",
					Cascade: false, // 严禁级联删除！
				},
			},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":   {Name: "id", Type: "id"},
			"name": {Name: "name", Type: "string"},
		},
	}
	SetModel(parentMod.ID, parentMod)

	_ = capsule.Query().Table("test_doc_parents").Insert(map[string]interface{}{"id": 10, "name": "Parent10"})
	_ = capsule.Query().Table("test_doc_children").Insert([]map[string]interface{}{
		{"id": 1, "parent_id": 10, "title": "Child1"},
		{"id": 2, "parent_id": 10, "title": "Child2"},
	})

	ctx := context.Background()

	// 默认执行级联删除：因为 cascade: false，子表数据必须完整保留，不能被误删！
	err = parentMod.ExecuteSafeCascadeDeleteBatch(ctx, 10, DeleteOptions{}, 1)
	assert.NoError(t, err)

	childCount, err := capsule.Query().Table("test_doc_children").Where("parent_id", 10).Count()
	assert.NoError(t, err)
	assert.Equal(t, int64(2), childCount, "Child records must not be deleted when relation has cascade: false")
}

