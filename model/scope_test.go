package model

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
)

func TestNewAccessScope(t *testing.T) {
	// Test empty scope
	scope := NewAccessScope()
	assert.NotNil(t, scope)
	assert.Empty(t, scope.CreatedBy)
	assert.Empty(t, scope.UpdatedBy)
	assert.Empty(t, scope.TeamID)
	assert.Empty(t, scope.TenantID)

	// Test with maps.MapStr
	data1 := maps.MapStr{
		"__yao_created_by": "user123",
		"__yao_updated_by": "user456",
		"__yao_team_id":    "team789",
		"__yao_tenant_id":  "tenant000",
	}
	scope1 := NewAccessScope(data1)
	assert.Equal(t, "user123", scope1.CreatedBy)
	assert.Equal(t, "user456", scope1.UpdatedBy)
	assert.Equal(t, "team789", scope1.TeamID)
	assert.Equal(t, "tenant000", scope1.TenantID)

	// Test with map[string]interface{}
	data2 := map[string]interface{}{
		"__yao_created_by": "user111",
		"__yao_team_id":    "team222",
	}
	scope2 := NewAccessScope(data2)
	assert.Equal(t, "user111", scope2.CreatedBy)
	assert.Empty(t, scope2.UpdatedBy)
	assert.Equal(t, "team222", scope2.TeamID)
	assert.Empty(t, scope2.TenantID)
}

func TestAccessScopeWheres(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	// Test with nil wheres
	wheres := scope.Wheres(nil)
	assert.NotNil(t, wheres)
	assert.Equal(t, 3, len(wheres))

	// Test with existing wheres
	existingWheres := []QueryWhere{
		{Column: "status", Value: "active", OP: "eq"},
	}
	wheres = scope.Wheres(existingWheres)
	assert.Equal(t, 4, len(wheres))
	assert.Equal(t, "status", wheres[0].Column)
	assert.Equal(t, "__yao_created_by", wheres[1].Column)
	assert.Equal(t, "user123", wheres[1].Value)
	assert.Equal(t, "__yao_team_id", wheres[2].Column)
	assert.Equal(t, "team456", wheres[2].Value)
	assert.Equal(t, "__yao_tenant_id", wheres[3].Column)
	assert.Equal(t, "tenant789", wheres[3].Value)

	// Test with empty scope
	emptyScope := &AccessScope{}
	wheres = emptyScope.Wheres(nil)
	assert.Equal(t, 0, len(wheres))
}

func TestAccessScopeAppend(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		UpdatedBy: "user456",
		TeamID:    "team789",
		TenantID:  "tenant000",
	}

	// Test with maps.MapStr
	data1 := maps.MapStr{"name": "test"}
	result1 := scope.Append(data1)
	assert.NotNil(t, result1)
	assert.Equal(t, "test", result1["name"])
	assert.Equal(t, "user123", result1["__yao_created_by"])
	assert.Equal(t, "user456", result1["__yao_updated_by"])
	assert.Equal(t, "team789", result1["__yao_team_id"])
	assert.Equal(t, "tenant000", result1["__yao_tenant_id"])

	// Test with map[string]interface{}
	data2 := map[string]interface{}{"name": "test2"}
	result2 := scope.Append(data2)
	assert.NotNil(t, result2)
	assert.Equal(t, "test2", result2["name"])
	assert.Equal(t, "user123", result2["__yao_created_by"])
	assert.Equal(t, "user456", result2["__yao_updated_by"])
	assert.Equal(t, "team789", result2["__yao_team_id"])
	assert.Equal(t, "tenant000", result2["__yao_tenant_id"])

	// Test with nil maps.MapStr
	var nilMapStr maps.MapStr
	result3 := scope.Append(nilMapStr)
	assert.NotNil(t, result3)
	assert.Equal(t, "user123", result3["__yao_created_by"])

	// Test with nil map[string]interface{}
	var nilMap map[string]interface{}
	result4 := scope.Append(nilMap)
	assert.NotNil(t, result4)
	assert.Equal(t, "user123", result4["__yao_created_by"])

	// Test with empty scope
	emptyScope := &AccessScope{}
	data3 := maps.MapStr{"name": "test3"}
	result5 := emptyScope.Append(data3)
	assert.NotNil(t, result5)
	assert.Equal(t, "test3", result5["name"])
	assert.Nil(t, result5["__yao_created_by"])
	assert.Nil(t, result5["__yao_updated_by"])

	// Test with partial scope
	partialScope := &AccessScope{
		TenantID: "tenant123",
	}
	data4 := maps.MapStr{"name": "test4"}
	result6 := partialScope.Append(data4)
	assert.NotNil(t, result6)
	assert.Equal(t, "test4", result6["name"])
	assert.Equal(t, "tenant123", result6["__yao_tenant_id"])
	assert.Nil(t, result6["__yao_created_by"])
}

