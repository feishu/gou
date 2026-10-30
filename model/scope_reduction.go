package model

import (
	"sort"
)

// ReduceRoleScopes 偏序格归约与公因子提取算法 (保证历史单测与调用的 100% 稳定兼容)
func ReduceRoleScopes(sec *SecurityContext, bindings []RoleScopeBinding) ReducedScopePlan {
	if sec == nil {
		return ReducedScopePlan{IsDenied: true}
	}
	if sec.InternalCall || sec.HasAdminPrivilege {
		return ReducedScopePlan{IsUnrestricted: true}
	}
	if len(bindings) == 0 {
		// 故障闭锁 (Fail-Closed)
		return ReducedScopePlan{IsDenied: true}
	}

	var (
		hasTenantScope    bool
		hasOrgScope       bool
		hasSelfScope      bool
		deptSet           = make(map[int64]struct{})
		orgSet            = make(map[int64]struct{})
		ancestors         []int64
		hasCustomDiscrete bool
	)

	for _, b := range bindings {
		switch b.ScopeType {
		case ScopeTypeAll:
			return ReducedScopePlan{IsUnrestricted: true} // 短路全量放行
		case ScopeTypeTenant, ScopeTypeMultiTenant:
			hasTenantScope = true
		case ScopeTypeOrg:
			hasOrgScope = true
			if len(b.OrgIDs) > 0 {
				for _, oid := range b.OrgIDs {
					if oid > 0 {
						orgSet[oid] = struct{}{}
					}
				}
			} else if sec.OrgID > 0 {
				orgSet[sec.OrgID] = struct{}{}
			}
		case ScopeTypeDeptSub:
			if sec.DeptID > 0 {
				deptSet[sec.DeptID] = struct{}{}
				ancestors = append(ancestors, sec.DeptID)
			}
			for _, subID := range sec.SubDeptIDs {
				if subID > 0 {
					deptSet[subID] = struct{}{}
				}
			}
			for _, did := range b.DeptIDs {
				if did > 0 {
					deptSet[did] = struct{}{}
					hasCustomDiscrete = true
				}
			}
		case ScopeTypeDept:
			if sec.DeptID > 0 {
				deptSet[sec.DeptID] = struct{}{}
			}
			for _, did := range b.DeptIDs {
				if did > 0 {
					deptSet[did] = struct{}{}
					hasCustomDiscrete = true
				}
			}
		case ScopeTypeCustom:
			for _, did := range b.DeptIDs {
				if did > 0 {
					deptSet[did] = struct{}{}
					hasCustomDiscrete = true
				}
			}
			for _, oid := range b.OrgIDs {
				if oid > 0 {
					orgSet[oid] = struct{}{}
				}
			}
		case ScopeTypeSelf:
			hasSelfScope = true
		}
	}

	// 偏序包含公因子吞噬 (Subsumption Lattice):
	// 若已获得全院(Org)或全租户(Tenant)权限，则向下完全吞噬科室与个人维度，消除多余 OR 条件破坏索引
	if hasOrgScope {
		return ReducedScopePlan{
			EnforceTenant: hasTenantScope,
			EnforceOrg:    true,
			AllowedOrgIDs: toSortedSlice(orgSet),
		}
	}

	return ReducedScopePlan{
		EnforceTenant:          hasTenantScope,
		IncludeSelf:            hasSelfScope,
		AllowedOrgIDs:          toSortedSlice(orgSet),
		AllowedDeptIDs:         toSortedSlice(deptSet),
		RootAncestorDeptIDs:    ancestors,
		HasCustomDiscreteDepts: hasCustomDiscrete,
	}
}

