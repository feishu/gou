package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataScopeQueryParamBypass(t *testing.T) {
	mod := &Model{
		ID:         "test_order",
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":         {Name: "id"},
			"tenant_id":  {Name: "tenant_id"},
			"org_id":     {Name: "org_id"},
			"dept_id":    {Name: "dept_id"},
			"created_by": {Name: "created_by"},
		},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
			UserField:   "created_by",
		},
	}

	sec := &SecurityContext{
		UserID:   1001,
		TenantID: 1,
		OrgID:    2,
		DeptID:   3,
	}
	global := map[string]interface{}{
		"__sec_ctx": sec,
		"__sec_bindings": []RoleScopeBinding{
			{ScopeType: ScopeTypeDept, DeptIDs: []int64{3}},
		},
	}

	// 1. 常规查询：应注入权限条件
	normalParam := &QueryParam{
		Model: "test_order",
	}
	err := mod.BeforeQueryDataScope(context.Background(), global, normalParam)
	assert.NoError(t, err)
	assert.NotEmpty(t, normalParam.Wheres, "normal query should have injected data scope wheres")

	// 2. 旁路测试 1: DataScope = pointer to false
	falseVal := false
	bypassParam1 := &QueryParam{
		Model:     "test_order",
		DataScope: &falseVal,
	}
	err = mod.BeforeQueryDataScope(context.Background(), global, bypassParam1)
	assert.NoError(t, err)
	assert.Empty(t, bypassParam1.Wheres, "DataScope=&false should bypass data scope injection")

	// 3. 旁路测试 2: DisableDataScope = true
	bypassParam2 := &QueryParam{
		Model:            "test_order",
		DisableDataScope: true,
	}
	err = mod.BeforeQueryDataScope(context.Background(), global, bypassParam2)
	assert.NoError(t, err)
	assert.Empty(t, bypassParam2.Wheres, "DisableDataScope=true should bypass data scope injection")

	// 4. 旁路测试 3: BypassDataScope = true
	bypassParam3 := &QueryParam{
		Model:           "test_order",
		BypassDataScope: true,
	}
	err = mod.BeforeQueryDataScope(context.Background(), global, bypassParam3)
	assert.NoError(t, err)
	assert.Empty(t, bypassParam3.Wheres, "BypassDataScope=true should bypass data scope injection")

	// 5. 旁路测试 4: global["data_scope"] = false
	globalBypass := map[string]interface{}{
		"__sec_ctx":  sec,
		"data_scope": false,
	}
	bypassParam4 := &QueryParam{
		Model: "test_order",
	}
	err = mod.BeforeQueryDataScope(context.Background(), globalBypass, bypassParam4)
	assert.NoError(t, err)
	assert.Empty(t, bypassParam4.Wheres, "global['data_scope']=false should bypass data scope injection")
}

func TestSlaveTableGracefulDowngrade(t *testing.T) {
	// 处方明细表（从表）：只有 tenant_id, org_id, item_id, prescription_id，没有任何 dept_id 或 doctor_id
	slaveMod := &Model{
		ID:         "prescription_item",
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":              {Name: "id"},
			"tenant_id":       {Name: "tenant_id"},
			"org_id":          {Name: "org_id"},
			"prescription_id": {Name: "prescription_id"},
			"item_id":         {Name: "item_id"},
		},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			TableRole:   TableRoleSlave, // 显式标记为从表
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
			UserField:   "created_by",
		},
	}

	sec := &SecurityContext{
		UserID:   2001,
		DoctorID: 3001,
		TenantID: 10,
		OrgID:    20,
		DeptID:   30,
	}

	// 医生角色只有个人/科室权限 (ScopeTypeDept)
	global := map[string]interface{}{
		"__sec_ctx": sec,
		"__sec_bindings": []RoleScopeBinding{
			{ScopeType: ScopeTypeDept, DeptIDs: []int64{30}},
		},
	}

	param := &QueryParam{
		Model: "prescription_item",
	}

	// 执行注入：必须不报错，且必须成功注入 tenant_id 与 org_id 顶层边界
	err := slaveMod.BeforeQueryDataScope(context.Background(), global, param)
	assert.NoError(t, err, "slave table query must NOT return error even without dept/doctor columns")
	assert.NotEmpty(t, param.Wheres, "slave table must inject tenant/org boundaries")

	// 验证注入的顶层边界
	topGroup := param.Wheres[0]
	assert.Equal(t, "where", topGroup.Method)
	assert.True(t, len(topGroup.Wheres) >= 1)

	var hasTenant, hasOrg, hasDept, hasUser bool
	for _, w := range topGroup.Wheres {
		if w.Column == "tenant_id" && w.Value == int64(10) {
			hasTenant = true
		}
		if w.Column == "org_id" && w.Value == int64(20) {
			hasOrg = true
		}
		if w.Column == "dept_id" {
			hasDept = true
		}
		if w.Column == "created_by" {
			hasUser = true
		}
	}

	assert.True(t, hasTenant, "slave table query must enforce tenant_id boundary")
	assert.True(t, hasOrg, "slave table query must enforce org_id boundary")
	assert.False(t, hasDept, "slave table must NOT inject dept_id")
	assert.False(t, hasUser, "slave table must NOT inject user_id/doctor_id")

	// 对比测试：如果未标记为 TableRoleSlave（主表角色），缺少部门字段必须触发 Fail-Closed 安全拦截
	masterModWithoutDept := &Model{
		ID:         "faulty_master_entity",
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":        {Name: "id"},
			"tenant_id": {Name: "tenant_id"},
			"org_id":    {Name: "org_id"},
		},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			TableRole:   TableRoleMaster, // 主表
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
			UserField:   "created_by",
		},
	}

	faultyParam := &QueryParam{
		Model: "faulty_master_entity",
	}
	err = masterModWithoutDept.BeforeQueryDataScope(context.Background(), global, faultyParam)
	assert.Error(t, err, "master model without mapped dept/user columns must fail closed")
	assert.Contains(t, err.Error(), "security block: model [faulty_master_entity] enabled data_scope but lacks mapped dept/user columns")
}

