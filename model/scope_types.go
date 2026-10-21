package model

import (
	"errors"
)

// ScopeType 数据范围类型枚举 (7级标准偏序)
type ScopeType int

const (
	// ScopeTypeDenied 显式禁止/故障闭锁
	ScopeTypeDenied ScopeType = 0
	// ScopeTypeSelf 仅本人数据
	ScopeTypeSelf ScopeType = 1
	// ScopeTypeDept 本科室数据
	ScopeTypeDept ScopeType = 2
	// ScopeTypeDeptSub 本科室及下属科室数据
	ScopeTypeDeptSub ScopeType = 3
	// ScopeTypeOrg 本机构全院数据
	ScopeTypeOrg ScopeType = 4
	// ScopeTypeTenant 本租户全域数据
	ScopeTypeTenant ScopeType = 5
	// ScopeTypeCustom 自定义规则匹配
	ScopeTypeCustom ScopeType = 6
	// ScopeTypeAll 全量放行(内部/超管)
	ScopeTypeAll ScopeType = 10
)

var (
	// ErrMissingSecurityCtx 安全上下文缺失
	ErrMissingSecurityCtx = errors.New("missing or invalid security context")
	// ErrDataScopeAccessDenied 数据权限校验未通过
	ErrDataScopeAccessDenied = errors.New("data scope access denied")
	// ErrModelProtected 模型防误删保护阻断
	ErrModelProtected = errors.New("model is protected from deletion")
	// ErrCascadeDepthExceeded 级联深度超限或循环引用
	ErrCascadeDepthExceeded = errors.New("cascade delete depth limit exceeded")
)

// SecurityContext 安全上下文（提取自 JWT/Session/Header/Global）
type SecurityContext struct {
	UserID            int64   `json:"user_id"`
	TenantID          int64   `json:"tenant_id"`
	OrgID             int64   `json:"org_id"`
	DeptID            int64   `json:"dept_id"`
	RoleIDs           []int64 `json:"role_ids"`
	DeptPathPrefix    string  `json:"dept_path_prefix"`
	SubDeptIDs        []int64 `json:"sub_dept_ids"`
	InternalCall      bool    `json:"internal_call"`
	HasAdminPrivilege bool    `json:"has_admin_privilege"`
}

// RoleScopeBinding 角色与数据权限范围绑定
type RoleScopeBinding struct {
	RoleID    int64     `json:"role_id"`
	ScopeType ScopeType `json:"scope_type"`
	DeptIDs   []int64   `json:"dept_ids,omitempty"`
	OrgIDs    []int64   `json:"org_ids,omitempty"`
	RuleJSON  string    `json:"rule_json,omitempty"`
}

// ReducedScopePlan 偏序格归约后的单次查询执行计划
type ReducedScopePlan struct {
	IsUnrestricted         bool    `json:"is_unrestricted"`          // 是否短路全量放行
	IsDenied               bool    `json:"is_denied"`                // 是否故障闭锁
	EnforceTenant          bool    `json:"enforce_tenant"`           // 是否强制注入 TenantID
	EnforceOrg             bool    `json:"enforce_org"`              // 是否强制注入 OrgID (全院放行)
	IncludeSelf            bool    `json:"include_self"`             // 是否包含本人创建的数据
	AllowedOrgIDs          []int64 `json:"allowed_org_ids,omitempty"`// 允许的机构列表
	AllowedDeptIDs         []int64 `json:"allowed_dept_ids,omitempty"`// 允许的科室列表
	RootAncestorDeptIDs    []int64 `json:"root_ancestor_dept_ids,omitempty"` // 根科室祖先节点
	HasCustomDiscreteDepts bool    `json:"has_custom_discrete_depts"`// 是否存在分散自定义科室
}

const (
	// MaxCascadeDepth 最大级联删除深度（防循环引用与栈溢出）
	MaxCascadeDepth = 5
)

// DeleteOptions 删除操作扩展选项
type DeleteOptions struct {
	Cascade          interface{} `json:"cascade,omitempty"`            // bool 或 []string 或 []interface{}
	ClearForeignKeys bool        `json:"clear_foreign_keys,omitempty"` // 当不级联时是否将子表外键置空 (Set NULL)
	HardDelete       bool        `json:"hard_delete,omitempty"`        // 是否物理真删除
	Force            bool        `json:"force,omitempty"`              // 强制破盾标记
	Reason           string      `json:"reason,omitempty"`             // 破盾审计原因
}

// ModelDataScopeSpec 模型 DSL 中配置的 data_scope 元数据
type ModelDataScopeSpec struct {
	Enabled     bool   `json:"enabled,omitempty"`      // 是否启用行级数据权限
	Mode        string `json:"mode,omitempty"`         // enforcing (阻断) | shadow (影子观察)
	TenantField string `json:"tenant_field,omitempty"` // 租户字段映射名，默认 tenant_id
	OrgField    string `json:"org_field,omitempty"`    // 机构字段映射名，默认 org_id
	DeptField   string `json:"dept_field,omitempty"`   // 科室字段映射名，默认 dept_id
	UserField   string `json:"user_field,omitempty"`   // 用户字段映射名，默认 created_by
}

// ConditionValueSchema 自定义数据权限条件表达式模式
type ConditionValueSchema struct {
	Logic string          `json:"logic"` // "AND" | "OR"
	Rules []ConditionRule `json:"rules"`
}

// ConditionRule 自定义数据权限单个原子规则
type ConditionRule struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // eq, ne, in, not_in, gt, lt, ge, le, between
	Slot     string      `json:"slot"`     // __user_id, __org_id, __dept_id, __dept_ids, __tenant_id
	Value    interface{} `json:"value"`
}
