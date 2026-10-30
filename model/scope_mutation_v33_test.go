package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/kun/maps"
)

func TestSecureMutationReadOnlyBlocked(t *testing.T) {
	mod := &Model{
		ID:         "medical_record",
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":         {Name: "id"},
			"tenant_id":  {Name: "tenant_id"},
			"org_id":     {Name: "org_id"},
			"dept_id":    {Name: "dept_id"},
			"doctor_id":  {Name: "doctor_id"},
			"created_by": {Name: "created_by"},
		},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			TableRole:   TableRoleMaster,
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
			UserField:   "created_by",
		},
	}

	sec := &SecurityContext{
		UserID:   5001,
		DoctorID: 6001,
		TenantID: 1,
		OrgID:    2,
		DeptID:   3,
	}

	// 质控员/审计员/进修生：读范围为科室级，但写策略为纯只读 (MutationReadOnly)
	global := map[string]interface{}{
		"__sec_ctx": sec,
		"role_scope_tuples": []RoleScopeTuple{
			{
				RoleID:         88,
				AnchorAxis:     AxisClinicalDept,
				ReadScope:      ScopeTypeUnit,
				MutationPolicy: MutationReadOnly, // 纯只读
			},
		},
	}

	// 1. 测试 SecureCreate: 必须直接拦截并返回 ErrDataScopeReadOnlyBlocked
	createRow := maps.MapStrAny{
		"patient_name": "张三",
	}
	_, err := mod.SecureCreate(context.Background(), global, createRow)
	assert.Error(t, err)
	assert.Equal(t, ErrDataScopeReadOnlyBlocked, err, "read-only role calling SecureCreate must return ErrDataScopeReadOnlyBlocked")

	// 2. 测试 SecureUpdateByID: 必须直接拦截并返回 ErrDataScopeReadOnlyBlocked
	updatePayload := maps.MapStrAny{
		"patient_name": "张三(已修改)",
	}
	err = mod.SecureUpdateByID(context.Background(), global, 101, updatePayload)
	assert.Error(t, err)
	assert.Equal(t, ErrDataScopeReadOnlyBlocked, err, "read-only role calling SecureUpdateByID must return ErrDataScopeReadOnlyBlocked")

	// 3. 测试 SecureDeleteByID: 必须直接拦截并返回 ErrDataScopeReadOnlyBlocked
	err = mod.SecureDeleteByID(context.Background(), global, 101)
	assert.Error(t, err)
	assert.Equal(t, ErrDataScopeReadOnlyBlocked, err, "read-only role calling SecureDeleteByID must return ErrDataScopeReadOnlyBlocked")

	// 4. 测试 SecureDeleteWhere: 必须直接拦截并返回 ErrDataScopeReadOnlyBlocked
	_, err = mod.SecureDeleteWhere(context.Background(), global, QueryParam{Model: mod.ID})
	assert.Error(t, err)
	assert.Equal(t, ErrDataScopeReadOnlyBlocked, err, "read-only role calling SecureDeleteWhere must return ErrDataScopeReadOnlyBlocked")
}

