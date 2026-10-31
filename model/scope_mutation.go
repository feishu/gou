package model

import (
	"context"
	"fmt"
	"os"

	"github.com/yaoapp/kun/maps"
)

// isMutationBypassed 判定写操作是否显式旁路穿透
func isMutationBypassed(global map[string]interface{}) bool {
	if os.Getenv("YAO_DATASCOPE_GLOBAL_DISABLE") == "true" {
		return true
	}
	if global != nil {
		if val, ok := global["data_scope"]; ok {
			if b, ok := val.(bool); ok && !b {
				return true
			}
		}
		if val, ok := global["disable_data_scope"]; ok {
			if b, ok := val.(bool); ok && b {
				return true
			}
		}
		if val, ok := global["bypass_data_scope"]; ok {
			if b, ok := val.(bool); ok && b {
				return true
			}
		}
	}
	return false
}

// resolveMutationPlan 解析当前安全上下文并计算写变更执行计划
func (mod *Model) resolveMutationPlan(ctx context.Context, global map[string]interface{}) (*SecurityContext, ReducedMutationPlan, ModelDataScopeSpec, error) {
	spec := mod.GetDataScopeSpec()
	if !spec.Enabled {
		return nil, ReducedMutationPlan{EffectiveWriteScope: ScopeTypeAll}, spec, nil
	}

	sec, bindings := ResolveSecurityContext(ctx, global)

	var writePlan ReducedMutationPlan
	if global != nil {
		if rawTuples, ok := global["role_scope_tuples"]; ok {
			tuples := parseRoleScopeTuples(rawTuples)
			if len(tuples) > 0 {
				var intentCap ScopeType
				if sec != nil && sec.IntentScopeCap > 0 {
					intentCap = sec.IntentScopeCap
				}
				_, writePlan = ReduceRoleScopeTuples(sec, tuples, intentCap)
			}
		}
	}

	// 兼容未传入 role_scope_tuples 的场景
	if writePlan.EffectiveWriteScope == 0 && !writePlan.IsReadOnlyBlocked {
		readPlan := ReduceRoleScopes(sec, bindings)
		if readPlan.IsUnrestricted {
			writePlan = ReducedMutationPlan{EffectiveWriteScope: ScopeTypeAll}
		} else if readPlan.IsDenied {
			writePlan = ReducedMutationPlan{IsReadOnlyBlocked: true, EffectiveWriteScope: ScopeTypeDenied}
		} else {
			writePlan = ReducedMutationPlan{
				EffectiveWriteScope: ScopeTypeSelf,
				StrictSelfDoctorID:  sec.DoctorID,
				AllowedWriteDeptIDs: readPlan.AllowedDeptIDs,
			}
			if readPlan.EnforceOrg {
				writePlan.EffectiveWriteScope = ScopeTypeOrg
			} else if len(readPlan.AllowedDeptIDs) > 0 {
				writePlan.EffectiveWriteScope = ScopeTypeUnit
			}
		}
	}

	return sec, writePlan, spec, nil
}

