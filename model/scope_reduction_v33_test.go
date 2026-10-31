package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReduceRoleScopeTuples_PreventReadOnlyDeadlock
// 场景: 李主任同时持有“区域多租户督导(Read=71, Write=0)”与“本院门诊医生(Read=1, Write=1)”双角色
// 验证: 归约后读天花板=71(跨租户)，写天花板=1(仅本人)，写权限未被督导员的只读属性锁死！
func TestReduceRoleScopeTuples_PreventReadOnlyDeadlock(t *testing.T) {
	sec := &SecurityContext{
		UserID:   1001,
		DoctorID: 501,
		TenantID: 10,
		OrgID:    100,
		DeptID:   201,
	}

	tuples := []RoleScopeTuple{
		{
			RoleID:           1, // 区域多租户督导
			AnchorAxis:       AxisClinicalDept,
			ReadScope:        ScopeTypeMultiTenant, // Level 71
			MutationPolicy:   MutationReadOnly,     // Level 0
			GrantedTenantIDs: []int64{10, 20},
		},
		{
			RoleID:         2, // 本院出诊医生
			AnchorAxis:     AxisClinicalDept,
			ReadScope:      ScopeTypeSelf,       // Level 1
			MutationPolicy: MutationStrictSelf, // Level 1
			IdentitySlots:  []string{"doctor_id"},
		},
	}

	readPlan, writePlan := ReduceRoleScopeTuples(sec, tuples, ScopeTypeDenied)

	// 1. 验证读天花板取 Max(71, 1) = 71
	assert.Equal(t, ScopeTypeMultiTenant, readPlan.EffectiveReadScope)
	assert.Equal(t, []int64{10, 20}, readPlan.AllowedTenantIDs)
	assert.False(t, readPlan.IsDenied)

	// 2. 验证写天花板取 Max(0, 1) = 1 (绝不被 0 锁死)
	assert.Equal(t, ScopeTypeSelf, writePlan.EffectiveWriteScope)
	assert.False(t, writePlan.IsReadOnlyBlocked, "Write permission must NOT be blocked by read-only supervisor role")
	assert.Equal(t, int64(501), writePlan.StrictSelfDoctorID)
}

// TestReduceRoleScopeTuples_ReceptionDeskIntentConvergence
// 场景: 李主任进入【今日待接诊】工作台，路由声明 IntentScopeCap = Level 1 (仅本人)
// 验证: 读有效域自动 Min(71, 1) = 1，防止全科/跨租户单据污染看诊；写有效域保持 Min(1, 1) = 1
func TestReduceRoleScopeTuples_ReceptionDeskIntentConvergence(t *testing.T) {
	sec := &SecurityContext{
		UserID:         1001,
		DoctorID:       501,
		TenantID:       10,
		OrgID:          100,
		DeptID:         201,
		IntentScopeCap: ScopeTypeSelf, // 路由视角上限锁定为 Level 1
	}

	tuples := []RoleScopeTuple{
		{
			RoleID:           1, // 区域多租户督导
			AnchorAxis:       AxisClinicalDept,
			ReadScope:        ScopeTypeMultiTenant, // Level 71
			MutationPolicy:   MutationReadOnly,
			GrantedTenantIDs: []int64{10, 20},
		},
		{
			RoleID:         2, // 本院出诊医生
			AnchorAxis:     AxisClinicalDept,
			ReadScope:      ScopeTypeSelf, // Level 1
			MutationPolicy: MutationStrictSelf,
		},
	}

	readPlan, writePlan := ReduceRoleScopeTuples(sec, tuples, ScopeTypeDenied)

	// 1. 验证读有效域收敛为 Level 1
	assert.Equal(t, ScopeTypeSelf, readPlan.EffectiveReadScope)
	assert.True(t, readPlan.IncludeSelf)

	// 2. 验证写有效域保持为 Level 1
	assert.Equal(t, ScopeTypeSelf, writePlan.EffectiveWriteScope)
	assert.False(t, writePlan.IsReadOnlyBlocked)
}

