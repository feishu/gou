package model

import (
	"sort"
)

// ReduceRoleScopes 偏序格归约与公因子提取算法
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
		case ScopeTypeTenant:
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
