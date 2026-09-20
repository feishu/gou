package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yaoapp/kun/log"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun"
	"github.com/yaoapp/xun/dbal/query"
)

// QueryStack 查询栈
type QueryStack struct {
	Builders []QueryStackBuilder
	Params   []QueryStackParam
	Current  int
}

// QueryStackBuilder 查询构建器
type QueryStackBuilder struct {
	Model     *Model
	Query     query.Query
	ColumnMap map[string]ColumnMap
}

// QueryStackParam QueryStack 查询参数
type QueryStackParam struct {
	QueryParam   QueryParam
	Relation     Relation
	ExportPrefix string // 字段导出前缀
}

// MakeQueryStack 创建查询栈
func MakeQueryStack() *QueryStack {
	return &QueryStack{
		Builders: []QueryStackBuilder{},
		Params:   []QueryStackParam{},
		Current:  -1,
	}
}

// NewQueryStack 新建查询栈
func NewQueryStack(param QueryParam) *QueryStack {
	return param.Query(nil)
}

// Push 添加查询器
func (stack *QueryStack) Push(builder QueryStackBuilder, param QueryStackParam) {
	stack.Builders = append(stack.Builders, builder)
	stack.Params = append(stack.Params, param)
	stack.Current = len(stack.Builders) - 1
}

// Merge 合并 Stack
func (stack *QueryStack) Merge(new *QueryStack) {
	curr := stack.Current
	for i, builder := range new.Builders {
		stack.Builders = append(stack.Builders, builder)
		stack.Params = append(stack.Params, new.Params[i])
	}
	stack.Current = curr
}

// Len 查询器数量
func (stack *QueryStack) Len() int {
	return len(stack.Builders)
}

// Builder 返回当前查询构建器
func (stack *QueryStack) Builder() *QueryStackBuilder {
	if stack.Current < 0 {
		return nil
	}
	return &stack.Builders[stack.Current]
}

// Query 返回当前查询器
func (stack *QueryStack) Query() query.Query {
	if stack.Current < 0 {
		return nil
	}
	return stack.Builders[stack.Current].Query
}

// FirstQuery 返回第一个查询器
func (stack *QueryStack) FirstQuery() query.Query {
	if len(stack.Builders) == 0 {
		return nil
	}
	return stack.Builders[0].Query
}

// QueryParam 返回当前查询参数
func (stack *QueryStack) QueryParam() QueryParam {
	if stack.Current < 0 {
		return QueryParam{}
	}
	return stack.Params[stack.Current].QueryParam
}

// Relation 返回当前查询参数
func (stack *QueryStack) Relation() Relation {
	if stack.Current < 0 {
		return Relation{}
	}
	return stack.Params[stack.Current].Relation
}

// Next 返回下一个查询器
func (stack *QueryStack) Next() int {
	next := stack.Current + 1
	if next < stack.Len() {
		stack.Current = next
		return next
	}
	return -1
}

// Run 执行查询栈
func (stack *QueryStack) Run() []maps.MapStrAny {
	res := [][]maps.MapStrAny{}
	for i, qb := range stack.Builders {
		param := stack.Params[i]
		switch param.Relation.Type {
		case "hasMany":
			stack.runHasMany(&res, qb, param)
			break
		default:
			stack.run(&res, qb, param)
		}
	}

	if len(res) < 0 {
		return nil
	}
	return res[0]
}

// Paginate 执行查询栈(分页查询)
func (stack *QueryStack) Paginate(page int, pagesize int) maps.MapStrAny {
	res := [][]maps.MapStrAny{}
	var pageInfo xun.P
	for i, qb := range stack.Builders {
		param := stack.Params[i]
		if i == 0 {
			pageInfo = stack.paginate(page, pagesize, &res, qb, param)
			continue
		}
		switch param.Relation.Type {
		case "hasMany":
			stack.runHasMany(&res, qb, param)
			break
		default:
			stack.run(&res, qb, param)
		}
	}

	if len(res) < 0 {
		return nil
	}

	response := maps.MapStrAny{}
	response["data"] = res[0]
	response["pagesize"] = pageInfo.PageSize
	response["pagecnt"] = pageInfo.TotalPages
	response["pagesize"] = pageInfo.PageSize
	response["page"] = pageInfo.CurrentPage
	response["next"] = pageInfo.NextPage
	response["prev"] = pageInfo.PreviousPage
	response["total"] = pageInfo.Total
	return response
}

