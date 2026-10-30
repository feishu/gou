package model

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yaoapp/kun/log"
)

// BeforeQueryDataScope 为模型注入行级数据权限切面
func (mod *Model) BeforeQueryDataScope(ctx context.Context, global map[string]interface{}, param *QueryParam) error {
	if os.Getenv("YAO_DATASCOPE_GLOBAL_DISABLE") == "true" {
		return nil
	}

	// 1. 检查 Bypass 短路：上游显式传入 data_scope: false / disable_data_scope: true / bypass_data_scope: true
	if param != nil {
		if param.DisableDataScope || param.BypassDataScope || (param.DataScope != nil && !*param.DataScope) {
			modID := "unknown"
			if mod != nil {
				modID = mod.ID
			}
			log.Trace("[DATA_SCOPE_BYPASS] Model: %s, QueryParam bypassed data scope", modID)
			return nil
		}
	}
	if global != nil {
		if val, ok := global["data_scope"]; ok {
			if b, ok := val.(bool); ok && !b {
				return nil
			}
		}
		if val, ok := global["disable_data_scope"]; ok {
			if b, ok := val.(bool); ok && b {
				return nil
			}
		}
		if val, ok := global["bypass_data_scope"]; ok {
			if b, ok := val.(bool); ok && b {
				return nil
			}
		}
	}

	spec := mod.GetDataScopeSpec()
	if !spec.Enabled {
		return nil
	}

	sec, bindings := ResolveSecurityContext(ctx, global)

	// V3.3: 优先使用双轨角色元组 (RoleScopeTuple) 进行降阶，兼容回退至经典 bindings
	var plan ReducedScopePlan
	if global != nil {
		if tuples, ok := global["role_scope_tuples"].([]RoleScopeTuple); ok && len(tuples) > 0 {
			var intentCap ScopeType
			if sec != nil && sec.IntentScopeCap > 0 {
				intentCap = sec.IntentScopeCap
			}
			plan, _ = ReduceRoleScopeTuples(sec, tuples, intentCap)
		}
	}
	if plan.EffectiveReadScope == 0 {
		plan = ReduceRoleScopes(sec, bindings)
	}

	var customSchema *ConditionValueSchema
	for _, b := range bindings {
		if b.ScopeType == ScopeTypeCustom && b.RuleJSON != "" {
			var err error
			customSchema, err = CompileAndValidateCondition(mod, b.RuleJSON)
			if err != nil {
				return err
			}
			break
		}
	}

	return BuildAndInjectQueryWhere(mod, param, sec, plan, spec, customSchema)
}