// injectMutationWhere 为写操作（UPDATE / DELETE）注入行级权限与隔离边界
func (mod *Model) injectMutationWhere(
	param *QueryParam,
	sec *SecurityContext,
	plan ReducedMutationPlan,
	spec ModelDataScopeSpec,
) error {
	if plan.EffectiveWriteScope == ScopeTypeAll {
		return nil
	}
	if plan.IsReadOnlyBlocked {
		return ErrDataScopeReadOnlyBlocked
	}

	prefix := resolveTableAlias(mod, param)
	var mutationWheres []QueryWhere

	// 1. 租户公因子
	if spec.TenantField != "" && sec != nil && sec.TenantID > 0 {
		if mod == nil || mod.Columns == nil || mod.Columns[spec.TenantField] != nil {
			mutationWheres = append(mutationWheres, QueryWhere{
				Method: "where",
				Column: formatColumn(prefix, spec.TenantField),
				Value:  sec.TenantID,
			})
		}
	}

	// 2. 机构公因子
	if spec.OrgField != "" && sec != nil && sec.OrgID > 0 {
		if mod == nil || mod.Columns == nil || mod.Columns[spec.OrgField] != nil {
			mutationWheres = append(mutationWheres, QueryWhere{
				Method: "where",
				Column: formatColumn(prefix, spec.OrgField),
				Value:  sec.OrgID,
			})
		}
	}

	// 3. 从表 (TableRoleSlave) 仅需租户与机构边界守护
	if spec.TableRole == TableRoleSlave {
		if len(mutationWheres) > 0 {
			injectSafeGroup(param, QueryWhere{
				Method: "where",
				Wheres: mutationWheres,
			})
		}
		return nil
	}

	// 4. 个人级写控制 (ScopeTypeSelf)：强制绑定医生或创建者
	if plan.EffectiveWriteScope == ScopeTypeSelf {
		doctorCol := "doctor_id"
		hasDoctorCol := false
		if mod != nil && mod.Columns != nil && mod.Columns[doctorCol] != nil {
			hasDoctorCol = true
		}

		if hasDoctorCol && sec != nil && sec.DoctorID > 0 {
			mutationWheres = append(mutationWheres, QueryWhere{
				Method: "where",
				Column: formatColumn(prefix, doctorCol),
				Value:  sec.DoctorID,
			})
		} else if spec.UserField != "" && sec != nil && sec.UserID > 0 {
			if mod == nil || mod.Columns == nil || mod.Columns[spec.UserField] != nil {
				mutationWheres = append(mutationWheres, QueryWhere{
					Method: "where",
					Column: formatColumn(prefix, spec.UserField),
					Value:  sec.UserID,
				})
			}
		}
	} else if plan.EffectiveWriteScope.Rank() >= ScopeTypeUnit.Rank() && plan.EffectiveWriteScope.Rank() < ScopeTypeOrg.Rank() {
		// 5. 科室级写控制
		if spec.DeptField != "" {
			if mod == nil || mod.Columns == nil || mod.Columns[spec.DeptField] != nil {
				if len(plan.AllowedWriteDeptIDs) > 0 {
					mutationWheres = append(mutationWheres, buildInOrEqWhere("where", formatColumn(prefix, spec.DeptField), plan.AllowedWriteDeptIDs))
				} else if sec != nil && (sec.ActiveDeptID > 0 || sec.DeptID > 0) {
					deptID := sec.ActiveDeptID
					if deptID <= 0 {
						deptID = sec.DeptID
					}
					mutationWheres = append(mutationWheres, QueryWhere{
						Method: "where",
						Column: formatColumn(prefix, spec.DeptField),
						Value:  deptID,
					})
				}
			}
		}
	}

	if len(mutationWheres) > 0 {
		injectSafeGroup(param, QueryWhere{
			Method: "where",
			Wheres: mutationWheres,
		})
	}
	return nil
}

// SecureUpdateByID 单点主键安全更新：强制数据归属防篡改、科室辖区核验，并免疫 IDOR 越权
func (mod *Model) SecureUpdateByID(ctx context.Context, global map[string]interface{}, id interface{}, payload maps.MapStrAny) error {
	if isMutationBypassed(global) {
		return mod.Update(id, payload, ctx)
	}

	sec, writePlan, spec, err := mod.resolveMutationPlan(ctx, global)
	if err != nil {
		return err
	}
	if !spec.Enabled {
		return mod.Update(id, payload, ctx)
	}

	// 1. 只读保护拦截
	if writePlan.IsReadOnlyBlocked {
		return ErrDataScopeReadOnlyBlocked
	}

	// 2. 净化 Payload
	if err := mod.sanitizeMutationPayload(sec, writePlan, spec, payload, false); err != nil {
		return err
	}

	param := QueryParam{
		Model:   mod.ID,
		Context: ctx,
		Wheres: []QueryWhere{
			{Method: "where", Column: mod.PrimaryKey, Value: id},
		},
	}

	if err := mod.injectMutationWhere(&param, sec, writePlan, spec); err != nil {
		return err
	}

	affected, err := mod.UpdateWhere(param, payload)
	if err != nil {
		return err
	}

	// 解决 MySQL AffectedRows == 0 导致的 403 误杀 (变动幂等性保障)
	if affected == 0 {
		countParam := QueryParam{
			Model:   mod.ID,
			Context: ctx,
			Wheres:  param.Wheres,
		}
		existsCount, err := mod.Count(countParam)
		if err != nil || existsCount == 0 {
			return ErrDataScopeAccessDenied
		}
	}
	return nil
}