func TestAccessScopeIntegration(t *testing.T) {
	// Test full workflow
	inputData := maps.MapStr{
		"__yao_created_by": "user123",
		"__yao_team_id":    "team456",
		"__yao_tenant_id":  "tenant789",
	}

	// Create scope from data
	scope := NewAccessScope(inputData)
	assert.Equal(t, "user123", scope.CreatedBy)
	assert.Equal(t, "team456", scope.TeamID)
	assert.Equal(t, "tenant789", scope.TenantID)

	// Convert to query conditions
	wheres := scope.Wheres(nil)
	assert.Equal(t, 3, len(wheres))

	// Append to data for insertion
	insertData := maps.MapStr{"name": "Test Record"}
	insertResult := scope.Append(insertData)
	assert.Equal(t, "Test Record", insertResult["name"])
	assert.Equal(t, "user123", insertResult["__yao_created_by"])
	assert.Equal(t, "team456", insertResult["__yao_team_id"])
	assert.Equal(t, "tenant789", insertResult["__yao_tenant_id"])

	// Update scenario - change updated_by
	scope.UpdatedBy = "user999"
	updateData := maps.MapStr{"name": "Updated Record"}
	updateResult := scope.Append(updateData)
	assert.Equal(t, "Updated Record", updateResult["name"])
	assert.Equal(t, "user999", updateResult["__yao_updated_by"])
}

func TestAccessScopeWithCount(t *testing.T) {
	// Test AccessScope with Count method
	scope := &AccessScope{
		TenantID: "tenant123",
		TeamID:   "team456",
	}

	// Create QueryParam with scope conditions
	param := QueryParam{}
	param.Wheres = scope.Wheres(param.Wheres)

	// Verify wheres are properly set
	assert.Equal(t, 2, len(param.Wheres))
	assert.Equal(t, "__yao_team_id", param.Wheres[0].Column)
	assert.Equal(t, "team456", param.Wheres[0].Value)
	assert.Equal(t, "__yao_tenant_id", param.Wheres[1].Column)
	assert.Equal(t, "tenant123", param.Wheres[1].Value)

	// Test with additional conditions
	scope2 := &AccessScope{
		CreatedBy: "user789",
	}
	param2 := QueryParam{
		Wheres: []QueryWhere{
			{Column: "status", Value: "active", OP: "eq"},
		},
	}
	param2.Wheres = scope2.Wheres(param2.Wheres)
	assert.Equal(t, 2, len(param2.Wheres))
	assert.Equal(t, "status", param2.Wheres[0].Column)
	assert.Equal(t, "__yao_created_by", param2.Wheres[1].Column)
}

func TestWheresTeamOnly(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresTeamOnly(nil)
	assert.Equal(t, 2, len(wheres))
	assert.Equal(t, "__yao_team_id", wheres[0].Column)
	assert.Equal(t, "team456", wheres[0].Value)
	assert.Equal(t, "__yao_tenant_id", wheres[1].Column)
	assert.Equal(t, "tenant789", wheres[1].Value)
}