// BuildAndInjectQueryWhere AST 双括号隔离与安全数据权限注入引擎
func BuildAndInjectQueryWhere(
	mod *Model,
	param *QueryParam,
	sec *SecurityContext,
	plan ReducedScopePlan,
	spec ModelDataScopeSpec,
	customSchema *ConditionValueSchema,
) error {
	if plan.IsUnrestricted {
		return nil
	}

	prefix := resolveTableAlias(mod, param)

	// 1. 故障闭锁 (Fail-Closed)：注入恒假条件
	if plan.IsDenied {
		pkCol := "id"
		if mod != nil && mod.PrimaryKey != "" {
			pkCol = mod.PrimaryKey
		}
		injectSafeGroup(param, QueryWhere{
			Method: "where",
			Column: formatColumn(prefix, pkCol),
			OP:     "=",
			Value:  -999999999,
		})
		return nil
	}

	var topAndWheres []QueryWhere

	// 2. 提取公因子：TenantID (多租户绝对基石，单租户及多租户实参均必须隔离)
	if spec.TenantField != "" && sec != nil && sec.TenantID > 0 {
		if mod == nil || mod.Columns == nil || mod.Columns[spec.TenantField] != nil {
			if len(plan.AllowedTenantIDs) > 0 {
				topAndWheres = append(topAndWheres, buildInOrEqWhere("where", formatColumn(prefix, spec.TenantField), plan.AllowedTenantIDs))
			} else {
				topAndWheres = append(topAndWheres, QueryWhere{
					Method: "where",
					Column: formatColumn(prefix, spec.TenantField),
					Value:  sec.TenantID,
				})
			}
		}
	}

	// 3. 提取公因子：OrgID
	if spec.OrgField != "" {
		if mod == nil || mod.Columns == nil || mod.Columns[spec.OrgField] != nil {
			if plan.EnforceOrg && sec != nil && sec.OrgID > 0 {
				topAndWheres = append(topAndWheres, QueryWhere{
					Method: "where",
					Column: formatColumn(prefix, spec.OrgField),
					Value:  sec.OrgID,
				})
			} else if len(plan.AllowedOrgIDs) > 0 {
				topAndWheres = append(topAndWheres, buildInOrEqWhere("where", formatColumn(prefix, spec.OrgField), plan.AllowedOrgIDs))
			} else if sec != nil && sec.OrgID > 0 {
				topAndWheres = append(topAndWheres, QueryWhere{
					Method: "where",
					Column: formatColumn(prefix, spec.OrgField),
					Value:  sec.OrgID,
				})
			}
		}
	}

	// 3.1 主从表降阶支持 (V3.3)：明细从表仅继承租户和机构隔离边界，跳过科室/人员检查与注入
	if spec.TableRole == TableRoleSlave {
		if len(topAndWheres) == 0 {
			return nil
		}
		if strings.EqualFold(spec.Mode, "shadow") {
			modID := "unknown"
			if mod != nil {
				modID = mod.ID
			}
			logShadowScopeDiff(modID, param, topAndWheres)
			return nil
		}
		injectSafeGroup(param, QueryWhere{
			Method: "where",
			Wheres: topAndWheres,
		})
		return nil
	}

	// 4. 自定义规则优先
	if customSchema != nil && len(customSchema.Rules) > 0 {
		customGroup, err := buildCustomConditionGroup(prefix, sec, plan, customSchema)
		if err != nil {
			return err
		}
		if len(customGroup.Wheres) > 0 || customGroup.Column != nil {
			topAndWheres = append(topAndWheres, customGroup)
		}
	} else {
		// 5. 标准部门与人员正交维度并集
		var innerOrWheres []QueryWhere

		if len(plan.AllowedDeptIDs) > 0 && spec.DeptField != "" {
			if mod == nil || mod.Columns == nil || mod.Columns[spec.DeptField] != nil {
				deptWhere := buildOptimizedDeptWhere(mod, prefix, spec.DeptField, sec, plan)
				innerOrWheres = append(innerOrWheres, deptWhere)
			}
		}

		if plan.IncludeSelf && spec.UserField != "" && sec != nil && sec.UserID > 0 {
			if mod == nil || mod.Columns == nil || mod.Columns[spec.UserField] != nil {
				m := "where"
				if len(innerOrWheres) > 0 {
					m = "orwhere"
				}
				innerOrWheres = append(innerOrWheres, QueryWhere{
					Method: m,
					Column: formatColumn(prefix, spec.UserField),
					Value:  sec.UserID,
				})
			}
		}

		// 故障闭锁防御：启用了权限但缺失必要字段
		if (len(plan.AllowedDeptIDs) > 0 || plan.IncludeSelf) && !plan.EnforceOrg && len(innerOrWheres) == 0 {
			modID := "unknown"
			if mod != nil {
				modID = mod.ID
			}
			return fmt.Errorf("security block: model [%s] enabled data_scope but lacks mapped dept/user columns", modID)
		}

		if len(innerOrWheres) == 1 {
			topAndWheres = append(topAndWheres, innerOrWheres[0])
		} else if len(innerOrWheres) > 1 {
			topAndWheres = append(topAndWheres, QueryWhere{
				Method: "where",
				Wheres: innerOrWheres,
			})
		}
	}

	if len(topAndWheres) == 0 {
		return nil
	}

	// 6. 观察模式 Diff 日志
	if strings.EqualFold(spec.Mode, "shadow") {
		modID := "unknown"
		if mod != nil {
			modID = mod.ID
		}
		logShadowScopeDiff(modID, param, topAndWheres)
		return nil
	}

	// 7. 双括号代数隔离注入
	injectSafeGroup(param, QueryWhere{
		Method: "where",
		Wheres: topAndWheres,
	})
	return nil
}

func resolveTableAlias(mod *Model, param *QueryParam) string {
	if param != nil && param.Alias != "" {
		return param.Alias
	}
	return ""
}

func formatColumn(prefix, col string) string {
	if prefix != "" {
		return fmt.Sprintf("%s.%s", prefix, col)
	}
	return col
}

