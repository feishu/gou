package model

import (
	"fmt"
	"strings"
	"sync"

	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/types"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun/capsule"
)

// Models 已载入模型
var Models = map[string]*Model{}
var rwlock sync.RWMutex // Use RWMutex for better concurrency

// LoadSync load model sync
func LoadSync(file string, id string) (*Model, error) {
	return Load(file, id)
}

// LoadSourceSync load model sync
func LoadSourceSync(source []byte, id string, file string) (*Model, error) {
	return LoadSource(source, id, "")
}

// Load load model
func Load(file string, id string) (*Model, error) {
	data, err := application.App.Read(file)
	if err != nil {
		return nil, err
	}
	return LoadSource(data, id, file)
}

// LoadSource load model from source
func LoadSource(source []byte, id string, file string) (*Model, error) {
	if file == "" {
		file = fmt.Sprintf("__source.%s.mod.yao", id)
	}

	metadata := MetaData{}
	err := application.Parse(file, source, &metadata)
	if err != nil {
		exception.Err(err, 400).Throw()
	}

	mod := &Model{
		ID:        id,
		Name:      id,
		File:      file,
		MetaData:  metadata,
		source:    source,
		Protected: metadata.Protected,
		DataScope: metadata.DataScope,
	}

	// Parse common values
	columns := map[string]*Column{}
	columnNames := []interface{}{}
	PrimaryKey := "id"
	uniqueColumns := []*Column{}

	// Add timestamp columns if enabled
	if mod.MetaData.Option.Timestamps {
		mod.MetaData.Columns = append(mod.MetaData.Columns,
			Column{
				Label:    "::Created At",
				Name:     "created_at",
				Type:     "timestamp",
				Comment:  "::Created At",
				Nullable: true,
			},
			Column{
				Label:    "::Updated At",
				Name:     "updated_at",
				Type:     "timestamp",
				Comment:  "::Updated At",
				Nullable: true,
			},
		)
	}

	// Add soft delete column if enabled
	if mod.MetaData.Option.SoftDeletes {
		mod.MetaData.Columns = append(mod.MetaData.Columns, Column{
			Label:    "::Delete At",
			Name:     "deleted_at",
			Type:     "timestamp",
			Comment:  "::Delete At",
			Nullable: true,
		})
	}

	// Add permission columns if enabled
	if mod.MetaData.Option.Permission {
		mod.MetaData.Columns = append(mod.MetaData.Columns,
			Column{
				Label:    "::Created By",
				Name:     "__yao_created_by",
				Type:     "string",
				Comment:  "::Created By User ID",
				Nullable: true,
				Length:   128,
				Index:    true,
			},
			Column{
				Label:    "::Updated By",
				Name:     "__yao_updated_by",
				Type:     "string",
				Comment:  "::Updated By User ID",
				Nullable: true,
				Length:   128,
			},
			Column{
				Label:    "::Team ID",
				Name:     "__yao_team_id",
				Type:     "string",
				Comment:  "::Team ID",
				Nullable: true,
				Length:   128,
				Index:    true,
			},
			Column{
				Label:    "::Tenant ID",
				Name:     "__yao_tenant_id",
				Type:     "string",
				Comment:  "::Tenant ID",
				Nullable: true,
				Length:   128,
				Index:    true,
			},
		)
	}

	for i, column := range mod.MetaData.Columns {
		mod.MetaData.Columns[i].model = mod
		columns[column.Name] = &mod.MetaData.Columns[i]
		columnNames = append(columnNames, column.Name)
		if strings.ToLower(column.Type) == "id" || column.Primary == true {
			PrimaryKey = column.Name
		}

		if column.Unique || column.Primary {
			uniqueColumns = append(uniqueColumns, columns[column.Name])
		}
	}

	// Process unique indexes (avoid duplicates)
	uniqueColumnMap := map[string]*Column{}
	for _, col := range uniqueColumns {
		uniqueColumnMap[col.Name] = col
	}

	for _, index := range mod.MetaData.Indexes {
		if strings.ToLower(index.Type) == "unique" {
			for _, name := range index.Columns {
				col, has := columns[name]
				if has {
					uniqueColumnMap[col.Name] = col
				}
			}
		} else if strings.ToLower(index.Type) == "primary" {
			for _, name := range index.Columns {
				col, has := columns[name]
				if has {
					PrimaryKey = col.Name
					uniqueColumnMap[col.Name] = col
				}
			}
		}
	}

	// Convert map back to slice
	uniqueColumns = []*Column{}
	for _, col := range uniqueColumnMap {
		uniqueColumns = append(uniqueColumns, col)
	}

	mod.Columns = columns
	mod.ColumnNames = columnNames
	mod.PrimaryKey = PrimaryKey
	mod.UniqueColumns = uniqueColumns

	if capsule.Global != nil {
		if primary, err := capsule.Global.Primary(); err == nil && primary.Config != nil {
			mod.Driver = primary.Config.Driver
		}
	}

	SetModel(id, mod)
	return mod, nil
}

// Reload updates the model
func (mod *Model) Reload() (*Model, error) {
	// Load new model first
	new, err := Load(mod.File, mod.ID)
	if err != nil {
		return nil, err
	}

	*mod = *new
	SetModel(mod.ID, mod)
	return mod, nil
}

// Migrate 数据迁移
func (mod *Model) Migrate(force bool, opts ...MigrateOption) error {
	options := &MigrateOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if force {
		err := mod.DropTable()
		if err != nil {
			return err
		}
	}

	has, err := mod.HasTable()
	if err != nil {
		return err
	}

	if !has {
		err := mod.CreateTable()
		if err != nil {
			return err
		}

		if !options.DonotInsertValues {
			_, errs := mod.InsertValues()
			if len(errs) > 0 {
				for _, err := range errs {
					log.Error("[Migrate] %s", err.Error())
				}
				return fmt.Errorf("%d values error, please check the logs", len(errs))
			}
		}
		return nil
	}

	return mod.SaveTable()
}

