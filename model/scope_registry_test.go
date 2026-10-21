package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
)

// TestDynamicScopeRegistryPriority 验证数据权限规格获取的三级回退梯度
func TestDynamicScopeRegistryPriority(t *testing.T) {
	ClearDataScopeRegistry()
	defer ClearDataScopeRegistry()

	// 准备基准模型：包含静态 DSL 配置
	mod := &Model{
		ID: "test.order",
		MetaData: MetaData{
			Table: Table{Name: "test_orders"},
			DataScope: ModelDataScopeSpec{
				Enabled:     true,
				Mode:        "enforcing",
				TenantField: "tid",
				OrgField:    "oid",
				DeptField:   "did",
				UserField:   "uid",
			},
		},
		Columns: map[string]*Column{
			"id":   {Name: "id"},
			"oid":  {Name: "oid"},
			"did":  {Name: "did"},
			"name": {Name: "name"},
		},
	}

	// 1. 无数据库动态配置时：命中 Level 2 (mod.yao DSL 静态声明)
	specLevel2 := mod.GetDataScopeSpec()
	assert.True(t, specLevel2.Enabled)
	assert.Equal(t, "oid", specLevel2.OrgField)
	assert.Equal(t, "did", specLevel2.DeptField)
	assert.Equal(t, "tid", specLevel2.TenantField)

	// 2. 向数据库注册表动态写入最新规则 (模拟后台运营将字段调整为自定义字段)
	RegisterDataScopeSpec("test_orders", ModelDataScopeSpec{
		Enabled:     true,
		Mode:        "shadow",
		TenantField: "db_tenant_id",
		OrgField:    "db_org_id",
		DeptField:   "db_dept_id",
		UserField:   "db_created_by",
	})

	// 3. 再次获取：必须优先命中 Level 1 (数据库动态注册表，覆盖静态配置)
	specLevel1 := mod.GetDataScopeSpec()
	assert.True(t, specLevel1.Enabled)
	assert.Equal(t, "shadow", specLevel1.Mode)
	assert.Equal(t, "db_org_id", specLevel1.OrgField)
	assert.Equal(t, "db_dept_id", specLevel1.DeptField)
	assert.Equal(t, "db_tenant_id", specLevel1.TenantField)

	// 4. 清空注册表后：必须平滑回落至 Level 2
	ClearDataScopeRegistry()
	specFallback := mod.GetDataScopeSpec()
	assert.True(t, specFallback.Enabled)
	assert.Equal(t, "enforcing", specFallback.Mode)
	assert.Equal(t, "oid", specFallback.OrgField)

	// 5. 验证 Level 3: 当静态 DSL 未启用，但字段符合 (org_id/oid + dept_id/department_id) 约定
	modNoDSL := &Model{
		ID: "test.clinic",
		MetaData: MetaData{
			Table: Table{Name: "test_clinic"},
		},
		Columns: map[string]*Column{
			"id":      {Name: "id"},
			"org_id":  {Name: "org_id"},
			"dept_id": {Name: "dept_id"},
		},
	}
	specLevel3 := modNoDSL.GetDataScopeSpec()
	assert.True(t, specLevel3.Enabled, "应该自动启发式命中 Level 3 约定")
	assert.Equal(t, "org_id", specLevel3.OrgField)
	assert.Equal(t, "dept_id", specLevel3.DeptField)

	// 6. 验证纯无关联表（例如仅包含 id 和 name 的字典表）：不应误开启
	modDict := &Model{
		ID: "test.dict",
		MetaData: MetaData{
			Table: Table{Name: "test_dict"},
		},
		Columns: map[string]*Column{
			"id":   {Name: "id"},
			"name": {Name: "name"},
		},
	}
	specDict := modDict.GetDataScopeSpec()
	assert.False(t, specDict.Enabled, "无科室与机构字段的表绝不应误判启用")
}