func TestBeforeInsertGuardDoctorIDTampering(t *testing.T) {
	mod := &Model{
		ID:         "prescription",
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":         {Name: "id"},
			"tenant_id":  {Name: "tenant_id"},
			"org_id":     {Name: "org_id"},
			"dept_id":    {Name: "dept_id"},
			"doctor_id":  {Name: "doctor_id"},
			"created_by": {Name: "created_by"},
			"campus_id":  {Name: "campus_id"},
		},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			TableRole:   TableRoleMaster,
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
			UserField:   "created_by",
		},
	}

	sec := &SecurityContext{
		UserID:         8001,
		DoctorID:       9001, // 真实医生 ID 为 9001
		TenantID:       10,
		OrgID:          20,
		ActiveDeptID:   30,
		ActiveCampusID: 40,
	}

	// 医生角色：本人写权限 (MutationStrictSelf)
	global := map[string]interface{}{
		"__sec_ctx": sec,
		"role_scope_tuples": []RoleScopeTuple{
			{
				RoleID:         1,
				AnchorAxis:     AxisClinicalDept,
				ReadScope:      ScopeTypeSelf,
				MutationPolicy: MutationStrictSelf,
			},
		},
	}

	// 恶意前端伪造参数：传入虚假的 doctor_id: 99999, dept_id: 88888, tenant_id: 77777, org_id: 66666
	fraudulentPayload := maps.MapStrAny{
		"doctor_id": int64(99999),
		"dept_id":   int64(88888),
		"tenant_id": int64(77777),
		"org_id":    int64(66666),
		"drug_name": "阿莫西林胶囊",
	}

	secPlan, writePlan, spec, err := mod.resolveMutationPlan(context.Background(), global)
	assert.NoError(t, err)
	assert.Equal(t, ScopeTypeSelf, writePlan.EffectiveWriteScope)

	err = mod.sanitizeMutationPayload(secPlan, writePlan, spec, fraudulentPayload, true)
	assert.NoError(t, err)

	// 验证 Before-Insert Guard 防御效果：
	// 1. 机构与租户被强制覆盖为当前会话真实值
	assert.Equal(t, int64(10), fraudulentPayload["tenant_id"], "tenant_id must be forcefully overwritten by session")
	assert.Equal(t, int64(20), fraudulentPayload["org_id"], "org_id must be forcefully overwritten by session")
	// 2. 医生 ID 被强制覆盖为本人真实 DoctorID (9001)，阻止冒用工号
	assert.Equal(t, int64(9001), fraudulentPayload["doctor_id"], "doctor_id must be overwritten by session DoctorID")
	// 3. 科室被强制同步为当前活跃执业科室 (30)
	assert.Equal(t, int64(30), fraudulentPayload["dept_id"], "dept_id must be overwritten by session ActiveDeptID")
	// 4. 执业院区自动填充
	assert.Equal(t, int64(40), fraudulentPayload["campus_id"], "campus_id must be automatically populated")
}

func TestInjectMutationWhereStrictSelfIDOR(t *testing.T) {
	mod := &Model{
		ID:         "outpatient_emr",
		PrimaryKey: "id",
		Columns: map[string]*Column{
			"id":        {Name: "id"},
			"tenant_id": {Name: "tenant_id"},
			"org_id":    {Name: "org_id"},
			"dept_id":   {Name: "dept_id"},
			"doctor_id": {Name: "doctor_id"},
		},
		DataScope: ModelDataScopeSpec{
			Enabled:     true,
			TableRole:   TableRoleMaster,
			TenantField: "tenant_id",
			OrgField:    "org_id",
			DeptField:   "dept_id",
		},
	}

	sec := &SecurityContext{
		UserID:   8001,
		DoctorID: 9001, // 医生 A 的工号为 9001
		TenantID: 10,
		OrgID:    20,
	}

	// 医生 A 为本人写 (MutationStrictSelf)
	writePlan := ReducedMutationPlan{
		EffectiveWriteScope: ScopeTypeSelf,
		StrictSelfDoctorID:  9001,
	}

	param := &QueryParam{
		Model: "outpatient_emr",
		Wheres: []QueryWhere{
			{Method: "where", Column: "id", Value: 12345}, // 目标病历 ID
		},
	}

	err := mod.injectMutationWhere(param, sec, writePlan, mod.DataScope)
	assert.NoError(t, err)

	// 检查注入的写条件：必须强制并入 doctor_id = 9001
	assert.Equal(t, 2, len(param.Wheres))
	mutationGroup := param.Wheres[1]
	assert.Equal(t, "where", mutationGroup.Method)

	var hasTenant, hasOrg, hasDoctorID bool
	for _, w := range mutationGroup.Wheres {
		if w.Column == "tenant_id" && w.Value == int64(10) {
			hasTenant = true
		}
		if w.Column == "org_id" && w.Value == int64(20) {
			hasOrg = true
		}
		if w.Column == "doctor_id" && w.Value == int64(9001) {
			hasDoctorID = true
		}
	}

	assert.True(t, hasTenant, "mutation where must enforce tenant_id")
	assert.True(t, hasOrg, "mutation where must enforce org_id")
	assert.True(t, hasDoctorID, "mutation where must enforce doctor_id = session.DoctorID for StrictSelf")
}

