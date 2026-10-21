package model

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
)

// TestDeadlockUnderMaxConns 验证连接池满载与并发事务下零死锁
// 模拟严苛的连接池限制 (MaxOpenConns=3)，并发 30 个事务执行跨表级联、数据权限注入与变动
// 验证物理事务连接在整个生命周期内严格复用，绝不泄漏额外连接导致死锁
func TestDeadlockUnderMaxConns(t *testing.T) {
	dbPath := "./test_stress.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for stress test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	primary, err := capsule.Global.Primary()
	assert.NoError(t, err)
	// 严格限制最大连接数为 3
	primary.DB.SetMaxOpenConns(3)
	primary.DB.SetMaxIdleConns(3)

	sch := capsule.Schema()
	sch.MustDropTableIfExists("test_stress_parents")
	sch.MustDropTableIfExists("test_stress_children")

	sch.MustCreateTable("test_stress_parents", func(table schema.Blueprint) {
		table.ID("id")
		table.String("name", 50)
		table.Integer("org_id")
		table.Integer("dept_id")
		table.Integer("tenant_id")
		table.Integer("created_by")
	})

	sch.MustCreateTable("test_stress_children", func(table schema.Blueprint) {
		table.ID("id")
		table.Integer("parent_id")
		table.String("title", 50)
	})

	childMod := &Model{
		ID:     "test.stress_child",
		Name:   "test.stress_child",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_stress_children"},
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
		ID:     "test.stress_parent",
		Name:   "test.stress_parent",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_stress_parents"},
			Relations: map[string]Relation{
				"children": {
					Type:    "hasMany",
					Model:   "test.stress_child",
					Key:     "id",
					Foreign: "parent_id",
					Cascade: true,
				},
			},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":         {Name: "id", Type: "id"},
			"name":       {Name: "name", Type: "string"},
			"org_id":     {Name: "org_id", Type: "integer"},
			"dept_id":    {Name: "dept_id", Type: "integer"},
			"tenant_id":  {Name: "tenant_id", Type: "integer"},
			"created_by": {Name: "created_by", Type: "integer"},
		},
		ColumnNames: []interface{}{"id", "name", "org_id", "dept_id", "tenant_id", "created_by"},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			Mode:        "enforcing",
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
			UserField:   "created_by",
		},
	}
	SetModel(parentMod.ID, parentMod)

	sec := &SecurityContext{
		UserID:   8001,
		TenantID: 1,
		OrgID:    88,
		DeptID:   101,
	}
	global := map[string]interface{}{
		"__sec_ctx": sec,
		"__sec_bindings": []RoleScopeBinding{
			{RoleID: 1, ScopeType: ScopeTypeDept, DeptIDs: []int64{101}},
		},
	}

	const concurrency = 30
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	done := make(chan struct{})
	go func() {
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				txErr := Transaction(func(txCtx context.Context) error {
					// 1. 创建父记录
					pID, err := parentMod.SecureCreate(txCtx, global, maps.MapStrAny{
						"name":    fmt.Sprintf("Parent_%d", idx),
						"dept_id": 101,
					})
					if err != nil {
						return fmt.Errorf("create parent %d error: %w", idx, err)
					}

					// 2. 创建关联子记录
					err = childMod.Query(txCtx).Table("test_stress_children").Insert(map[string]interface{}{
						"parent_id": pID,
						"title":     fmt.Sprintf("Child_%d", idx),
					})
					if err != nil {
						return fmt.Errorf("create child %d error: %w", idx, err)
					}

					// 3. 事务内通过 Find 查询 (验证事务传播与行级权限注入)
					row, err := parentMod.Find(pID, QueryParam{Context: txCtx})
					if err != nil {
						return fmt.Errorf("find parent %d error: %w", idx, err)
					}
					if row["name"] == nil {
						return fmt.Errorf("find parent %d returned empty name", idx)
					}

					// 4. 事务内安全更新
					err = parentMod.SecureUpdateByID(txCtx, global, pID, maps.MapStrAny{
						"name": fmt.Sprintf("Parent_%d_Updated", idx),
					})
					if err != nil {
						return fmt.Errorf("update parent %d error: %w", idx, err)
					}

					// 5. 级联删除子记录与父记录
					err = parentMod.SecureDeleteByID(txCtx, global, pID, DeleteOptions{Cascade: true})
					if err != nil {
						return fmt.Errorf("cascade delete parent %d error: %w", idx, err)
					}

					return nil
				})
				if txErr != nil {
					errCh <- txErr
				}
			}(i)
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(errCh)
		for err := range errCh {
			assert.NoError(t, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK DETECTED: Connection pool starved under high concurrency transactions!")
	}
}