// TestParseScopeSpecFromJSON 验证 condition_value 解析与防御容错
func TestParseScopeSpecFromJSON(t *testing.T) {
	// 1. 空输入容错
	specNil := parseScopeSpecFromJSON(nil)
	assert.True(t, specNil.Enabled)
	assert.Equal(t, "org_id", specNil.OrgField)

	// 2. 正常合法 JSON 字符串
	jsonStr := `{"enabled": true, "mode": "shadow", "org_field": "hospital_id", "dept_field": "section_id"}`
	specNormal := parseScopeSpecFromJSON(jsonStr)
	assert.True(t, specNormal.Enabled)
	assert.Equal(t, "shadow", specNormal.Mode)
	assert.Equal(t, "hospital_id", specNormal.OrgField)
	assert.Equal(t, "section_id", specNormal.DeptField)
	assert.Equal(t, "tenant_id", specNormal.TenantField, "未指定的字段应自动补齐默认值")

	// 3. 显式禁用
	jsonDisabled := `{"enabled": false}`
	specDisabled := parseScopeSpecFromJSON(jsonDisabled)
	assert.False(t, specDisabled.Enabled)

	// 4. 脏数据/破损 JSON：不崩溃，回退默认
	specBroken := parseScopeSpecFromJSON(`{broken_json}`)
	assert.True(t, specBroken.Enabled)
	assert.Equal(t, "org_id", specBroken.OrgField)

	// 5. 传入 map[string]interface{}
	rawMap := map[string]interface{}{
		"org_field":  "my_org",
		"dept_field": "my_dept",
	}
	specMap := parseScopeSpecFromJSON(rawMap)
	assert.Equal(t, "my_org", specMap.OrgField)
	assert.Equal(t, "my_dept", specMap.DeptField)
}

// TestProcessReloadDataScopesExecution 验证全局 Process 挂载与热刷新触发
func TestProcessReloadDataScopesExecution(t *testing.T) {
	// 调用 models.reloaddatascopes 流程
	pModels := process.New("models.reloaddatascopes")
	resModels, err := pModels.Exec()
	assert.NoError(t, err)
	assert.Equal(t, true, resModels)

	// 调用 model.reloaddatascopes 流程
	pModel := process.New("model.reloaddatascopes")
	resModel, err := pModel.Exec()
	assert.NoError(t, err)
	assert.Equal(t, true, resModel)
}

// TestReloadDataScopesWithRealCapsuleTable 验证真实数据库表拉取与原子热重载
func TestReloadDataScopesWithRealCapsuleTable(t *testing.T) {
	dbPath := filepath.Join(os.TempDir(), "test_scope_registry.db")
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("scope_reg_primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for registry test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	sch := capsule.Schema()
	sch.MustDropTableIfExists("sys_datascopes_access")

	sch.MustCreateTable("sys_datascopes_access", func(table schema.Blueprint) {
		table.ID("id")
		table.String("uuid", 36)
		table.String("name", 255).Null()
		table.Integer("scope_type").Null()
		table.String("table", 100)
		table.JSON("condition_value")
		table.String("remark", 500).Null()
		table.Timestamps()
		table.SoftDeletes()
	})
	defer sch.MustDropTableIfExists("sys_datascopes_access")

	// 插入一条测试数据
	err = capsule.Query().Table("sys_datascopes_access").Insert(map[string]interface{}{
		"uuid":            "test-uuid-101",
		"name":            "门诊病历科室隔离测试",
		"scope_type":      2,
		"table":           "syd_clinic_test_records",
		"condition_value": `{"enabled": true, "mode": "enforcing", "org_field": "oid", "dept_field": "dept_id"}`,
	})
	assert.NoError(t, err)

	// 执行热重载
	err = ReloadDataScopes()
	assert.NoError(t, err)

	// 验证已自动装载入 DataScopeRegistry
	val, ok := DataScopeRegistry.Load("syd_clinic_test_records")
	assert.True(t, ok, "应该能在动态注册表中读到 syd_clinic_test_records")
	spec := val.(ModelDataScopeSpec)
	assert.True(t, spec.Enabled)
	assert.Equal(t, "oid", spec.OrgField)
	assert.Equal(t, "dept_id", spec.DeptField)
}