// Count 统计记录数
func (stack *QueryStack) Count() (int64, error) {
	if len(stack.Builders) == 0 {
		return 0, nil
	}

	builder := stack.Builders[0]
	count, err := builder.Query.Count()
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (stack *QueryStack) paginate(page int, pagesize int, res *[][]maps.MapStrAny, builder QueryStackBuilder, param QueryStackParam) xun.P {

	// Debug mode Log SQL
	if param.QueryParam.Debug {
		defer log.With(log.F{"page": page, "pagesize": pagesize, "bindings": builder.Query.GetBindings()}).Trace("%s", builder.Query.ToSQL())
	}

	pageRes := builder.Query.MustPaginateRecordSet(pagesize, page)
	fmtRows := formatRecordSet(pageRes.RecordSet, builder.ColumnMap)

	*res = append(*res, fmtRows)
	stack.Next()
	return xun.P{
		Total:        pageRes.Total,
		TotalPages:   pageRes.TotalPages,
		PageSize:     pageRes.PageSize,
		CurrentPage:  pageRes.CurrentPage,
		NextPage:     pageRes.NextPage,
		PreviousPage: pageRes.PreviousPage,
		LastPage:     pageRes.LastPage,
		Options:      pageRes.Options,
	}
}

// colExtractPlan 列提取静态执行计划
type colExtractPlan struct {
	targetKey string
	filterCol *Column
	hasDot    bool
}

// compileColPlan 在处理行数据前，一次性编译所有列的映射规则与属性，消除行级重复查表
func compileColPlan(columns []string, columnMap map[string]ColumnMap) ([]colExtractPlan, bool) {
	plan := make([]colExtractPlan, len(columns))
	anyDot := false
	for i, col := range columns {
		cmap, has := columnMap[col]
		if !has {
			cmap, has = columnMap[strings.ToLower(col)]
		}
		if has {
			hasDot := strings.ContainsRune(cmap.Export, '.')
			if hasDot {
				anyDot = true
			}
			colPtr := cmap.Column
			plan[i] = colExtractPlan{
				targetKey: cmap.Export,
				filterCol: colPtr,
				hasDot:    hasDot,
			}
		} else {
			hasDot := strings.ContainsRune(col, '.')
			if hasDot {
				anyDot = true
			}
			plan[i] = colExtractPlan{
				targetKey: col,
				filterCol: nil,
				hasDot:    hasDot,
			}
		}
	}
	return plan, anyDot
}

// formatRecordSet 将 xun.RecordSet 二维紧凑数据转换为 []maps.MapStr (零中间哈希桶构造)
func formatRecordSet(rs *xun.RecordSet, columnMap map[string]ColumnMap) []maps.MapStr {
	if rs == nil || len(rs.Rows) == 0 {
		return []maps.MapStr{}
	}
	colLen := len(rs.Columns)
	plan, anyDot := compileColPlan(rs.Columns, columnMap)

	fmtRows := make([]maps.MapStr, len(rs.Rows))
	for rowIdx, row := range rs.Rows {
		fmtRow := make(maps.MapStr, colLen)
		for i, cp := range plan {
			if i < len(row) {
				val := row[i]
				fmtRow[cp.targetKey] = val
				if cp.filterCol != nil {
					cp.filterCol.FliterOut(val, fmtRow, cp.targetKey)
				}
			}
		}
		if anyDot {
			fmtRows[rowIdx] = fmtRow.UnDot()
		} else {
			fmtRows[rowIdx] = fmtRow
		}
	}
	return fmtRows
}

func (stack *QueryStack) run(res *[][]maps.MapStrAny, builder QueryStackBuilder, param QueryStackParam) {

	// 默认单表安全保护：未指定时默认取 100 条防止全表扫描导致内存暴涨
	// 若显式传入 Limit < 0（如 -1），则表示不限条数
	limit := 100
	unlimited := false
	if param.QueryParam.Limit > 0 {
		limit = param.QueryParam.Limit
	} else if param.QueryParam.Limit < 0 {
		unlimited = true
	}

	if param.QueryParam.Debug {
		sqlStr := builder.Query.ToSQL()
		bindings := builder.Query.GetBindings()
		if !unlimited {
			sqlStr = builder.Query.Limit(limit).ToSQL()
			bindings = builder.Query.Limit(limit).GetBindings()
		}
		defer log.With(log.F{
			"sql":      sqlStr,
			"bindings": bindings}).
			Trace("QueryStack run()")
	}

	var rs *xun.RecordSet
	if unlimited {
		rs = builder.Query.MustGetRecordSet()
	} else {
		rs = builder.Query.Limit(limit).MustGetRecordSet()
	}

	fmtRows := formatRecordSet(rs, builder.ColumnMap)
	*res = append(*res, fmtRows)
	stack.Next()
}

func (stack *QueryStack) runHasMany(res *[][]maps.MapStrAny, builder QueryStackBuilder, param QueryStackParam) {

	if param.QueryParam.Debug {
		defer log.With(log.F{
			"sql":      builder.Query.ToSQL(),
			"bindings": builder.Query.GetBindings()}).
			Trace("QueryStack runHasMany()")
	}

	// 获取上次查询结果，拼接结果集ID
	rel := stack.Relation()
	foreignIDs := []interface{}{}
	if len(*res) == 0 {
		return
	}

	// 优先定位包含外键字段的上一层主表数据（默认根主表数据，避免多个同级 hasMany 相互污染）
	prevRows := (*res)[0]
	if len(*res) > 1 {
		lastRows := (*res)[len(*res)-1]
		if len(lastRows) > 0 && lastRows[0].Has(rel.Foreign) {
			prevRows = lastRows
		}
	}

	// 外键去重收集
	seenKeys := map[string]struct{}{}
	for _, row := range prevRows {
		id := row.Get(rel.Foreign)
		if id != nil {
			if k, ok := normalizeKey(id); ok {
				if _, exists := seenKeys[k]; !exists {
					seenKeys[k] = struct{}{}
					foreignIDs = append(foreignIDs, id)
				}
			}
		}
	}

	// 添加 WhereIn 查询数据
	name := rel.Key
	if param.QueryParam.Alias != "" {
		name = param.QueryParam.Alias + "." + name
	}

	// 空数据
	if len(foreignIDs) == 0 {
		*res = append(*res, []maps.MapStr{})
		varname := rel.Name
		for idx := range prevRows {
			prevRows[idx][varname] = []maps.MapStr{}
		}
		return
	}

	// 计算关联查询的行数限制：
	perParentLimit := param.QueryParam.Limit
	queryLimit := 0

	if perParentLimit > 0 {
		queryLimit = perParentLimit * len(foreignIDs)
	} else if perParentLimit == 0 {
		queryLimit = 100 * len(foreignIDs)
	}

	if queryLimit > 0 {
		builder.Query.WhereIn(name, foreignIDs).Limit(queryLimit)
	} else {
		builder.Query.WhereIn(name, foreignIDs)
	}
	rs := builder.Query.MustGetRecordSet()

	// 格式化数据，使用归一化字符串键消除跨数据库驱动整数类型差异（如 int32 vs int64）
	fmtRowMap := map[string][]maps.MapStr{}
	colLen := len(rs.Columns)
	plan, anyDot := compileColPlan(rs.Columns, builder.ColumnMap)
	fmtRows := make([]maps.MapStr, len(rs.Rows))

	for rowIdx, row := range rs.Rows {
		fmtRow := make(maps.MapStr, colLen)
		for i, cp := range plan {
			if i < len(row) {
				val := row[i]
				fmtRow[cp.targetKey] = val
				if cp.filterCol != nil {
					cp.filterCol.FliterOut(val, fmtRow, cp.targetKey)
				}
			}
		}

		var unDotRow maps.MapStr
		if anyDot {
			unDotRow = fmtRow.UnDot()
		} else {
			unDotRow = fmtRow
		}
		fmtRows[rowIdx] = unDotRow

		relVal := fmtRow.Get(rel.Key)
		if k, ok := normalizeKey(relVal); ok {
			fmtRowMap[k] = append(fmtRowMap[k], unDotRow)
		}
	}

	// 追加到上一层主表数据
	varname := rel.Name
	for idx, prow := range prevRows {
		// 安全类型断言与初始化，遵循 golang-safety 准则
		existing, ok := prevRows[idx][varname].([]maps.MapStr)
		if !ok {
			existing = []maps.MapStr{}
		}

		id := prow.Get(rel.Foreign)
		if k, ok := normalizeKey(id); ok {
			if matched, has := fmtRowMap[k]; has {
				// 若用户指定了单父级上限，严格限制挂载条数，确保精确的按父级条数语义
				if perParentLimit > 0 && len(matched) > perParentLimit {
					existing = append(existing, matched[:perParentLimit]...)
				} else {
					existing = append(existing, matched...)
				}
			}
		}
		prevRows[idx][varname] = existing
	}

	*res = append(*res, fmtRows)
}

// normalizeKey 将各类数值/字符串主外键归一化为字符串，用于跨数据库驱动关联匹配
func normalizeKey(val interface{}) (string, bool) {
	if val == nil {
		return "", false
	}
	switch v := val.(type) {
	case string:
		if v == "" || v == "<nil>" || v == "null" {
			return "", false
		}
		return v, true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", false
		}
		if v >= math.MinInt64 && v <= math.MaxInt64 && v == math.Trunc(v) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case float32:
		fv := float64(v)
		if math.IsNaN(fv) || math.IsInf(fv, 0) {
			return "", false
		}
		if fv >= math.MinInt64 && fv <= math.MaxInt64 && fv == math.Trunc(fv) {
			return strconv.FormatInt(int64(fv), 10), true
		}
		return strconv.FormatFloat(fv, 'f', -1, 32), true
	case []byte:
		if len(v) == 0 {
			return "", false
		}
		return string(v), true
	case fmt.Stringer:
		s := v.String()
		if s == "" || s == "<nil>" {
			return "", false
		}
		return s, true
	default:
		s := fmt.Sprintf("%v", v)
		if s == "" || s == "<nil>" {
			return "", false
		}
		return s, true
	}
}
