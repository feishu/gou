package model

import (
	"errors"
	"strings"
)

// ScopeType 数据范围类型枚举 (8级单调递增全序格，对齐 PRD V3.3)
type ScopeType int

const (
	// ScopeTypeDenied 显式禁止/故障闭锁 / 纯只读写拦截 (1=0)
	ScopeTypeDenied ScopeType = 0
	// ScopeTypeSelf 仅本人数据 (多态本人插槽 OR 动态白名单)
	ScopeTypeSelf ScopeType = 1
	// ScopeTypeMedicalGroup 本医疗组/诊疗组数据
	ScopeTypeMedicalGroup ScopeType = 15
	// ScopeTypeUnit 本临床科室 或 本护理病区 (依 anchor_axis 路由)
	ScopeTypeUnit ScopeType = 2
	// ScopeTypeDept 本科室数据 (向前兼容别名)
	ScopeTypeDept ScopeType = 2
	// ScopeTypeUnitSub 本科室及下属科室数据 (依 anchor_axis 路由)
	ScopeTypeUnitSub ScopeType = 3
	// ScopeTypeDeptSub 本科室及下属科室数据 (向前兼容别名)
	ScopeTypeDeptSub ScopeType = 3
	// ScopeTypeMultiUnit 指定多个科室 或 多个病区列表 (参数化实参)
	ScopeTypeMultiUnit ScopeType = 31
	// ScopeTypeCampus 当前执业院区数据
	ScopeTypeCampus ScopeType = 4
	// ScopeTypeMultiCampus 指定多个院区列表 (参数化实参)
	ScopeTypeMultiCampus ScopeType = 41
	// ScopeTypeOrg 本机构全院数据 (由历史 4 升级为 6)
	ScopeTypeOrg ScopeType = 6
	// ScopeTypeTenant 本租户全域数据 (由历史 5 升级为 7)
	ScopeTypeTenant ScopeType = 7
	// ScopeTypeMultiTenant 指定多个租户列表 (参数化实参)
	ScopeTypeMultiTenant ScopeType = 71
	// ScopeTypeCustom 自定义规则匹配
	ScopeTypeCustom ScopeType = 8
	// ScopeTypeAll 全量放行(内部/超管)
	ScopeTypeAll ScopeType = 10
)

// NormalizeLegacyScopeType 存量历史配置平滑向前兼容转换
func NormalizeLegacyScopeType(legacy int) ScopeType {
	switch legacy {
	case 4: // 历史旧配置 4: 本机构全院
		return ScopeTypeOrg // 映射至新体系 Level 6
	case 5: // 历史旧配置 5: 本租户全域
		return ScopeTypeTenant // 映射至新体系 Level 7
	default:
		return ScopeType(legacy)
	}
}

// Rank 返回偏序格在 8 级全序链中的真实单调递增权重，消除 Level 15 / 31 / 71 的整数面值大小歧义
func (s ScopeType) Rank() int {
	switch s {
	case ScopeTypeDenied:
		return 0
	case ScopeTypeCustom: // 自定义规则按规则本身求值，不作为宏观层级吞噬
		return 5
	case ScopeTypeSelf:
		return 10
	case ScopeTypeMedicalGroup: // Level 15
		return 15
	case ScopeTypeUnit: // Level 2
		return 20
	case ScopeTypeUnitSub: // Level 3
		return 30
	case ScopeTypeMultiUnit: // Level 31
		return 35
	case ScopeTypeCampus: // Level 4
		return 40
	case ScopeTypeMultiCampus: // Level 41
		return 45
	case ScopeTypeOrg: // Level 6
		return 60
	case ScopeTypeTenant: // Level 7
		return 70
	case ScopeTypeMultiTenant: // Level 71
		return 75
	case ScopeTypeAll: // Level 10
		return 100
	default:
		return int(s)
	}
}

// GreaterThan 判断当前偏序等级是否严格大于 other
func (s ScopeType) GreaterThan(other ScopeType) bool {
	return s.Rank() > other.Rank()
}

// LessThan 判断当前偏序等级是否严格小于 other
func (s ScopeType) LessThan(other ScopeType) bool {
	return s.Rank() < other.Rank()
}

// GreaterOrEqual 判断当前偏序等级是否大于或等于 other
func (s ScopeType) GreaterOrEqual(other ScopeType) bool {
	return s.Rank() >= other.Rank()
}

// LessOrEqual 判断当前偏序等级是否小于或等于 other
func (s ScopeType) LessOrEqual(other ScopeType) bool {
	return s.Rank() <= other.Rank()
}

