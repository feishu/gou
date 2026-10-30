package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestScopeTypesLatticeOrdering 严格验证 8 级偏序格的全序单调递增性
func TestScopeTypesLatticeOrdering(t *testing.T) {
	// 顺序对齐 PRD V3.3: Denied(0) < Self(1) < MedicalGroup(15) < Unit(2) < UnitSub(3) < MultiUnit(31) < Campus(4) < MultiCampus(41) < Org(6) < Tenant(7) < MultiTenant(71) < All(10)
	orderedTypes := []ScopeType{
		ScopeTypeDenied,
		ScopeTypeSelf,
		ScopeTypeMedicalGroup,
		ScopeTypeUnit,
		ScopeTypeUnitSub,
		ScopeTypeMultiUnit,
		ScopeTypeCampus,
		ScopeTypeMultiCampus,
		ScopeTypeOrg,
		ScopeTypeTenant,
		ScopeTypeMultiTenant,
		ScopeTypeAll,
	}

	for i := 0; i < len(orderedTypes)-1; i++ {
		current := orderedTypes[i]
		next := orderedTypes[i+1]

		assert.True(t, current.Rank() < next.Rank(), "Rank ordering violation between %v (rank %d) and %v (rank %d)", current, current.Rank(), next, next.Rank())
		assert.True(t, next.GreaterThan(current), "%v should be GreaterThan %v", next, current)
		assert.True(t, current.LessThan(next), "%v should be LessThan %v", current, next)
		assert.True(t, next.GreaterOrEqual(current), "%v should be GreaterOrEqual %v", next, current)
		assert.True(t, current.LessOrEqual(next), "%v should be LessOrEqual %v", current, next)
	}

	// 验证向前兼容别名
	assert.Equal(t, ScopeTypeUnit, ScopeTypeDept, "ScopeTypeDept should be an alias of ScopeTypeUnit")
	assert.Equal(t, ScopeTypeUnitSub, ScopeTypeDeptSub, "ScopeTypeDeptSub should be an alias of ScopeTypeUnitSub")
}

// TestNormalizeLegacyScopeType 验证存量历史配置平滑向前兼容
func TestNormalizeLegacyScopeType(t *testing.T) {
	assert.Equal(t, ScopeTypeOrg, NormalizeLegacyScopeType(4), "Legacy 4 must map to ScopeTypeOrg(6)")
	assert.Equal(t, ScopeTypeTenant, NormalizeLegacyScopeType(5), "Legacy 5 must map to ScopeTypeTenant(7)")
	assert.Equal(t, ScopeTypeSelf, NormalizeLegacyScopeType(1), "1 remains ScopeTypeSelf")
	assert.Equal(t, ScopeTypeUnit, NormalizeLegacyScopeType(2), "2 remains ScopeTypeUnit")
	assert.Equal(t, ScopeTypeAll, NormalizeLegacyScopeType(10), "10 remains ScopeTypeAll")
}

// TestQueryParamDataScopeBypass 验证 QueryParam 在 JSON 反序列化时对 data_scope: false 显式旁路的解析
func TestQueryParamDataScopeBypass(t *testing.T) {
	// 1. 测试 data_scope: false
	payload1 := `{"model":"syd_register_order","data_scope":false,"limit":10}`
	var qp1 QueryParam
	err := json.Unmarshal([]byte(payload1), &qp1)
	assert.NoError(t, err)
	assert.NotNil(t, qp1.DataScope)
	assert.False(t, *qp1.DataScope, "data_scope must be explicitly false")

	// 2. 测试 disable_data_scope: true
	payload2 := `{"disable_data_scope":true}`
	var qp2 QueryParam
	err = json.Unmarshal([]byte(payload2), &qp2)
	assert.NoError(t, err)
	assert.True(t, qp2.DisableDataScope, "disable_data_scope must be true")

	// 3. 测试 bypass_datascope: true
	payload3 := `{"bypass_datascope":true}`
	var qp3 QueryParam
	err = json.Unmarshal([]byte(payload3), &qp3)
	assert.NoError(t, err)
	assert.True(t, qp3.BypassDataScope, "bypass_datascope must be true")
}

// TestEnumsAndSentinels 验证医护双轴、主从表分级、写策略枚举及哨兵错误
func TestEnumsAndSentinels(t *testing.T) {
	// 1. 医护双轴
	assert.Equal(t, AnchorAxis("clinical_dept"), AxisClinicalDept)
	assert.Equal(t, AnchorAxis("nursing_ward"), AxisNursingWard)
	assert.Equal(t, AnchorAxis("dual_union"), AxisDualUnion)

	// 2. 主从表角色
	assert.Equal(t, TableRole("MASTER_ENTITY"), TableRoleMaster)
	assert.Equal(t, TableRole("DETAIL_SLAVE"), TableRoleSlave)

	// 3. 写策略
	assert.Equal(t, MutationPolicy("strict_self"), MutationStrictSelf)
	assert.Equal(t, MutationPolicy("read_only"), MutationReadOnly)
	assert.Equal(t, MutationPolicy("follow_read"), MutationFollowRead)
	assert.Equal(t, MutationPolicy("custom_level"), MutationCustomLevel)

	// 4. 新增哨兵错误
	assert.NotNil(t, ErrDataScopeReadOnlyBlocked)
	assert.NotNil(t, ErrDataScopeStrictSelfViolation)
	assert.NotNil(t, ErrDataScopeDeptMismatch)
	assert.NotNil(t, ErrBreakGlassLimitExceeded)
}