func buildInOrEqWhere(method, col string, ids []int64) QueryWhere {
	if len(ids) == 1 {
		return QueryWhere{Method: method, Column: col, Value: ids[0]}
	}
	vals := make([]interface{}, len(ids))
	for i, v := range ids {
		vals[i] = v
	}
	return QueryWhere{Method: method, OP: "in", Column: col, Value: vals}
}

func buildOptimizedDeptWhere(mod *Model, prefix, deptCol string, sec *SecurityContext, plan ReducedScopePlan) QueryWhere {
	fullDeptCol := formatColumn(prefix, deptCol)

	// 策略 1：Base36 物化路径前缀闭区间扫描
	if mod != nil && mod.Columns != nil {
		if _, hasPath := mod.Columns["dept_path"]; hasPath && sec != nil && sec.DeptPathPrefix != "" && len(plan.RootAncestorDeptIDs) == 1 && !plan.HasCustomDiscreteDepts {
			pathCol := formatColumn(prefix, "dept_path")
			return QueryWhere{
				Method: "where",
				Wheres: []QueryWhere{
					{Method: "where", Column: pathCol, OP: ">=", Value: sec.DeptPathPrefix},
					{Method: "where", Column: pathCol, OP: "<", Value: sec.DeptPathPrefix + "~"},
				},
			}
		}
	}

	// 策略 2：标准参数化集合绑定
	return buildInOrEqWhere("where", fullDeptCol, plan.AllowedDeptIDs)
}

func injectSafeGroup(param *QueryParam, group QueryWhere) {
	if len(param.Wheres) == 0 {
		param.Wheres = []QueryWhere{group}
		return
	}
	orig := param.Wheres
	param.Wheres = []QueryWhere{
		{Method: "where", Wheres: orig},
		group,
	}
}

func buildCustomConditionGroup(prefix string, sec *SecurityContext, plan ReducedScopePlan, schema *ConditionValueSchema) (QueryWhere, error) {
	var subWheres []QueryWhere
	for _, rule := range schema.Rules {
		col := formatColumn(prefix, rule.Field)
		var val interface{} = rule.Value

		switch rule.Slot {
		case "__user_id":
			if sec != nil {
				val = sec.UserID
			}
		case "__org_id":
			if sec != nil {
				val = sec.OrgID
			}
		case "__dept_id":
			if sec != nil {
				val = sec.DeptID
			}
		case "__dept_ids":
			if sec != nil {
				val = sec.SubDeptIDs
			}
		case "__tenant_id":
			if sec != nil {
				val = sec.TenantID
			}
		}

		m := "where"
		if strings.EqualFold(schema.Logic, "OR") && len(subWheres) > 0 {
			m = "orwhere"
		}

		subWheres = append(subWheres, QueryWhere{
			Method: m,
			Column: col,
			OP:     rule.Operator,
			Value:  val,
		})
	}

	if len(subWheres) == 0 {
		return QueryWhere{}, nil
	}
	if len(subWheres) == 1 {
		return subWheres[0], nil
	}
	return QueryWhere{
		Method: "where",
		Wheres: subWheres,
	}, nil
}

func logShadowScopeDiff(modelID string, param *QueryParam, topAndWheres []QueryWhere) {
	log.Trace("[DATA_SCOPE_SHADOW] Model: %s, Injected: %+v", modelID, topAndWheres)
}

