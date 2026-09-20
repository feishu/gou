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

	rows := []xun.R{}
	pageRes := builder.Query.MustPaginate(pagesize, page)
	for _, item := range pageRes.Items {
		rows = append(rows, xun.MakeR(item))
	}

	fmtRows := []maps.MapStr{}
	for _, row := range rows {
		fmtRow := maps.MapStr{}
		for key, value := range row {
			cmap, has := builder.ColumnMap[key]
			if !has {
				cmap, has = builder.ColumnMap[strings.ToLower(key)]
			}
			if has {
				fmtRow[cmap.Export] = value
				cmap.Column.FliterOut(value, fmtRow, cmap.Export)
				continue
			}
			fmtRow[key] = value
		}

		fmtRows = append(fmtRows, fmtRow.UnDot())
	}
	*res = append(*res, fmtRows)
	stack.Next()
	return pageRes
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

	var rows []xun.R
	if unlimited {
		rows = builder.Query.MustGet()
	} else {
		rows = builder.Query.Limit(limit).MustGet()
	}

	fmtRows := []maps.MapStr{}
	for _, row := range rows {
		fmtRow := maps.MapStr{}
		for key, value := range row {
			cmap, has := builder.ColumnMap[key]
			if !has {
				cmap, has = builder.ColumnMap[strings.ToLower(key)]
			}
			if has {
				fmtRow[cmap.Export] = value
				cmap.Column.FliterOut(value, fmtRow, cmap.Export)
				continue
			}
			fmtRow[key] = value
		}
		fmtRows = append(fmtRows, fmtRow.UnDot())
	}
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
	// 针对多主记录场景，单层 SQL 的 LIMIT 不能直接使用单一的 limit（否则会导致后半段父记录数据被静默截断）
	// 同时为了防止关联大表引发内存暴涨，按父记录数动态扩充安全上限：
	perParentLimit := param.QueryParam.Limit
	queryLimit := 0

	if perParentLimit > 0 {
		// 用户明确指定了每个父级最多需要的子记录数（如每个用户取 5 条最新订单）
		queryLimit = perParentLimit * len(foreignIDs)
	} else if perParentLimit == 0 {
		// 用户未显式指定 limit：保留安全保护机制（防止全表扫描导致内存暴涨）
		// 按每个父记录预留 100 条额度，而不是让所有父记录总共只能分 100 条
		queryLimit = 100 * len(foreignIDs)
	}
	// perParentLimit < 0 时为显式不限制 (queryLimit = 0)

	if queryLimit > 0 {
		builder.Query.WhereIn(name, foreignIDs).Limit(queryLimit)
	} else {
		builder.Query.WhereIn(name, foreignIDs)
	}
	rows := builder.Query.MustGet()

	// 格式化数据，使用归一化字符串键消除跨数据库驱动整数类型差异（如 int32 vs int64）
	fmtRowMap := map[string][]maps.MapStr{}
	fmtRows := []maps.MapStr{}
	for _, row := range rows {
		fmtRow := maps.MapStr{}
		for key, value := range row {
			cmap, has := builder.ColumnMap[key]
			if !has {
				cmap, has = builder.ColumnMap[strings.ToLower(key)]
			}
			if has {
				fmtRow[cmap.Export] = value
				cmap.Column.FliterOut(value, fmtRow, cmap.Export)
				continue
			}
			fmtRow[key] = value
		}

		unDotRows := fmtRow.UnDot()
		fmtRows = append(fmtRows, unDotRows)

		relVal := fmtRow.Get(rel.Key)
		if k, ok := normalizeKey(relVal); ok {
			fmtRowMap[k] = append(fmtRowMap[k], unDotRows)
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