// MigrateOptions Migrate options
type MigrateOptions struct {
	DonotInsertValues bool `json:"donot_insert_values"`
}

// MigrateOption Migrate option
type MigrateOption func(*MigrateOptions)

// WithDonotInsertValues with donot insert values
func WithDonotInsertValues(v bool) MigrateOption {
	return func(mo *MigrateOptions) {
		mo.DonotInsertValues = v
	}
}

// Select selects a model (100% 纯无锁原子读)
func Select(id string) *Model {
	mod, has := SelectFast(id)
	if !has {
		exception.New(
			fmt.Sprintf("Model:%s; not found", id),
			400,
		).Throw()
	}
	return mod
}

// Exists checks if model exists (100% 纯无锁原子读)
func Exists(id string) bool {
	_, has := SelectFast(id)
	return has
}

// GetMetaData gets model metadata
func GetMetaData(id string) MetaData {
	return Select(id).MetaData
}

// Read reads model source
func Read(id string) []byte {
	return Select(id).source
}

// Validate 数值校验
func (mod *Model) Validate(row maps.MapStrAny) []ValidateResponse {
	res := []ValidateResponse{}
	for name, value := range row {
		column, has := mod.Columns[name]
		if !has {
			continue
		}

		// 如果允许为 null
		if value == nil && column.Nullable {
			continue
		}

		success, messages := column.Validate(value, row)
		if !success {
			res = append(res, ValidateResponse{
				Column:   column.Name,
				Messages: messages,
			})
		}
	}
	return res
}

// GetMetaInfo returns the meta information of the model
func (mod *Model) GetMetaInfo() types.MetaInfo {
	return mod.MetaData.MetaInfo
}

// GetDataScopeSpec 获取并规范化模型的数据权限规格配置 (遵循三级回退梯度)
func (mod *Model) GetDataScopeSpec() ModelDataScopeSpec {
	if mod == nil {
		return ModelDataScopeSpec{}
	}

	// Level 1: 优先从 sys_datascopes_access 数据库注册表中动态获取 (按 Table.Name 与 ModelID 双键查找)
	if mod.MetaData.Table.Name != "" {
		if val, ok := DataScopeRegistry.Load(mod.MetaData.Table.Name); ok {
			if spec, ok := val.(ModelDataScopeSpec); ok {
				return normalizeSpecDefaults(spec)
			}
		}
		if val, ok := DataScopeRegistry.Load(strings.ToLower(mod.MetaData.Table.Name)); ok {
			if spec, ok := val.(ModelDataScopeSpec); ok {
				return normalizeSpecDefaults(spec)
			}
		}
	}
	if mod.ID != "" {
		if val, ok := DataScopeRegistry.Load(mod.ID); ok {
			if spec, ok := val.(ModelDataScopeSpec); ok {
				return normalizeSpecDefaults(spec)
			}
		}
		if val, ok := DataScopeRegistry.Load(strings.ToLower(mod.ID)); ok {
			if spec, ok := val.(ModelDataScopeSpec); ok {
				return normalizeSpecDefaults(spec)
			}
		}
	}

	// Level 2: 回退到 mod.yao DSL 静态配置
	spec := mod.DataScope
	if !spec.Enabled && mod.MetaData.DataScope.Enabled {
		spec = mod.MetaData.DataScope
	}
	if spec.Enabled {
		return normalizeSpecDefaults(spec)
	}

	// Level 3: 约定探测兜底（若字段中同时包含 org_id/oid 与 dept_id/department_id）
	return detectConventionSpec(mod)
}

// normalizeSpecDefaults 规范化默认字段名与阻断模式
func normalizeSpecDefaults(spec ModelDataScopeSpec) ModelDataScopeSpec {
	if spec.Mode == "" {
		spec.Mode = "enforcing"
	}
	if spec.TenantField == "" {
		spec.TenantField = "tenant_id"
	}
	if spec.OrgField == "" {
		spec.OrgField = "org_id"
	}
	if spec.DeptField == "" {
		spec.DeptField = "dept_id"
	}
	if spec.UserField == "" {
		spec.UserField = "created_by"
	}
	return spec
}

// detectConventionSpec 启发式探测模型字段约定的数据权限规格
func detectConventionSpec(mod *Model) ModelDataScopeSpec {
	spec := ModelDataScopeSpec{
		Enabled:     false,
		Mode:        "enforcing",
		TenantField: "tenant_id",
		OrgField:    "org_id",
		DeptField:   "dept_id",
		UserField:   "created_by",
	}

	if mod == nil || len(mod.Columns) == 0 {
		return spec
	}

	hasOrg := false
	hasDept := false

	for colName := range mod.Columns {
		lower := strings.ToLower(colName)
		switch lower {
		case "org_id":
			spec.OrgField = colName
			hasOrg = true
		case "oid":
			if spec.OrgField == "org_id" && !hasOrg {
				spec.OrgField = colName
			}
			hasOrg = true
		case "dept_id":
			spec.DeptField = colName
			hasDept = true
		case "department_id":
			if spec.DeptField == "dept_id" && !hasDept {
				spec.DeptField = colName
			}
			hasDept = true
		case "tenant_id":
			spec.TenantField = colName
		case "tid":
			spec.TenantField = colName
		case "created_by", "creator":
			spec.UserField = colName
		}
	}

	// 仅当明确同时存在机构/组织字段以及科室/部门字段时，才作为约定兜底启用
	if hasOrg && hasDept {
		spec.Enabled = true
	}

	return spec
}