// ReduceRoleScopeTuples 执行角色二元组解析、跨角色独立 Max 天花板归约，以及结合意图的 Min 收敛 (V3.3 核心)
func ReduceRoleScopeTuples(sec *SecurityContext, tuples []RoleScopeTuple, baselineRead ScopeType) (ReducedScopePlan, ReducedMutationPlan) {
	if sec == nil {
		return ReducedScopePlan{IsDenied: true}, ReducedMutationPlan{IsReadOnlyBlocked: true}
	}
	if sec.InternalCall || sec.HasAdminPrivilege {
		return ReducedScopePlan{IsUnrestricted: true}, ReducedMutationPlan{EffectiveWriteScope: ScopeTypeAll}
	}
	if len(tuples) == 0 {
		return ReducedScopePlan{IsDenied: true}, ReducedMutationPlan{IsReadOnlyBlocked: true}
	}

	var (
		ceilingRead  ScopeType = baselineRead
		ceilingWrite ScopeType = ScopeTypeDenied

		tenantSet = make(map[int64]struct{})
		campusSet = make(map[int64]struct{})
		orgSet    = make(map[int64]struct{})
		deptSet   = make(map[int64]struct{})
		wardSet   = make(map[int64]struct{})
		slotsSet  = make(map[string]struct{})
		ancestors []int64

		primaryAxis       AnchorAxis = AxisClinicalDept
		hasCustomDiscrete bool
	)

	// 1. 遍历计算单角色写偏序，并求跨角色 Max 天花板
	for _, t := range tuples {
		// 单角色结算写等级
		var roleWrite ScopeType
		switch t.MutationPolicy {
		case MutationReadOnly:
			roleWrite = ScopeTypeDenied
		case MutationStrictSelf:
			roleWrite = ScopeTypeSelf
		case MutationFollowRead:
			roleWrite = t.ReadScope
		case MutationCustomLevel:
			roleWrite = t.CustomMutationScope
		default:
			roleWrite = ScopeTypeSelf
		}

		if t.ReadScope.GreaterThan(ceilingRead) {
			ceilingRead = t.ReadScope
			if t.AnchorAxis != "" {
				primaryAxis = t.AnchorAxis
			}
		}
		if roleWrite.GreaterThan(ceilingWrite) {
			ceilingWrite = roleWrite
		}

		// 收集动态管辖实参
		for _, tid := range t.GrantedTenantIDs {
			if tid > 0 {
				tenantSet[tid] = struct{}{}
			}
		}
		for _, cid := range t.GrantedCampusIDs {
			if cid > 0 {
				campusSet[cid] = struct{}{}
			}
		}
		for _, did := range t.GrantedDeptIDs {
			if did > 0 {
				deptSet[did] = struct{}{}
				hasCustomDiscrete = true
			}
		}
		for _, wid := range t.GrantedWardIDs {
			if wid > 0 {
				wardSet[wid] = struct{}{}
				hasCustomDiscrete = true
			}
		}
		for _, slot := range t.IdentitySlots {
			if slot != "" {
				slotsSet[slot] = struct{}{}
			}
		}

		// 处理 Unit / UnitSub 默认绑定的 SecurityContext 部门
		if t.ReadScope == ScopeTypeUnitSub {
			if sec.DeptID > 0 {
				deptSet[sec.DeptID] = struct{}{}
				ancestors = append(ancestors, sec.DeptID)
			}
			for _, subID := range sec.SubDeptIDs {
				if subID > 0 {
					deptSet[subID] = struct{}{}
				}
			}
		} else if t.ReadScope == ScopeTypeUnit {
			if sec.DeptID > 0 {
				deptSet[sec.DeptID] = struct{}{}
			}
			if sec.WardID > 0 {
				wardSet[sec.WardID] = struct{}{}
			}
		}
	}

	// 2. 结合请求意图 (IntentScopeCap) 进行 Min 收敛
	effRead := ceilingRead
	if sec.IntentScopeCap > 0 && sec.IntentScopeCap.LessThan(effRead) {
		effRead = sec.IntentScopeCap
	}

	effWrite := ceilingWrite
	if effRead.LessThan(effWrite) {
		effWrite = effRead
	}

	// 3. 构建读计划
	readPlan := ReducedScopePlan{
		EffectiveReadScope:     effRead,
		AnchorAxis:             primaryAxis,
		AllowedTenantIDs:       toSortedSlice(tenantSet),
		AllowedCampusIDs:       toSortedSlice(campusSet),
		AllowedOrgIDs:          toSortedSlice(orgSet),
		AllowedDeptIDs:         toSortedSlice(deptSet),
		AllowedWardIDs:         toSortedSlice(wardSet),
		RootAncestorDeptIDs:    ancestors,
		HasCustomDiscreteDepts: hasCustomDiscrete,
		IdentitySlots:          mapKeysToSlice(slotsSet),
	}

	if effRead == ScopeTypeAll {
		readPlan.IsUnrestricted = true
		return readPlan, ReducedMutationPlan{
			EffectiveWriteScope: effWrite,
			StrictSelfDoctorID:  sec.DoctorID,
			AllowedWriteDeptIDs: toSortedSlice(deptSet),
		}
	}

	if effRead == ScopeTypeTenant || effRead == ScopeTypeMultiTenant {
		readPlan.EnforceTenant = true
	}
	if effRead == ScopeTypeOrg {
		readPlan.EnforceOrg = true
	}
	if effRead == ScopeTypeSelf {
		readPlan.IncludeSelf = true
	}

	// 偏序公因子吞噬 (Subsumption Lattice):
	// 若当前有效读域达到全院(Org)或全租户(Tenant)，向下完全吞噬科室与个人维度，消除多余 OR 条件破坏索引
	if readPlan.EnforceOrg || readPlan.EnforceTenant {
		readPlan.AllowedDeptIDs = nil
		readPlan.AllowedWardIDs = nil
		readPlan.IncludeSelf = false
	}

	// 4. 构建写计划
	writePlan := ReducedMutationPlan{
		EffectiveWriteScope:  effWrite,
		IsReadOnlyBlocked:    (effWrite == ScopeTypeDenied),
		StrictSelfDoctorID:   sec.DoctorID,
		AllowedWriteDeptIDs:  toSortedSlice(deptSet),
		EnforceActiveContext: true,
	}

	return readPlan, writePlan
}

func toSortedSlice(m map[int64]struct{}) []int64 {
	if len(m) == 0 {
		return nil
	}
	res := make([]int64, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	return res
}

func mapKeysToSlice(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}