// SecureCreate 单条记录安全创建：Before-Insert 守护，自动填充租户/机构归属，校验科室管辖权，强制本人医生身份绑定
func (mod *Model) SecureCreate(ctx context.Context, global map[string]interface{}, row maps.MapStrAny) (interface{}, error) {
	if isMutationBypassed(global) {
		return mod.Create(row, ctx)
	}

	sec, writePlan, spec, err := mod.resolveMutationPlan(ctx, global)
	if err != nil {
		return nil, err
	}
	if !spec.Enabled {
		return mod.Create(row, ctx)
	}

	// 1. 只读保护拦截
	if writePlan.IsReadOnlyBlocked {
		return nil, ErrDataScopeReadOnlyBlocked
	}

	// 2. 净化与字段强制注入
	if err := mod.sanitizeMutationPayload(sec, writePlan, spec, row, true); err != nil {
		return nil, err
	}

	return mod.Create(row, ctx)
}

// SecureSave 保存单条记录（存在主键自动走 SecureUpdateByID，否则走 SecureCreate）
func (mod *Model) SecureSave(ctx context.Context, global map[string]interface{}, row maps.MapStrAny) (interface{}, error) {
	if pkVal, hasPK := row[mod.PrimaryKey]; hasPK && pkVal != nil && pkVal != "" && pkVal != 0 {
		err := mod.SecureUpdateByID(ctx, global, pkVal, row)
		if err != nil {
			return nil, err
		}
		return pkVal, nil
	}
	return mod.SecureCreate(ctx, global, row)
}

// SecureDeleteByID 单点主键安全删除：注入权限过滤条件，拦截越权删除，并执行安全级联
func (mod *Model) SecureDeleteByID(ctx context.Context, global map[string]interface{}, id interface{}, options ...DeleteOptions) error {
	if mod.Protected {
		return fmt.Errorf("%w: model [%s] is protected from deletion", ErrModelProtected, mod.ID)
	}

	var opt DeleteOptions
	if len(options) > 0 {
		opt = options[0]
	}

	// 1. 若开启了级联删除或外键维护，先执行安全级联处理
	if err := mod.ExecuteSafeCascadeDeleteBatch(ctx, id, opt, 1); err != nil {
		return err
	}

	if isMutationBypassed(global) {
		if opt.HardDelete {
			return mod.Destroy(id, ctx)
		}
		return mod.Delete(id, ctx)
	}

	sec, writePlan, spec, err := mod.resolveMutationPlan(ctx, global)
	if err != nil {
		return err
	}
	if !spec.Enabled {
		if opt.HardDelete {
			return mod.Destroy(id, ctx)
		}
		return mod.Delete(id, ctx)
	}

	// 只读保护拦截
	if writePlan.IsReadOnlyBlocked {
		return ErrDataScopeReadOnlyBlocked
	}

	param := QueryParam{
		Model:   mod.ID,
		Context: ctx,
		Wheres: []QueryWhere{
			{Method: "where", Column: mod.PrimaryKey, Value: id},
		},
	}

	if err := mod.injectMutationWhere(&param, sec, writePlan, spec); err != nil {
		return err
	}

	var affected int
	if opt.HardDelete {
		affected, err = mod.DestroyWhere(param)
	} else {
		affected, err = mod.DeleteWhere(param)
	}
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrDataScopeAccessDenied
	}
	return nil
}

// SecureDeleteWhere 批量条件安全删除：注入行级权限与安全级联
func (mod *Model) SecureDeleteWhere(ctx context.Context, global map[string]interface{}, param QueryParam, options ...DeleteOptions) (int, error) {
	if mod.Protected {
		return 0, fmt.Errorf("%w: model [%s] is protected from deletion", ErrModelProtected, mod.ID)
	}

	var opt DeleteOptions
	if len(options) > 0 {
		opt = options[0]
	}

	if isMutationBypassed(global) {
		if opt.HardDelete {
			return mod.DestroyWhere(param)
		}
		return mod.DeleteWhere(param)
	}

	sec, writePlan, spec, err := mod.resolveMutationPlan(ctx, global)
	if err != nil {
		return 0, err
	}

	if spec.Enabled {
		if writePlan.IsReadOnlyBlocked {
			return 0, ErrDataScopeReadOnlyBlocked
		}
		if err := mod.injectMutationWhere(&param, sec, writePlan, spec); err != nil {
			return 0, err
		}
	}

	// 级联处理
	if opt.Cascade != nil || opt.ClearForeignKeys {
		pkCol := mod.PrimaryKey
		if pkCol == "" {
			pkCol = "id"
		}
		pluckParam := QueryParam{
			Model:   mod.ID,
			Context: ctx,
			Select:  []interface{}{pkCol},
			Wheres:  param.Wheres,
		}
		rows, err := mod.Get(pluckParam)
		if err == nil && len(rows) > 0 {
			ids := make([]interface{}, 0, len(rows))
			for _, r := range rows {
				if v, ok := r[pkCol]; ok && v != nil {
					ids = append(ids, v)
				}
			}
			if len(ids) > 0 {
				if err := mod.ExecuteSafeCascadeDeleteBatch(ctx, ids, opt, 1); err != nil {
					return 0, err
				}
			}
		}
	}

	if opt.HardDelete {
		return mod.DestroyWhere(param)
	}
	return mod.DeleteWhere(param)
}