// ResolveSecurityContext 从 context 和 global 中解析 SecurityContext 和 bindings
func ResolveSecurityContext(ctx context.Context, global map[string]interface{}) (*SecurityContext, []RoleScopeBinding) {
	if global == nil && ctx == nil {
		return nil, nil
	}

	// 1. 优先从 global["__sec_ctx"] 读取
	if global != nil {
		if sec, ok := global["__sec_ctx"].(*SecurityContext); ok && sec != nil {
			bindings, _ := global["__sec_bindings"].([]RoleScopeBinding)
			return sec, bindings
		}
	}

	// 2. 从 global key-values 中构建
	sec := &SecurityContext{}
	if global != nil {
		if rawUID, ok := global["user_id"]; ok {
			sec.UserID, _ = toInt64(rawUID)
		}
		if rawTID, ok := global["tenant_id"]; ok {
			sec.TenantID, _ = toInt64(rawTID)
		}
		if rawOID, ok := global["org_id"]; ok {
			sec.OrgID, _ = toInt64(rawOID)
		}
		if rawDID, ok := global["dept_id"]; ok {
			sec.DeptID, _ = toInt64(rawDID)
		}
		if prefix, ok := global["dept_path_prefix"].(string); ok {
			sec.DeptPathPrefix = prefix
		}
		if rawInternal, ok := global["internal_call"].(bool); ok {
			sec.InternalCall = rawInternal
		}
		if rawAdmin, ok := global["has_admin_privilege"].(bool); ok {
			sec.HasAdminPrivilege = rawAdmin
		}
		if rawSubs, ok := global["sub_dept_ids"]; ok {
			sec.SubDeptIDs = toInt64Slice(rawSubs)
		}
		// V3.3: 提取多院区、病区、医疗组与医生维度上下文
		if rawDocID, ok := global["doctor_id"]; ok {
			sec.DoctorID, _ = toInt64(rawDocID)
		}
		if rawCampus, ok := global["active_campus_id"]; ok {
			sec.ActiveCampusID, _ = toInt64(rawCampus)
		}
		if rawActDept, ok := global["active_dept_id"]; ok {
			sec.ActiveDeptID, _ = toInt64(rawActDept)
		}
		if rawWard, ok := global["active_ward_id"]; ok {
			sec.ActiveWardID, _ = toInt64(rawWard)
		}
		if rawGroup, ok := global["active_group_id"]; ok {
			sec.ActiveGroupID, _ = toInt64(rawGroup)
		}
		if rawNurse, ok := global["is_outpatient_nurse"].(bool); ok {
			sec.IsOutpatientNurse = rawNurse
		}
		if rawCross, ok := global["allow_cross_ward"].(bool); ok {
			sec.AllowCrossWard = rawCross
		}
		if rawCampuses, ok := global["allowed_campus_ids"]; ok {
			sec.AllowedCampusIDs = toInt64Slice(rawCampuses)
		}
		if rawWards, ok := global["allowed_ward_ids"]; ok {
			sec.AllowedWardIDs = toInt64Slice(rawWards)
		}
		if rawGroups, ok := global["allowed_group_ids"]; ok {
			sec.AllowedGroupIDs = toInt64Slice(rawGroups)
		}
		if sec.DeptID <= 0 && sec.ActiveDeptID > 0 {
			sec.DeptID = sec.ActiveDeptID
		}
	}

	// 3. 构建默认 bindings
	var bindings []RoleScopeBinding
	if global != nil {
		if b, ok := global["role_scope_bindings"].([]RoleScopeBinding); ok {
			bindings = b
		}
	}

	if len(bindings) == 0 && sec.UserID > 0 {
		if sec.HasAdminPrivilege || sec.InternalCall {
			bindings = []RoleScopeBinding{{ScopeType: ScopeTypeAll}}
		} else if sec.OrgID > 0 && sec.DeptID <= 0 {
			bindings = []RoleScopeBinding{{ScopeType: ScopeTypeOrg, OrgIDs: []int64{sec.OrgID}}}
		} else if sec.DeptID > 0 {
			if len(sec.SubDeptIDs) > 0 {
				bindings = []RoleScopeBinding{{ScopeType: ScopeTypeDeptSub, DeptIDs: append([]int64{sec.DeptID}, sec.SubDeptIDs...)}}
			} else {
				bindings = []RoleScopeBinding{{ScopeType: ScopeTypeDept, DeptIDs: []int64{sec.DeptID}}}
			}
		} else {
			bindings = []RoleScopeBinding{{ScopeType: ScopeTypeSelf}}
		}
	}

	return sec, bindings
}

func toInt64(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case int:
		return int64(val), true
	case int64:
		return val, true
	case int32:
		return int64(val), true
	case float64:
		return int64(val), true
	case float32:
		return int64(val), true
	}
	return 0, false
}

func toInt64Slice(v interface{}) []int64 {
	switch val := v.(type) {
	case []int64:
		return val
	case []int:
		res := make([]int64, len(val))
		for i, x := range val {
			res[i] = int64(x)
		}
		return res
	case []interface{}:
		var res []int64
		for _, x := range val {
			if num, ok := toInt64(x); ok {
				res = append(res, num)
			}
		}
		return res
	}
	return nil
}
