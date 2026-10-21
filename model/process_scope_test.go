package model

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/schema"
)

func TestProcessDataScopeWiring(t *testing.T) {
	dbPath := "./test_process_scope.db"
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	manager, err := capsule.Add("primary", "sqlite3", dbPath)
	if err != nil {
		t.Skip("sqlite3 not available for process scope test:", err)
		return
	}
	manager.SetAsGlobal()
	defer manager.Close()

	sch := capsule.Schema()
	sch.MustDropTableIfExists("test_proc_patients")
	sch.MustCreateTable("test_proc_patients", func(table schema.Blueprint) {
		table.ID("id")
		table.String("name", 50)
		table.Integer("org_id")
		table.Integer("dept_id")
		table.Integer("tenant_id")
		table.Integer("created_by")
	})

	mod := &Model{
		ID:     "test.proc_patient",
		Name:   "test.proc_patient",
		Driver: "sqlite3",
		MetaData: MetaData{
			Table: Table{Name: "test_proc_patients"},
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

	secOrg88Dept101 := &SecurityContext{
		UserID:   1001,
		TenantID: 1,
		OrgID:    88,
		DeptID:   101,
	}
	globalOrg88Dept101 := map[string]interface{}{
		"__sec_ctx": secOrg88Dept101,
		"__sec_bindings": []RoleScopeBinding{
			{RoleID: 1, ScopeType: ScopeTypeDept, DeptIDs: []int64{101}},
		},
	}

	secOrg99Dept101 := &SecurityContext{
		UserID:   2001,
		TenantID: 1,
		OrgID:    99,
		DeptID:   101,
	}
	globalOrg99Dept101 := map[string]interface{}{
		"__sec_ctx": secOrg99Dept101,
		"__sec_bindings": []RoleScopeBinding{
			{RoleID: 2, ScopeType: ScopeTypeDept, DeptIDs: []int64{101}},
		},
	}

	ctx := context.Background()

	// 1. 测试 models.test.proc_patient.Create 通过 Process 创建数据
	createProc, err := process.Of("models.test.proc_patient.Create", map[string]interface{}{
		"name":    "Patient Bob",
		"dept_id": 101,
	})
	assert.NoError(t, err)
	createProc.WithContext(ctx).WithGlobal(globalOrg88Dept101)
	resID := createProc.Run()
	assert.NotNil(t, resID)
	patientID, _ := toInt64(resID)

	// 验证底层数据正确打上了 org_id=88, tenant_id=1
	row, err := capsule.Query().Table("test_proc_patients").Where("id", patientID).First()
	assert.NoError(t, err)
	orgIDInDB, _ := toInt64(row["org_id"])
	assert.Equal(t, int64(88), orgIDInDB)

	// 2. 测试 models.test.proc_patient.Find: 同科室同机构医生可以查到
	findProc, err := process.Of("models.test.proc_patient.Find", patientID, QueryParam{})
	assert.NoError(t, err)
	findProc.WithContext(ctx).WithGlobal(globalOrg88Dept101)
	foundRecord := findProc.Run()
	assert.NotNil(t, foundRecord)
	foundMap, ok := foundRecord.(maps.MapStr)
	assert.True(t, ok)
	assert.Equal(t, "Patient Bob", foundMap["name"])

	// 3. 测试 models.test.proc_patient.Find: 机构 99 的医生跨机构越权查询被阻断（未找到）
	findProcCross, err := process.Of("models.test.proc_patient.Find", patientID, QueryParam{})
	assert.NoError(t, err)
	findProcCross.WithContext(ctx).WithGlobal(globalOrg99Dept101)
	assert.Panics(t, func() {
		findProcCross.Run()
	}, "Cross-org Find should fail to find record and panic with 500/not-found")

	// 4. 测试 models.test.proc_patient.Get: 只能查到当前机构和科室的数据
	getProc, err := process.Of("models.test.proc_patient.Get", QueryParam{})
	assert.NoError(t, err)
	getProc.WithContext(ctx).WithGlobal(globalOrg88Dept101)
	getRecords := getProc.Run()
	rowsSlice, ok := getRecords.([]maps.MapStr)
	assert.True(t, ok)
	assert.Equal(t, 1, len(rowsSlice))

	getProcCross, err := process.Of("models.test.proc_patient.Get", QueryParam{})
	assert.NoError(t, err)
	getProcCross.WithContext(ctx).WithGlobal(globalOrg99Dept101)
	getCrossRecords := getProcCross.Run()
	crossSlice, ok := getCrossRecords.([]maps.MapStr)
	assert.True(t, ok)
	assert.Equal(t, 0, len(crossSlice), "Cross-org Get must return 0 records")

	// 5. 测试 models.test.proc_patient.Update 跨机构越权更新拦截
	updateProcCross, err := process.Of("models.test.proc_patient.Update", patientID, map[string]interface{}{
		"name": "Hacked Bob",
	})
	assert.NoError(t, err)
	updateProcCross.WithContext(ctx).WithGlobal(globalOrg99Dept101)
	assert.Panics(t, func() {
		updateProcCross.Run()
	}, "Cross-org Update must panic with 403 access denied")

	// 6. 测试 models.test.proc_patient.Delete 正常删除
	deleteProc, err := process.Of("models.test.proc_patient.Delete", patientID)
	assert.NoError(t, err)
	deleteProc.WithContext(ctx).WithGlobal(globalOrg88Dept101)
	assert.NotPanics(t, func() {
		deleteProc.Run()
	})

	// 确认数据库中该记录已被删除
	remCount, _ := capsule.Query().Table("test_proc_patients").Where("id", patientID).Count()
	assert.Equal(t, int64(0), remCount)
}

func TestProcessProtectedModelShield(t *testing.T) {
	protMod := &Model{
		ID:        "test.proc_protected",
		Name:      "test.proc_protected",
		Driver:    "sqlite3",
		Protected: true,
		MetaData: MetaData{
			Table:     Table{Name: "test_proc_protected"},
			Protected: true,
		},
		PrimaryKey: "id",
	}
	SetModel(protMod.ID, protMod)

	delProc, err := process.Of("models.test.proc_protected.Delete", 1)
	assert.NoError(t, err)
	delProc.WithContext(context.Background())

	assert.Panics(t, func() {
		delProc.Run()
	}, "Delete protected model via Process must panic with exception")

	defer func() {
		if r := recover(); r != nil {
			if ex, ok := r.(*exception.Exception); ok {
				assert.Equal(t, 403, ex.Code)
			}
		}
	}()
}
