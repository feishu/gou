package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/yaoapp/kun/maps"
)

// ExecuteSafeCascadeDeleteBatch 集合化批量级联删除与防误删护栏引擎
// 支持级联深度控制 (<= MaxCascadeDepth)、模型保护屏蔽 (ErrModelProtected)、
// 反向级联修正 (严禁 cascade: false 误删)、外键置空 (ClearForeignKeys) 与批量删除优化
func (mod *Model) ExecuteSafeCascadeDeleteBatch(
	ctx context.Context,
	parentID interface{},
	opts DeleteOptions,
	depth int,
) error {
	if mod == nil {
		return nil
	}

	// 1. 模型保护屏障 (Protected Barrier)
	if mod.Protected {
		return fmt.Errorf("%w: model [%s] is protected from deletion", ErrModelProtected, mod.ID)
	}

	// 2. 级联深度屏障，防止循环引用导致栈溢出
	if depth > MaxCascadeDepth {
		return fmt.Errorf("%w: cascade delete depth %d exceeded max allowed depth %d", ErrCascadeDepthExceeded, depth, MaxCascadeDepth)
	}

	parentIDs := normalizeIDs(parentID)
	if len(parentIDs) == 0 {
		return nil
	}

	if len(mod.MetaData.Relations) == 0 {
		return nil
	}

	for relName, rel := range mod.MetaData.Relations {
		// 仅对 hasOne 和 hasMany 关系处理级联下发
		relType := strings.ToLower(rel.Type)
		if relType != "hasmany" && relType != "hasone" {
			continue
		}

		if rel.Model == "" || rel.Foreign == "" {
			continue
		}

		shouldCascade := shouldCascadeRelation(relName, rel, opts)

		childMod := Select(rel.Model)
		if childMod == nil {
			return fmt.Errorf("cascade delete failed: relation model [%s] for relation [%s] not found", rel.Model, relName)
		}

		if shouldCascade {
			// 子模型保护校验
			if childMod.Protected {
				return fmt.Errorf("%w: cannot cascade delete protected child model [%s] via relation [%s]", ErrModelProtected, childMod.ID, relName)
			}

			// 查询关联的子记录主键列表
			childPK := childMod.PrimaryKey
			if childPK == "" {
				childPK = "id"
			}

			qb := childMod.Query(ctx).Table(childMod.MetaData.Table.Name).Select(childPK)
			if len(parentIDs) == 1 {
				qb = qb.Where(rel.Foreign, parentIDs[0])
			} else {
				qb = qb.WhereIn(rel.Foreign, parentIDs)
			}

			rows, err := qb.Get()
			if err != nil {
				return fmt.Errorf("failed to query child records for relation [%s]: %w", relName, err)
			}

			if len(rows) == 0 {
				continue
			}

			childIDs := make([]interface{}, 0, len(rows))
			for _, row := range rows {
				if val, ok := row[childPK]; ok && val != nil {
					childIDs = append(childIDs, val)
					continue
				}
				for k, val := range row {
					if strings.EqualFold(k, childPK) && val != nil {
						childIDs = append(childIDs, val)
						break
					}
				}
			}

			if len(childIDs) > 0 {
				// 递归级联孙表
				err = childMod.ExecuteSafeCascadeDeleteBatch(ctx, childIDs, opts, depth+1)
				if err != nil {
					return err
				}

				// 批量删除子表记录
				deleteParam := QueryParam{
					Model:   childMod.ID,
					Context: ctx,
					Wheres: []QueryWhere{
						{Method: "wherein", Column: childPK, Value: childIDs},
					},
				}

				if opts.HardDelete {
					_, err = childMod.DestroyWhere(deleteParam)
				} else {
					_, err = childMod.DeleteWhere(deleteParam)
				}
				if err != nil {
					return fmt.Errorf("failed to batch delete child records for relation [%s]: %w", relName, err)
				}
			}
		} else if opts.ClearForeignKeys {
			// 不级联但要求将子表外键置空 (Set NULL)
			qb := childMod.Query(ctx).Table(childMod.MetaData.Table.Name)
			if len(parentIDs) == 1 {
				qb = qb.Where(rel.Foreign, parentIDs[0])
			} else {
				qb = qb.WhereIn(rel.Foreign, parentIDs)
			}
			_, err := qb.Update(maps.MapStrAny{rel.Foreign: nil})
			if err != nil {
				return fmt.Errorf("failed to clear foreign key [%s] for relation [%s]: %w", rel.Foreign, relName, err)
			}
		}
	}

	return nil
}

// shouldCascadeRelation 判断特定关联是否允许执行级联删除
func shouldCascadeRelation(relName string, rel Relation, opts DeleteOptions) bool {
	// 动态选项优先覆盖
	if opts.Cascade != nil {
		switch v := opts.Cascade.(type) {
		case bool:
			if !v {
				// 明确禁止级联：严禁误删！
				return false
			}
			// 动态显式指定 true 时，仍需遵循 DSL 声明（若 DSL 未显式开启级联，则不级联）
			return rel.Cascade
		case []string:
			for _, item := range v {
				if item == relName {
					return true
				}
			}
			return false
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok && str == relName {
					return true
				}
			}
			return false
		}
	}

	// 默认依据 DSL 静态声明
	return rel.Cascade
}

// normalizeIDs 将单 ID 或 ID 切片统一转换为 []interface{}
func normalizeIDs(id interface{}) []interface{} {
	if id == nil {
		return nil
	}

	switch v := id.(type) {
	case []interface{}:
		return v
	case []int:
		res := make([]interface{}, len(v))
		for i, val := range v {
			res[i] = val
		}
		return res
	case []int64:
		res := make([]interface{}, len(v))
		for i, val := range v {
			res[i] = val
		}
		return res
	case []string:
		res := make([]interface{}, len(v))
		for i, val := range v {
			res[i] = val
		}
		return res
	default:
		return []interface{}{id}
	}
}
