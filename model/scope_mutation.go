package model

import (
	"context"
	"fmt"
	"os"

	"github.com/yaoapp/kun/maps"
)

// SecureUpdateByID 单点主键安全更新：强制数据归属防篡改、科室辖区核验，并免疫 IDOR 越权
func (mod *Model) SecureUpdateByID(ctx context.Context, global map[string]interface{}, id interface{}, payload maps.MapStrAny) error {
	if os.Getenv("YAO_DATASCOPE_GLOBAL_DISABLE") == "true" {
		return mod.Update(id, payload, ctx)
	}

	spec := mod.GetDataScopeSpec()
	if !spec.Enabled {
		return mod.Update(id, payload, ctx)
	}

	if err := mod.sanitizeWritePayload(ctx, global, spec, payload, false); err != nil {
		return err
	}

	param := QueryParam{
		Model:   mod.ID,
		Context: ctx,
		Wheres: []QueryWhere{
			{Method: "where", Column: mod.PrimaryKey, Value: id},
		},
	}

	if err := mod.BeforeQueryDataScope(ctx, global, &param); err != nil {
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

// SecureCreate 单条记录安全创建：自动填充租户/机构归属，校验科室管辖权
func (mod *Model) SecureCreate(ctx context.Context, global map[string]interface{}, row maps.MapStrAny) (interface{}, error) {
	if os.Getenv("YAO_DATASCOPE_GLOBAL_DISABLE") == "true" {
		return mod.Create(row, ctx)
	}

	spec := mod.GetDataScopeSpec()
	if !spec.Enabled {
		return mod.Create(row, ctx)
	}

	if err := mod.sanitizeWritePayload(ctx, global, spec, row, true); err != nil {
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

	if os.Getenv("YAO_DATASCOPE_GLOBAL_DISABLE") == "true" {
		if opt.HardDelete {
			return mod.Destroy(id, ctx)
		}
		return mod.Delete(id, ctx)
	}

	spec := mod.GetDataScopeSpec()
	if !spec.Enabled {
		if opt.HardDelete {
			return mod.Destroy(id, ctx)
		}
		return mod.Delete(id, ctx)
	}

	param := QueryParam{
		Model:   mod.ID,
		Context: ctx,
		Wheres: []QueryWhere{
			{Method: "where", Column: mod.PrimaryKey, Value: id},
		},
	}

	if err := mod.BeforeQueryDataScope(ctx, global, &param); err != nil {
		return err
	}

	var (
		affected int
		err      error
	)
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

	if os.Getenv("YAO_DATASCOPE_GLOBAL_DISABLE") == "true" {
		if opt.HardDelete {
			return mod.DestroyWhere(param)
		}
		return mod.DeleteWhere(param)
	}

	spec := mod.GetDataScopeSpec()
	if spec.Enabled {
		if err := mod.BeforeQueryDataScope(ctx, global, &param); err != nil {
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

// sanitizeWritePayload 净化写操作数据负载，防篡改与越权分配
func (mod *Model) sanitizeWritePayload(
	ctx context.Context,
	global map[string]interface{},
	spec ModelDataScopeSpec,
	payload maps.MapStrAny,
	isCreate bool,
) error {
	sec, bindings := ResolveSecurityContext(ctx, global)
	plan := ReduceRoleScopes(sec, bindings)
	if plan.IsUnrestricted {
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
	} else {
		// 更新时严禁篡改归属机构与租户
		if spec.TenantField != "" {
			delete(payload, spec.TenantField)
		}
		if spec.OrgField != "" {
			delete(payload, spec.OrgField)
		}
	}

	// 校验目标科室管辖权
	if spec.DeptField != "" {
		if rawDept, ok := payload[spec.DeptField]; ok && rawDept != nil {
			deptID, ok := toInt64(rawDept)
			if ok && deptID > 0 && !isDeptAllowed(deptID, plan, sec) {
				return fmt.Errorf("security violation: target department [%d] not authorized", deptID)
			}
		}
	}
	return nil
}

func isDeptAllowed(deptID int64, plan ReducedScopePlan, sec *SecurityContext) bool {
	if plan.IsUnrestricted || plan.EnforceOrg {
		return true
	}
	if sec != nil && deptID == sec.DeptID {
		return true
	}
	for _, id := range plan.AllowedDeptIDs {
		if id == deptID {
			return true
		}
	}
	return false
}