// AnchorAxis 空间归属锚点轴
type AnchorAxis string

const (
	AxisClinicalDept AnchorAxis = "clinical_dept" // 临床科室轴 (医生/医技默认)
	AxisNursingWard  AnchorAxis = "nursing_ward"  // 护理病区单元轴 (护士/护士长默认)
	AxisDualUnion    AnchorAxis = "dual_union"    // 医护双轴并集 (科主任/综合管理)
)

// TableRole 受控表在业务领域中的主从分级角色
type TableRole string

const (
	TableRoleMaster TableRole = "MASTER_ENTITY" // 核心主单据表 (全维受控)
	TableRoleSlave  TableRole = "DETAIL_SLAVE"  // 明细从表 (仅受租户/机构顶层边界守护)
)

// NormalizeTableRole 将输入字符串或配置规范化为合法的 TableRole
func NormalizeTableRole(val string) TableRole {
	switch strings.ToUpper(strings.TrimSpace(val)) {
	case "SLAVE", "DETAIL_SLAVE":
		return TableRoleSlave
	default:
		return TableRoleMaster
	}
}

// MutationPolicy 写操作变更保护策略
type MutationPolicy string

const (
	MutationStrictSelf  MutationPolicy = "strict_self"  // 强制仅本人可写 (Write = Level 1)
	MutationReadOnly    MutationPolicy = "read_only"    // 纯只读禁改 (Write = Level 0)
	MutationFollowRead  MutationPolicy = "follow_read"  // 跟随读范围 (Write = ReadScope)
	MutationCustomLevel MutationPolicy = "custom_level" // 自定义写偏序等级
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
	// ErrDataScopeReadOnlyBlocked 写操作命中纯只读保护拦截
	ErrDataScopeReadOnlyBlocked = errors.New("MUTATION_BLOCKED_READ_ONLY: current role mutation policy is read-only")
	// ErrDataScopeStrictSelfViolation 试图修改或伪造非本人名下单据
	ErrDataScopeStrictSelfViolation = errors.New("MUTATION_BLOCKED_STRICT_SELF: cannot mutate data owned by other doctors")
	// ErrDataScopeDeptMismatch 插入或更新单据科室不在管辖白名单内
	ErrDataScopeDeptMismatch = errors.New("DATA_SCOPE_DEPT_MISMATCH: target department is not in granted scope")
	// ErrBreakGlassLimitExceeded 当日急诊破窗频次已达到熔断上限
	ErrBreakGlassLimitExceeded = errors.New("BREAK_GLASS_RATE_LIMITED: daily emergency access threshold exceeded")
	// ErrDataScopeBlockedTargetExists 详情主键轻量探针检测到目标记录客观存在但被权限拦截
	ErrDataScopeBlockedTargetExists = errors.New("DATA_SCOPE_BLOCKED_TARGET_EXISTS: record exists but is protected by data scope boundary")
)

// SecurityContext 安全上下文（提取自 JWT/Session/Header/Global）
type SecurityContext struct {
	UserID            int64     `json:"user_id"`
	DoctorID          int64     `json:"doctor_id"`
	TenantID          int64     `json:"tenant_id"`
	OrgID             int64     `json:"org_id"`
	DeptID            int64     `json:"dept_id"`
	WardID            int64     `json:"ward_id"`
	CampusID          int64     `json:"campus_id"`
	GroupID           int64     `json:"group_id"`
	RoleIDs           []int64   `json:"role_ids"`
	DeptPathPrefix    string    `json:"dept_path_prefix"`
	SubDeptIDs        []int64   `json:"sub_dept_ids"`
	InternalCall      bool      `json:"internal_call"`
	HasAdminPrivilege bool      `json:"has_admin_privilege"`
	ActiveCampusID    int64     `json:"active_campus_id"`
	ActiveDeptID      int64     `json:"active_dept_id"`
	ActiveWardID      int64     `json:"active_ward_id"`
	ActiveGroupID     int64     `json:"active_group_id"`
	IntentScopeCap    ScopeType `json:"intent_scope_cap"`
	IsOutpatientNurse bool      `json:"is_outpatient_nurse"`
	AllowCrossWard    bool      `json:"allow_cross_ward"`
	AllowedCampusIDs  []int64   `json:"allowed_campus_ids"`
	AllowedWardIDs    []int64   `json:"allowed_ward_ids"`
	AllowedGroupIDs   []int64   `json:"allowed_group_ids"`
}