// TestReduceRoleScopeTuples_NursingWardAxisRouting
// 场景: 大科护士长管辖 3 个病区 (Level 31, AnchorAxis = nursing_ward)
// 验证: 正确捕获护理病区轴，并精准聚合 GrantedWardIDs
func TestReduceRoleScopeTuples_NursingWardAxisRouting(t *testing.T) {
	sec := &SecurityContext{
		UserID: 2001,
		WardID: 501,
	}

	tuples := []RoleScopeTuple{
		{
			RoleID:         3,
			AnchorAxis:     AxisNursingWard,
			ReadScope:      ScopeTypeMultiUnit,
			MutationPolicy: MutationStrictSelf,
			GrantedWardIDs: []int64{501, 502, 508},
		},
	}

	readPlan, writePlan := ReduceRoleScopeTuples(sec, tuples, ScopeTypeDenied)

	assert.Equal(t, ScopeTypeMultiUnit, readPlan.EffectiveReadScope)
	assert.Equal(t, AxisNursingWard, readPlan.AnchorAxis)
	assert.Equal(t, []int64{501, 502, 508}, readPlan.AllowedWardIDs)
	assert.False(t, writePlan.IsReadOnlyBlocked)
}

// TestReduceRoleScopeTuples_SubsumptionLattice
// 场景: 具有全院权限 (Level 6) 时，自动向下吞噬科室与个人维度
func TestReduceRoleScopeTuples_SubsumptionLattice(t *testing.T) {
	sec := &SecurityContext{
		UserID: 3001,
		OrgID:  100,
		DeptID: 201,
	}

	tuples := []RoleScopeTuple{
		{RoleID: 1, ReadScope: ScopeTypeSelf, MutationPolicy: MutationStrictSelf},
		{RoleID: 2, ReadScope: ScopeTypeUnit, MutationPolicy: MutationStrictSelf, GrantedDeptIDs: []int64{201}},
		{RoleID: 3, ReadScope: ScopeTypeOrg, MutationPolicy: MutationStrictSelf},
	}

	readPlan, _ := ReduceRoleScopeTuples(sec, tuples, ScopeTypeDenied)

	assert.Equal(t, ScopeTypeOrg, readPlan.EffectiveReadScope)
	assert.True(t, readPlan.EnforceOrg)
	assert.False(t, readPlan.IncludeSelf, "Org scope must subsume IncludeSelf")
	assert.Empty(t, readPlan.AllowedDeptIDs, "Org scope must subsume AllowedDeptIDs")
}

// TestParseRoleScopeTuples_MapAndJSON
// 验证反序列化来自 Session Dump 或 TS/JS 运行时的 []interface{} 与 JSON string
func TestParseRoleScopeTuples_MapAndJSON(t *testing.T) {
	rawMaps := []interface{}{
		map[string]interface{}{
			"role_id":            int(101),
			"anchor_axis":        "clinical_dept",
			"read_scope":         int(4), // ScopeTypeCampus
			"mutation_policy":    "strict_self",
			"granted_campus_ids": []interface{}{int(1), int(2)},
		},
		map[string]interface{}{
			"role_id":         "102",
			"anchor_axis":     "nursing_ward",
			"read_scope":      "2", // ScopeTypeUnit
			"mutation_policy": "read_only",
			"granted_ward_ids": []interface{}{"301", "302"},
		},
	}

	tuples := parseRoleScopeTuples(rawMaps)
	assert.Len(t, tuples, 2)
	assert.Equal(t, int64(101), tuples[0].RoleID)
	assert.Equal(t, ScopeTypeCampus, tuples[0].ReadScope)
	assert.Equal(t, AxisClinicalDept, tuples[0].AnchorAxis)
	assert.Equal(t, []int64{1, 2}, tuples[0].GrantedCampusIDs)

	assert.Equal(t, int64(102), tuples[1].RoleID)
	assert.Equal(t, ScopeTypeUnit, tuples[1].ReadScope)
	assert.Equal(t, AxisNursingWard, tuples[1].AnchorAxis)
	assert.Equal(t, MutationReadOnly, tuples[1].MutationPolicy)
	assert.Equal(t, []int64{301, 302}, tuples[1].GrantedWardIDs)
}

// TestResolveSecurityContext_FallbackAliases
// 验证 uid / oid / tid / doctor_id / patient_id 等会话别名的正确解析
func TestResolveSecurityContext_FallbackAliases(t *testing.T) {
	global := map[string]interface{}{
		"uid":        "8001",
		"oid":        "101",
		"tid":        "1000000000",
		"doctor_id":  "501",
		"patient_id": "9001",
	}

	sec, _ := ResolveSecurityContext(nil, global)
	assert.NotNil(t, sec)
	assert.Equal(t, int64(8001), sec.UserID)
	assert.Equal(t, int64(101), sec.OrgID)
	assert.Equal(t, int64(1000000000), sec.TenantID)
	assert.Equal(t, int64(501), sec.DoctorID)
	assert.Equal(t, int64(9001), sec.PatientID)
}

