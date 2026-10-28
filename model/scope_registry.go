package model

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/yaoapp/kun/log"
	"github.com/yaoapp/xun/capsule"
)

// DataScopeRegistry 全局数据库动态数据权限规格注册表 (Key: TableName / ModelID -> Value: ModelDataScopeSpec)
var DataScopeRegistry sync.Map

// ReloadDataScopes 从 sys_datascopes_access 表全量拉取并原子刷新内存注册表
func ReloadDataScopes() error {
	if capsule.Global == nil {
		return nil
	}

	sch := capsule.Schema()
	hasTable, err := sch.HasTable("sys_datascopes_access")
	if err != nil || !hasTable {
		log.Warn("[DataScope] table sys_datascopes_access does not exist or schema check failed, skipping dynamic registry load")
		return nil
	}

	rows, err := capsule.Query().
		Table("sys_datascopes_access").
		WhereNull("deleted_at").
		Select("table", "scope_type", "condition_value").
		Get()
	if err != nil {
		return err
	}

	newMap := make(map[string]ModelDataScopeSpec)
	for _, row := range rows {
		tableNameRaw, exists := row["table"]
		if !exists || tableNameRaw == nil {
			continue
		}

		tableName, ok := tableNameRaw.(string)
		if !ok || strings.TrimSpace(tableName) == "" {
			continue
		}
		tableName = strings.TrimSpace(tableName)

		spec := parseScopeSpecFromJSON(row["condition_value"])
		newMap[tableName] = spec
		// 同时记录小写映射以支持不区分大小写匹配
		lowerName := strings.ToLower(tableName)
		if lowerName != tableName {
			newMap[lowerName] = spec
		}
	}

	// 原子刷新：先清理无效项，再存入最新规格
	DataScopeRegistry.Range(func(key, value interface{}) bool {
		DataScopeRegistry.Delete(key)
		return true
	})

	for k, v := range newMap {
		DataScopeRegistry.Store(k, v)
	}

	log.Info("[DataScope] successfully reloaded %d data scope specs from sys_datascopes_access", len(newMap))
	return nil
}

// ClearDataScopeRegistry 清空内存注册表 (供测试及重置使用)
func ClearDataScopeRegistry() {
	DataScopeRegistry.Range(func(key, value interface{}) bool {
		DataScopeRegistry.Delete(key)
		return true
	})
}

// RegisterDataScopeSpec 手动向动态注册表注册表级安全规格 (供扩展及测试使用)
func RegisterDataScopeSpec(key string, spec ModelDataScopeSpec) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	DataScopeRegistry.Store(key, normalizeSpecDefaults(spec))
	lower := strings.ToLower(key)
	if lower != key {
		DataScopeRegistry.Store(lower, normalizeSpecDefaults(spec))
	}
}

// parseScopeSpecFromJSON 解析 condition_value JSON 对象
func parseScopeSpecFromJSON(raw interface{}) ModelDataScopeSpec {
	spec := ModelDataScopeSpec{
		Enabled:     true,
		Mode:        "enforcing",
		TenantField: "tenant_id",
		OrgField:    "org_id",
		DeptField:   "dept_id",
		UserField:   "created_by",
	}

	if raw == nil {
		return spec
	}

	var bytes []byte
	switch v := raw.(type) {
	case string:
		bytes = []byte(strings.TrimSpace(v))
	case []byte:
		bytes = v
	default:
		b, err := json.Marshal(raw)
		if err == nil {
			bytes = b
		}
	}

	if len(bytes) == 0 || string(bytes) == "null" || string(bytes) == "{}" {
		return spec
	}

	var parsed struct {
		Enabled     *bool  `json:"enabled"`
		Mode        string `json:"mode"`
		TenantField string `json:"tenant_field"`
		OrgField    string `json:"org_field"`
		DeptField   string `json:"dept_field"`
		UserField   string `json:"user_field"`
	}

	if err := json.Unmarshal(bytes, &parsed); err == nil {
		if parsed.Enabled != nil {
			spec.Enabled = *parsed.Enabled
		}
		if parsed.Mode != "" {
			spec.Mode = strings.TrimSpace(parsed.Mode)
		}
		if parsed.TenantField != "" {
			spec.TenantField = strings.TrimSpace(parsed.TenantField)
		}
		if parsed.OrgField != "" {
			spec.OrgField = strings.TrimSpace(parsed.OrgField)
		}
		if parsed.DeptField != "" {
			spec.DeptField = strings.TrimSpace(parsed.DeptField)
		}
		if parsed.UserField != "" {
			spec.UserField = strings.TrimSpace(parsed.UserField)
		}
	}

	return normalizeSpecDefaults(spec)
}