// RoleScopeBinding 角色与数据权限范围绑定 (向前兼容)
type RoleScopeBinding struct {
	RoleID    int64     `json:"role_id"`
	ScopeType ScopeType `json:"scope_type"`
	DeptIDs   []int64   `json:"dept_ids,omitempty"`
	OrgIDs    []int64   `json:"org_ids,omitempty"`
	RuleJSON  string    `json:"rule_json,omitempty"`
}

// RoleScopeTuple 角色读写二元组绑定规格
type RoleScopeTuple struct {
	RoleID              int64          `json:"role_id"`
	AnchorAxis          AnchorAxis     `json:"anchor_axis"`
	ReadScope           ScopeType      `json:"read_scope"`
	MutationPolicy      MutationPolicy `json:"mutation_policy"`
	CustomMutationScope ScopeType      `json:"custom_mutation_scope"`
	IdentitySlots       []string       `json:"identity_slots"`
	GrantedTenantIDs    []int64        `json:"granted_tenant_ids"`
	GrantedCampusIDs    []int64        `json:"granted_campus_ids"`
	GrantedDeptIDs      []int64        `json:"granted_dept_ids"`
	GrantedWardIDs      []int64        `json:"granted_ward_ids"`
}

// ReducedScopePlan 偏序格归约后的单次查询执行计划 (for SELECT)
type ReducedScopePlan struct {
	IsUnrestricted         bool        `json:"is_unrestricted"`          // 是否短路全量放行
	IsDenied               bool        `json:"is_denied"`                // 是否故障闭锁
	EffectiveReadScope     ScopeType   `json:"effective_read_scope,omitempty"`
	AnchorAxis             AnchorAxis  `json:"anchor_axis,omitempty"`
	EnforceTenant          bool        `json:"enforce_tenant"`           // 是否强制注入 TenantID
	EnforceOrg             bool        `json:"enforce_org"`              // 是否强制注入 OrgID (全院放行)
	IncludeSelf            bool        `json:"include_self"`             // 是否包含本人创建的数据
	AllowedTenantIDs       []int64     `json:"allowed_tenant_ids,omitempty"`
	AllowedCampusIDs       []int64     `json:"allowed_campus_ids,omitempty"`
	AllowedOrgIDs          []int64     `json:"allowed_org_ids,omitempty"`// 允许的机构列表
	AllowedDeptIDs         []int64     `json:"allowed_dept_ids,omitempty"`// 允许的科室列表
	AllowedWardIDs         []int64     `json:"allowed_ward_ids,omitempty"`
	AllowedGroupIDs        []int64     `json:"allowed_group_ids,omitempty"`
	RootAncestorDeptIDs    []int64     `json:"root_ancestor_dept_ids,omitempty"` // 根科室祖先节点
	HasCustomDiscreteDepts bool        `json:"has_custom_discrete_depts"`// 是否存在分散自定义科室
	IdentitySlots          []string    `json:"identity_slots,omitempty"`
	DynamicPatientIDs      []int64     `json:"dynamic_patient_ids,omitempty"`
	ProtectSensitive       bool        `json:"protect_sensitive,omitempty"`
}

// ReducedMutationPlan 归约后的写变更执行计划 (for UPDATE / DELETE / INSERT)
type ReducedMutationPlan struct {
	IsReadOnlyBlocked    bool        `json:"is_read_only_blocked"`
	EffectiveWriteScope  ScopeType   `json:"effective_write_scope"`
	StrictSelfDoctorID   int64       `json:"strict_self_doctor_id"`
	AllowedWriteDeptIDs  []int64     `json:"allowed_write_dept_ids,omitempty"`
	EnforceActiveContext bool        `json:"enforce_active_context"`
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
	Enabled     bool      `json:"enabled,omitempty"`      // 是否启用行级数据权限
	Mode        string    `json:"mode,omitempty"`         // enforcing (阻断) | shadow (影子观察)
	TableRole   TableRole `json:"table_role,omitempty"`   // 主从表角色 MASTER_ENTITY | DETAIL_SLAVE
	TenantField string    `json:"tenant_field,omitempty"` // 租户字段映射名，默认 tenant_id
	OrgField    string    `json:"org_field,omitempty"`    // 机构字段映射名，默认 org_id
	CampusField string    `json:"campus_field,omitempty"` // 院区字段映射名，如 branch_id/campus_id
	DeptField   string    `json:"dept_field,omitempty"`   // 科室字段映射名，默认 dept_id
	WardField   string    `json:"ward_field,omitempty"`   // 病区字段映射名，默认 ward_id
	GroupField  string    `json:"group_field,omitempty"`  // 医疗组字段映射名，默认 group_id
	UserField   string    `json:"user_field,omitempty"`   // 用户字段映射名，默认 created_by
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
