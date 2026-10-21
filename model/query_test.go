package model

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/xun/capsule"
)

// func TestQueryWhere(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "mobile",
// 				Value:  "13900001111",
// 			},
// 			{
// 				Column: "type",
// 				Value:  "admin",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	stack.Run()

// }

// func TestQueryOrWhere(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			}, {
// 				Column: "mobile",
// 				Value:  "13900002222",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	stack.Run()
// }

// func TestQueryHasOne(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"manu": {
// 				Name: "manu",
// 				Query: QueryParam{
// 					Select: []interface{}{"name", "status", "short_name"},
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			}, {
// 				Column: "mobile",
// 				Value:  "13900002222",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Run()
// 	utils.Dump(res)
// }
// func TestQueryHasOneWhere(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"manu": {
// 				Name: "manu",
// 				Query: QueryParam{
// 					Select: []interface{}{"name", "status", "short_name"},
// 					Wheres: []QueryWhere{
// 						{
// 							Column: "status",
// 							Method: "where",
// 							Value:  "disabled",
// 						}},
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			}, {
// 				Column: "mobile",
// 				Value:  "13900002222",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	stack.Run()
// 	res := stack.Run()
// 	utils.Dump(res)
// }

// func TestQueryHasOneRel(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"manu": {
// 				Name: "manu",
// 				Query: QueryParam{
// 					Select: []interface{}{"name", "status", "short_name"},
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			}, {
// 				Rel:    "manu",
// 				Column: "short_name",
// 				Value:  "云道天成",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Run()
// 	utils.Dump(res)

// }

// func TestQueryHasOneThrough(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"mother": {Name: "mother"},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type", "id"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Run()
// 	utils.Dump(res)
// }

// func TestQueryHasOneThroughWhere(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"mother": {},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type", "id"},
// 		Wheres: []QueryWhere{
// 			{
// 				Rel:    "mother.friends",
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Run()
// 	utils.Dump(res)
// }

// func TestQueryHasMany(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"addresses": {
// 				Query: QueryParam{
// 					Select:   []interface{}{"province", "city", "location", "status"},
// 					PageSize: 20,
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type", "extra"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			}, {
// 				Column: "mobile",
// 				Value:  "13900002222",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Run()
// 	utils.Dump(res)

// }

// func TestQueryHasManyAndOne(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"manu": {
// 				Query: QueryParam{
// 					Select: []interface{}{"name", "status", "short_name"},
// 				},
// 			},
// 			"addresses": {
// 				Query: QueryParam{
// 					Select:   []interface{}{"province", "city", "location", "status"},
// 					PageSize: 20,
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type", "extra"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 			{
// 				Wheres: []QueryWhere{
// 					{
// 						Column: "type",
// 						Method: "where",
// 						Value:  "admin",
// 					},
// 					{
// 						Column: "type",
// 						Method: "orWhere",
// 						Value:  "staff",
// 					},
// 				},
// 			}, {
// 				Column: "mobile",
// 				Value:  "13900002222",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Run()
// 	utils.Dump(res)
// }

// func TestQueryHasManyAndOnePaginate(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"manu": {
// 				Query: QueryParam{
// 					Select: []interface{}{"name", "status", "short_name"},
// 				},
// 			},
// 			"addresses": {
// 				Query: QueryParam{
// 					Select:   []interface{}{"province", "city", "location", "status"},
// 					PageSize: 20,
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type", "extra"},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Paginate(1, 2)
// 	utils.Dump(res)
// }

// func TestQueryHasManyAndOnePaginateOrder(t *testing.T) {
// 	param := QueryParam{
// 		Model: "user",
// 		Withs: map[string]With{
// 			"manu": {
// 				Query: QueryParam{
// 					Select: []interface{}{"name", "status", "short_name"},
// 				},
// 			},
// 			"addresses": {
// 				Query: QueryParam{
// 					Select:   []interface{}{"province", "city", "location", "status"},
// 					PageSize: 20,
// 				},
// 			},
// 		},
// 		Select: []interface{}{"name", "secret", "status", "type", "extra"},
// 		Orders: []QueryOrder{
// 			{
// 				Column: "id",
// 				Option: "desc",
// 			},
// 		},
// 		Wheres: []QueryWhere{
// 			{
// 				Column: "status",
// 				Value:  "enabled",
// 			},
// 		},
// 	}
// 	stack := NewQueryStack(param)
// 	res := stack.Paginate(1, 2)
// 	utils.Dump(res)
// }

func TestQueryStackWhereInAST(t *testing.T) {
	dbPath := "./test_query_ast.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for query test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	mod := &Model{
		ID: "test_user",
		MetaData: MetaData{
			Table: Table{Name: "users"},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":      {Name: "id", Type: "id"},
			"dept_id": {Name: "dept_id", Type: "integer"},
			"status":  {Name: "status", Type: "string"},
		},
		ColumnNames: []interface{}{"id", "dept_id", "status"},
	}

	SetModel(mod.ID, mod)

	param := QueryParam{
		Model: "test_user",
		Table: "users",
		Alias: "users",
		Select: []interface{}{"id", "dept_id"},
		Wheres: []QueryWhere{
			{
				Method: "wherein",
				Column: "dept_id",
				Value:  []int64{101, 102, 103},
			},
			{
				Method: "orwherein",
				Column: "dept_id",
				Value:  []int64{201, 202},
			},
		},
	}

	stack := param.NewQuery()
	assert.NotNil(t, stack)
	sql := stack.Query().ToSQL()
	t.Logf("GENERATED SQL: %s", sql)

	assert.True(t, strings.Contains(strings.ToLower(sql), "in (101, 102, 103)") || strings.Contains(strings.ToLower(sql), "in (?,?,?)") || strings.Contains(strings.ToLower(sql), "in (?, ?, ?)"), "SQL should contain where in: "+sql)
	assert.True(t, strings.Contains(strings.ToLower(sql), "or `users`.`dept_id` in") || strings.Contains(strings.ToLower(sql), "or"), "SQL should contain OR for orwherein: "+sql)
}

func TestQueryStackTxPropagation(t *testing.T) {
	dbPath := "./test_query_tx.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for tx test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	mod := &Model{
		ID: "test_tx_model",
		MetaData: MetaData{
			Table: Table{Name: "users"},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id": {Name: "id", Type: "id"},
		},
		ColumnNames: []interface{}{"id"},
	}
	SetModel(mod.ID, mod)

	err = Transaction(func(txCtx context.Context) error {
		param := QueryParam{
			Model:   "test_tx_model",
			Table:   "users",
			Alias:   "users",
			Context: txCtx,
			Select:  []interface{}{"id"},
		}

		stack := param.NewQuery()
		assert.NotNil(t, stack)
		qb := stack.Query()
		assert.NotNil(t, qb)
		assert.NotNil(t, qb.Tx(), "QueryStack 中的 Query 必须继承事务 Tx")
		return nil
	})
	assert.NoError(t, err)
}