// sanitizeMutationPayload 净化写操作数据负载，防篡改与越权分配 (Before-Insert Guard & Update Defense)
func (mod *Model) sanitizeMutationPayload(
	sec *SecurityContext,
	writePlan ReducedMutationPlan,
	spec ModelDataScopeSpec,
	payload maps.MapStrAny,
	isCreate bool,
) error {
	if writePlan.EffectiveWriteScope == ScopeTypeAll {
		return nil
	}

	if isCreate {
		// 新增时强制注入当前操作者的机构与租户标识
		if spec.TenantField != "" && sec != nil && sec.TenantID > 0 {
			payload[spec.TenantField] = sec.TenantID
		}
		if spec.OrgField != "" && sec != nil && sec.OrgID > 0 {
			payload[spec.OrgField] = sec.OrgID
		}
		if mod.Columns != nil {
			if _, hasCampus := mod.Columns["campus_id"]; hasCampus && sec != nil && sec.ActiveCampusID > 0 {
				payload["campus_id"] = sec.ActiveCampusID
			}
		}

		// 个人级权限：强制覆写真实 DoctorID，杜绝伪造他人工号挂单
		if writePlan.EffectiveWriteScope == ScopeTypeSelf {
			if mod.Columns != nil {
				if _, hasDoctorCol := mod.Columns["doctor_id"]; hasDoctorCol && sec != nil && sec.DoctorID > 0 {
					payload["doctor_id"] = sec.DoctorID
				}
			}
			if spec.UserField != "" && sec != nil && sec.UserID > 0 {
				payload[spec.UserField] = sec.UserID
			}
			if spec.DeptField != "" && sec != nil && sec.ActiveDeptID > 0 {
				payload[spec.DeptField] = sec.ActiveDeptID
			}
		}
	} else {
		// 更新时严禁篡改归属机构与租户
		if spec.TenantField != "" {
			delete(payload, spec.TenantField)
		}
		if spec.OrgField != "" {
			delete(payload, spec.OrgField)
		}

		// 个人级权限：严禁篡改 doctor_id 或 user_id
		if writePlan.EffectiveWriteScope == ScopeTypeSelf {
			if mod.Columns != nil {
				if _, hasDoctorCol := mod.Columns["doctor_id"]; hasDoctorCol && sec != nil && sec.DoctorID > 0 {
					payload["doctor_id"] = sec.DoctorID
				}
			}
			if spec.UserField != "" && sec != nil && sec.UserID > 0 {
				payload[spec.UserField] = sec.UserID
			}
		}
	}

	// 校验目标科室管辖权 (非从表且非全院写)
	if spec.DeptField != "" && spec.TableRole != TableRoleSlave {
		if rawDept, ok := payload[spec.DeptField]; ok && rawDept != nil {
			deptID, ok := toInt64(rawDept)
			if ok && deptID > 0 && !isWriteDeptAllowed(deptID, writePlan, sec) {
				return fmt.Errorf("%w: target department [%d] not authorized", ErrDataScopeDeptMismatch, deptID)
			}
		}
	}
	return nil
}

func isWriteDeptAllowed(deptID int64, plan ReducedMutationPlan, sec *SecurityContext) bool {
	if plan.EffectiveWriteScope.GreaterOrEqual(ScopeTypeOrg) || plan.EffectiveWriteScope == ScopeTypeAll {
		return true
	}
	if sec != nil && (deptID == sec.DeptID || deptID == sec.ActiveDeptID) {
		return true
	}
	for _, id := range plan.AllowedWriteDeptIDs {
		if id == deptID {
			return true
		}
	}
	return false
}