func TestWheresCreatorOnly(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresCreatorOnly(nil)
	assert.Equal(t, 3, len(wheres))
	assert.Equal(t, "__yao_created_by", wheres[0].Column)
	assert.Equal(t, "user123", wheres[0].Value)
	assert.Equal(t, "__yao_team_id", wheres[1].Column)
	assert.Equal(t, "__yao_tenant_id", wheres[2].Column)
}

func TestWheresEditorOnly(t *testing.T) {
	scope := &AccessScope{
		UpdatedBy: "user456",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresEditorOnly(nil)
	assert.Equal(t, 3, len(wheres))
	assert.Equal(t, "__yao_updated_by", wheres[0].Column)
	assert.Equal(t, "user456", wheres[0].Value)
	assert.Equal(t, "__yao_team_id", wheres[1].Column)
	assert.Equal(t, "__yao_tenant_id", wheres[2].Column)
}

func TestWheresCreatorAndEditor(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		UpdatedBy: "user456",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresCreatorAndEditor(nil)
	assert.Equal(t, 4, len(wheres))
	assert.Equal(t, "__yao_created_by", wheres[0].Column)
	assert.Equal(t, "user123", wheres[0].Value)
	assert.Equal(t, "__yao_updated_by", wheres[1].Column)
	assert.Equal(t, "user456", wheres[1].Value)
}

func TestWheresCreatorOrEditor(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		UpdatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresCreatorOrEditor(nil)
	assert.Equal(t, 3, len(wheres))

	// First should be the OR group
	assert.NotNil(t, wheres[0].Wheres)
	assert.Equal(t, 2, len(wheres[0].Wheres))
	assert.Equal(t, "__yao_created_by", wheres[0].Wheres[0].Column)
	assert.Equal(t, "user123", wheres[0].Wheres[0].Value)
	assert.Equal(t, "__yao_updated_by", wheres[0].Wheres[1].Column)
	assert.Equal(t, "user123", wheres[0].Wheres[1].Value)
	assert.Equal(t, "orwhere", wheres[0].Wheres[1].Method)

	// Then team and tenant
	assert.Equal(t, "__yao_team_id", wheres[1].Column)
	assert.Equal(t, "__yao_tenant_id", wheres[2].Column)
}

func TestWheresCreatorOrEditorSingleField(t *testing.T) {
	// Test with only creator
	scope1 := &AccessScope{
		CreatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres1 := scope1.WheresCreatorOrEditor(nil)
	assert.Equal(t, 3, len(wheres1))
	assert.Equal(t, "__yao_created_by", wheres1[0].Column)
	assert.Equal(t, "user123", wheres1[0].Value)

	// Test with only editor
	scope2 := &AccessScope{
		UpdatedBy: "user456",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres2 := scope2.WheresCreatorOrEditor(nil)
	assert.Equal(t, 3, len(wheres2))
	assert.Equal(t, "__yao_updated_by", wheres2[0].Column)
	assert.Equal(t, "user456", wheres2[0].Value)
}

func TestWheresTeamOrCreator(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresTeamOrCreator(nil)
	assert.Equal(t, 2, len(wheres))

	// First should be the OR group
	assert.NotNil(t, wheres[0].Wheres)
	assert.Equal(t, 2, len(wheres[0].Wheres))
	assert.Equal(t, "__yao_team_id", wheres[0].Wheres[0].Column)
	assert.Equal(t, "team456", wheres[0].Wheres[0].Value)
	assert.Equal(t, "__yao_created_by", wheres[0].Wheres[1].Column)
	assert.Equal(t, "user123", wheres[0].Wheres[1].Value)
	assert.Equal(t, "orwhere", wheres[0].Wheres[1].Method)

	// Then tenant
	assert.Equal(t, "__yao_tenant_id", wheres[1].Column)
	assert.Equal(t, "tenant789", wheres[1].Value)
}

func TestWheresTeamOrEditor(t *testing.T) {
	scope := &AccessScope{
		UpdatedBy: "user456",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresTeamOrEditor(nil)
	assert.Equal(t, 2, len(wheres))

	// First should be the OR group
	assert.NotNil(t, wheres[0].Wheres)
	assert.Equal(t, 2, len(wheres[0].Wheres))
	assert.Equal(t, "__yao_team_id", wheres[0].Wheres[0].Column)
	assert.Equal(t, "team456", wheres[0].Wheres[0].Value)
	assert.Equal(t, "__yao_updated_by", wheres[0].Wheres[1].Column)
	assert.Equal(t, "user456", wheres[0].Wheres[1].Value)
	assert.Equal(t, "orwhere", wheres[0].Wheres[1].Method)

	// Then tenant
	assert.Equal(t, "__yao_tenant_id", wheres[1].Column)
}

func TestWheresCreatorOrTeamOrEditor(t *testing.T) {
	scope := &AccessScope{
		CreatedBy: "user123",
		UpdatedBy: "user456",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres := scope.WheresCreatorOrTeamOrEditor(nil)
	assert.Equal(t, 2, len(wheres))

	// First should be the OR group with all three conditions
	assert.NotNil(t, wheres[0].Wheres)
	assert.Equal(t, 3, len(wheres[0].Wheres))
	assert.Equal(t, "__yao_created_by", wheres[0].Wheres[0].Column)
	assert.Equal(t, "user123", wheres[0].Wheres[0].Value)
	assert.Equal(t, "__yao_updated_by", wheres[0].Wheres[1].Column)
	assert.Equal(t, "user456", wheres[0].Wheres[1].Value)
	assert.Equal(t, "orwhere", wheres[0].Wheres[1].Method)
	assert.Equal(t, "__yao_team_id", wheres[0].Wheres[2].Column)
	assert.Equal(t, "team456", wheres[0].Wheres[2].Value)
	assert.Equal(t, "orwhere", wheres[0].Wheres[2].Method)

	// Then tenant
	assert.Equal(t, "__yao_tenant_id", wheres[1].Column)
	assert.Equal(t, "tenant789", wheres[1].Value)
}

func TestWheresCreatorOrTeamOrEditorPartial(t *testing.T) {
	// Test with only creator
	scope1 := &AccessScope{
		CreatedBy: "user123",
		TenantID:  "tenant789",
	}

	wheres1 := scope1.WheresCreatorOrTeamOrEditor(nil)
	assert.Equal(t, 2, len(wheres1))
	assert.NotNil(t, wheres1[0].Wheres)
	assert.Equal(t, 1, len(wheres1[0].Wheres))
	assert.Equal(t, "__yao_created_by", wheres1[0].Wheres[0].Column)

	// Test with only team
	scope2 := &AccessScope{
		TeamID:   "team456",
		TenantID: "tenant789",
	}

	wheres2 := scope2.WheresCreatorOrTeamOrEditor(nil)
	assert.Equal(t, 2, len(wheres2))
	assert.NotNil(t, wheres2[0].Wheres)
	assert.Equal(t, 1, len(wheres2[0].Wheres))
	assert.Equal(t, "__yao_team_id", wheres2[0].Wheres[0].Column)

	// Test with creator and team
	scope3 := &AccessScope{
		CreatedBy: "user123",
		TeamID:    "team456",
		TenantID:  "tenant789",
	}

	wheres3 := scope3.WheresCreatorOrTeamOrEditor(nil)
	assert.Equal(t, 2, len(wheres3))
	assert.NotNil(t, wheres3[0].Wheres)
	assert.Equal(t, 2, len(wheres3[0].Wheres))
}

func TestModelDataScopeDomainTypes(t *testing.T) {
	sourceDSL := `{
		"name": "User Account",
		"table": {"name": "sys_users"},
		"protected": true,
		"data_scope": {
			"enabled": true,
			"mode": "shadow"
		},
		"columns": [
			{"name": "id", "type": "id"},
			{"name": "name", "type": "string"}
		],
		"relations": {
			"profiles": {
				"type": "hasMany",
				"model": "sys.user_profile",
				"key": "id",
				"foreign": "user_id",
				"cascade": true
			}
		}
	}`

	mod, err := LoadSource([]byte(sourceDSL), "sys.test_dsl_user", "")
	assert.NoError(t, err)
	assert.NotNil(t, mod)

	// 1. 验证 Protected
	assert.True(t, mod.Protected, "mod.Protected should be true")
	assert.True(t, mod.MetaData.Protected, "mod.MetaData.Protected should be true")

	// 2. 验证 DataScope
	assert.True(t, mod.DataScope.Enabled)
	assert.Equal(t, "shadow", mod.DataScope.Mode)

	// 3. 验证 GetDataScopeSpec 自动补齐默认字段
	spec := mod.GetDataScopeSpec()
	assert.True(t, spec.Enabled)
	assert.Equal(t, "tenant_id", spec.TenantField)
	assert.Equal(t, "org_id", spec.OrgField)
	assert.Equal(t, "dept_id", spec.DeptField)
	assert.Equal(t, "created_by", spec.UserField)

	// 4. 验证 Relation.Cascade
	assert.True(t, mod.MetaData.Relations["profiles"].Cascade, "profiles relation should have cascade=true")

	// 5. 验证哨兵错误体系
	assert.NotNil(t, ErrMissingSecurityCtx)
	assert.NotNil(t, ErrDataScopeAccessDenied)
	assert.NotNil(t, ErrModelProtected)
	assert.NotNil(t, ErrCascadeDepthExceeded)
}

func TestReduceRoleScopes(t *testing.T) {
	// 1. 偏序格吞噬：全院权限吞噬个人和科室权限
	sec1 := &SecurityContext{
		UserID: 1001,
		OrgID:  10,
		DeptID: 101,
	}
	bindings1 := []RoleScopeBinding{
		{RoleID: 1, ScopeType: ScopeTypeSelf},
		{RoleID: 2, ScopeType: ScopeTypeDept, DeptIDs: []int64{101}},
		{RoleID: 3, ScopeType: ScopeTypeOrg, OrgIDs: []int64{10}},
	}
	plan1 := ReduceRoleScopes(sec1, bindings1)
	assert.True(t, plan1.EnforceOrg, "Org scope must be enforced")
	assert.False(t, plan1.IncludeSelf, "Org scope should subsume self scope")
	assert.Empty(t, plan1.AllowedDeptIDs, "Org scope should subsume dept scope")

	// 2. 超管/内部调用短路全量放行
	sec2 := &SecurityContext{InternalCall: true}
	plan2 := ReduceRoleScopes(sec2, nil)
	assert.True(t, plan2.IsUnrestricted)

	// 3. 空权限故障闭锁 (Fail-Closed)
	sec3 := &SecurityContext{UserID: 2001}
	plan3 := ReduceRoleScopes(sec3, nil)
	assert.True(t, plan3.IsDenied)

	// 4. 科室与多级科室合并与排序
	sec4 := &SecurityContext{
		UserID:     3001,
		DeptID:     101,
		SubDeptIDs: []int64{102, 103},
	}
	bindings4 := []RoleScopeBinding{
		{RoleID: 4, ScopeType: ScopeTypeDeptSub},
		{RoleID: 5, ScopeType: ScopeTypeCustom, DeptIDs: []int64{105, 99}},
	}
	plan4 := ReduceRoleScopes(sec4, bindings4)
	assert.False(t, plan4.EnforceOrg)
	assert.Equal(t, []int64{99, 101, 102, 103, 105}, plan4.AllowedDeptIDs)
	assert.Equal(t, []int64{101}, plan4.RootAncestorDeptIDs)
	assert.True(t, plan4.HasCustomDiscreteDepts)
}

func TestASTDualParenthesesIsolation(t *testing.T) {
	dbPath := "./test_scope_ast.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for ast test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	mod := &Model{
		ID: "test_ast_patient",
		MetaData: MetaData{
			Table: Table{Name: "patients"},
		},
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":         {Name: "id", Type: "id"},
			"name":       {Name: "name", Type: "string"},
			"mobile":     {Name: "mobile", Type: "string"},
			"org_id":     {Name: "org_id", Type: "integer"},
			"dept_id":    {Name: "dept_id", Type: "integer"},
			"created_by": {Name: "created_by", Type: "integer"},
		},
		ColumnNames: []interface{}{"id", "name", "mobile", "org_id", "dept_id", "created_by"},
	}
	SetModel(mod.ID, mod)

	// 上游传入包含高危 OR 的查询: (name = '张' OR mobile = '138')
	param := QueryParam{
		Model: "test_ast_patient",
		Table: "patients",
		Alias: "patients",
		Select: []interface{}{"id", "name"},
		Wheres: []QueryWhere{
			{Method: "where", Column: "name", OP: "eq", Value: "张"},
			{Method: "orwhere", Column: "mobile", OP: "eq", Value: "138"},
		},
	}

	sec := &SecurityContext{
		UserID:   1001,
		OrgID:    88,
		TenantID: 1,
	}
	plan := ReducedScopePlan{
		EnforceOrg: true,
	}
	spec := ModelDataScopeSpec{
		Enabled:     true,
		Mode:        "enforcing",
		OrgField:    "org_id",
		DeptField:   "dept_id",
		UserField:   "created_by",
		TenantField: "tenant_id",
	}

	err = BuildAndInjectQueryWhere(mod, &param, sec, plan, spec, nil)
	assert.NoError(t, err)

	// 验证 AST 隔离：顶层 param.Wheres 必须分为 2 组
	assert.Equal(t, 2, len(param.Wheres))
	// 第 1 组包含原始业务条件
	assert.Equal(t, 2, len(param.Wheres[0].Wheres))
	// 第 2 组包含注入的数据权限条件
	assert.Equal(t, 1, len(param.Wheres[1].Wheres))

	// 生成实际 SQL 检验
	stack := param.NewQuery()
	assert.NotNil(t, stack)
	sql := stack.Query().ToSQL()
	t.Logf("Isolated SQL: %s", sql)

	// 断言：原始 OR 条件被包裹，且与 org_id 条件被 AND 隔离连接
	assert.True(t, strings.Contains(strings.ToLower(sql), "where") && strings.Contains(strings.ToLower(sql), "and"))
}

func TestScopeValidator(t *testing.T) {
	mod := &Model{
		ID: "test_val_patient",
		Columns: map[string]*Column{
			"id":      {Name: "id"},
			"dept_id": {Name: "dept_id"},
		},
	}

	// 1. 合法 JSON 校验通过
	validJSON := `{"logic": "AND", "rules": [{"field": "dept_id", "operator": "eq", "slot": "__dept_id"}]}`
	schema, err := CompileAndValidateCondition(mod, validJSON)
	assert.NoError(t, err)
	assert.NotNil(t, schema)
	assert.Equal(t, 1, len(schema.Rules))

	// 2. 字段名包含非法字符（SQL注入）拦截
	injectionJSON := `{"logic": "AND", "rules": [{"field": "dept_id; DROP TABLE patients--", "operator": "eq"}]}`
	_, err = CompileAndValidateCondition(mod, injectionJSON)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "security violation")

	// 3. 模型中不存在的字段拦截
	unknownFieldJSON := `{"logic": "AND", "rules": [{"field": "secret_field", "operator": "eq"}]}`
	_, err = CompileAndValidateCondition(mod, unknownFieldJSON)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in model")

	// 4. 非法 context slot 拦截
	unknownSlotJSON := `{"logic": "AND", "rules": [{"field": "dept_id", "operator": "eq", "slot": "__evil_slot"}]}`
	_, err = CompileAndValidateCondition(mod, unknownSlotJSON)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown context slot")
}

func TestSecureMutation(t *testing.T) {
	dbPath := "./test_mutation.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for mutation test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	sch := capsule.Schema()
	sch.MustDropTableIfExists("test_mutation_patients")
	sch.MustCreateTable("test_mutation_patients", func(table schema.Blueprint) {
		table.ID("id")
		table.String("name", 50)
		table.Integer("org_id")
		table.Integer("dept_id")
		table.Integer("tenant_id")
		table.Integer("created_by")
	})

	mod := &Model{
		ID:     "test.mutation_patient",
		Name:   "test.mutation_patient",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_mutation_patients"},
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
	SetModel(mod.ID, mod)

	// 1. 测试 SecureCreate: 自动写入当前机构与租户
	secOrg88 := &SecurityContext{
		UserID:   1001,
		TenantID: 1,
		OrgID:    88,
		DeptID:   101,
	}
	globalOrg88 := map[string]interface{}{
		"__sec_ctx": secOrg88,
		"__sec_bindings": []RoleScopeBinding{
			{RoleID: 1, ScopeType: ScopeTypeDept, DeptIDs: []int64{101}},
		},
	}

	createPayload := maps.MapStrAny{
		"name":    "Alice",
		"dept_id": 101,
	}
	id, err := mod.SecureCreate(context.Background(), globalOrg88, createPayload)
	assert.NoError(t, err)
	assert.NotNil(t, id)
	patientID, _ := toInt64(id)

	// 验证库中真实数据打上了 org_id=88, tenant_id=1
	row, err := capsule.Query().Table("test_mutation_patients").Where("id", patientID).First()
	assert.NoError(t, err)
	orgIDInDB, _ := toInt64(row["org_id"])
	tenantIDInDB, _ := toInt64(row["tenant_id"])
	assert.Equal(t, int64(88), orgIDInDB)
	assert.Equal(t, int64(1), tenantIDInDB)

	// 2. 测试 SecureUpdateByID 防篡改：试图篡改 org_id=999
	updateTamper := maps.MapStrAny{
		"name":   "Alice Modified",
		"org_id": 999,
	}
	err = mod.SecureUpdateByID(context.Background(), globalOrg88, patientID, updateTamper)
	assert.NoError(t, err)

	rowAfter, err := capsule.Query().Table("test_mutation_patients").Where("id", patientID).First()
	assert.NoError(t, err)
	orgIDAfter, _ := toInt64(rowAfter["org_id"])
	assert.Equal(t, "Alice Modified", rowAfter["name"])
	assert.Equal(t, int64(88), orgIDAfter, "org_id must not be tampered with!")

	// 3. 测试 SecureUpdateByID 越权科室拦截：试图将 dept_id 改为未授权的 202
	unauthDeptPayload := maps.MapStrAny{
		"dept_id": 202,
	}
	err = mod.SecureUpdateByID(context.Background(), globalOrg88, patientID, unauthDeptPayload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target department [202] not authorized")

	// 4. 测试 SecureUpdateByID 跨机构 IDOR 越权拦截：机构 99 的医生试图更新患者 1
	secOrg99 := &SecurityContext{
		UserID:   2001,
		TenantID: 1,
		OrgID:    99,
		DeptID:   101,
	}
	globalOrg99 := map[string]interface{}{
		"__sec_ctx": secOrg99,
		"__sec_bindings": []RoleScopeBinding{
			{RoleID: 2, ScopeType: ScopeTypeDept, DeptIDs: []int64{101}},
		},
	}
	crossOrgPayload := maps.MapStrAny{"name": "Hacked Alice"}
	err = mod.SecureUpdateByID(context.Background(), globalOrg99, patientID, crossOrgPayload)
	assert.ErrorIs(t, err, ErrDataScopeAccessDenied)

	// 5. 测试变动幂等性：提交完全相同的字段，不误报 403
	idempotentPayload := maps.MapStrAny{"name": "Alice Modified"}
	err = mod.SecureUpdateByID(context.Background(), globalOrg88, patientID, idempotentPayload)
	assert.NoError(t, err, "No actual changes must still succeed idempotently")

	// 6. 测试 SecureSave 路由
	saveUpdatePayload := maps.MapStrAny{
		"id":   patientID,
		"name": "Alice Saved",
	}
	savedID, err := mod.SecureSave(context.Background(), globalOrg88, saveUpdatePayload)
	assert.NoError(t, err)
	savedIDInt, _ := toInt64(savedID)
	assert.Equal(t, patientID, savedIDInt)
}
