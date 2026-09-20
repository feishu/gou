package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
	"github.com/yaoapp/kun/maps"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal"
)

// Find 查询单条记录
func (mod *Model) Find(id interface{}, param QueryParam) (maps.MapStr, error) {
	param.Model = mod.Name
	param.Wheres = []QueryWhere{
		{
			Column: mod.PrimaryKey,
			Value:  id,
		},
	}
	param.Limit = 1
	stack := NewQueryStack(param)
	res := stack.Run()
	if len(res) <= 0 {
		return nil, fmt.Errorf("ID=%v的数据不存在", id)
	}
	return res[0], nil
}

// MustFind 查询单条记录
func (mod *Model) MustFind(id interface{}, param QueryParam) maps.MapStr {
	res, err := mod.Find(id, param)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return res
}

// Get 按条件查询, 不分页
func (mod *Model) Get(param QueryParam) ([]maps.MapStr, error) {
	param.Model = mod.Name
	stack := NewQueryStack(param)
	res := stack.Run()
	return res, nil
}

// MustGet 按条件查询, 不分页, 失败抛出异常
func (mod *Model) MustGet(param QueryParam) []maps.MapStr {
	res, err := mod.Get(param)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return res
}

// Paginate 按条件查询, 分页
func (mod *Model) Paginate(param QueryParam, page int, pagesize int) (maps.MapStr, error) {
	param.Model = mod.Name
	stack := NewQueryStack(param)
	res := stack.Paginate(page, pagesize)
	return res, nil
}

// MustPaginate 按条件查询, 分页, 失败抛出异常
func (mod *Model) MustPaginate(param QueryParam, page int, pagesize int) maps.MapStr {
	res, err := mod.Paginate(param, page, pagesize)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return res
}

// Count count records with the given conditions
func (mod *Model) Count(param QueryParam) (int, error) {
	param.Model = mod.Name
	stack := NewQueryStack(param)
	count, err := stack.Count()
	return int(count), err
}

// MustCount count records with the given conditions, throw exception on error
func (mod *Model) MustCount(param QueryParam) int {
	count, err := mod.Count(param)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return count
}

// Create 创建单条数据, 返回新创建数据ID
func (mod *Model) Create(row maps.MapStrAny, ctx ...context.Context) (int, error) {

	errs := mod.Validate(row) // 输入数据校验
	if len(errs) > 0 {
		msgs := []string{}
		for _, err := range errs {
			msgs = append(msgs, err.Column, strings.Join(err.Messages, ","))
			log.Error("[Model] %s Create %v", mod.ID, err)
		}
		exception.New("%s", 400, strings.Join(msgs, ";")).Ctx(errs).Throw()
	}

	mod.FliterIn(row) // 入库前输入数据预处理

	if mod.MetaData.Option.Timestamps {
		row.Set("created_at", dbal.Raw("CURRENT_TIMESTAMP"))
	}

	id, err := mod.Query(ctx...).
		Table(mod.MetaData.Table.Name).
		InsertGetID(row)

	if err != nil {
		return 0, err
	}

	return int(id), err
}

// MustCreate 创建单条数据, 返回新创建数据ID, 失败抛出异常
func (mod *Model) MustCreate(row maps.MapStrAny, ctx ...context.Context) int {
	id, err := mod.Create(row, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return id
}

// Update 更新单条数据
func (mod *Model) Update(id interface{}, row maps.MapStrAny, ctx ...context.Context) error {

	errs := mod.Validate(row) // 输入数据校验
	if len(errs) > 0 {
		msgs := []string{}
		for _, err := range errs {
			msgs = append(msgs, err.Column, strings.Join(err.Messages, ","))
			log.Error("[Model] %s Update %v", mod.ID, err)
		}
		exception.New("%s", 400, strings.Join(msgs, ";")).Ctx(errs).Throw()
	}

	mod.FliterIn(row) // 入库前输入数据预处理

	if mod.MetaData.Option.Timestamps {
		row.Set("updated_at", dbal.Raw("CURRENT_TIMESTAMP"))
	}

	effect, err := mod.Query(ctx...).
		Table(mod.MetaData.Table.Name).
		Where(mod.PrimaryKey, id).
		Limit(1).
		Update(row)

	if effect == 0 {
		return fmt.Errorf("没有数据被更新")
	}

	return err
}

// MustUpdate 更新单条数据, 失败抛出异常
func (mod *Model) MustUpdate(id interface{}, row maps.MapStrAny, ctx ...context.Context) {
	err := mod.Update(id, row, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// Upsert new records or update the existing ones.
func (mod *Model) Upsert(row maps.MapStrAny, uniqueBy []interface{}, updateColumns []interface{}, ctx ...context.Context) (int, error) {

	errs := mod.Validate(row) // validate the input data
	if len(errs) > 0 {
		msgs := []string{}
		for _, err := range errs {
			msgs = append(msgs, err.Column, strings.Join(err.Messages, ","))
			log.Error("[Model] %s Update %v", mod.ID, err)
		}
		exception.New("%s", 400, strings.Join(msgs, ";")).Ctx(errs).Throw()
	}

	mod.FliterIn(row) // preprocess the input data

	if mod.MetaData.Option.Timestamps {
		row.Set("updated_at", dbal.Raw("CURRENT_TIMESTAMP"))
	}

	// If updateColumns is not provided, use all columns
	if updateColumns == nil {
		columns := []interface{}{}
		for _, col := range mod.Columns {
			columns = append(columns, col.Name)
		}
		updateColumns = columns
	}

	effect, err := mod.Query(ctx...).
		Table(mod.MetaData.Table.Name).
		Upsert(row, uniqueBy, updateColumns)

	if err != nil {
		return 0, err
	}

	return int(effect), nil
}

// MustUpsert Create or update a record matching the attributes, and fill it with values.
func (mod *Model) MustUpsert(row maps.MapStrAny, uniqueBy []interface{}, updateColumns []interface{}, ctx ...context.Context) int {
	id, err := mod.Upsert(row, uniqueBy, updateColumns, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return id
}

// Save 保存单条数据, 不存在创建记录, 存在更新记录,  返回数据ID
func (mod *Model) Save(row maps.MapStrAny, ctx ...context.Context) (interface{}, error) {

	errs := mod.Validate(row) // 输入数据校验
	if len(errs) > 0 {
		msgs := []string{}
		for _, err := range errs {
			msgs = append(msgs, err.Column, strings.Join(err.Messages, ","))
			log.Error("[Model] %s Save %v", mod.ID, err)
		}
		exception.New("%s", 400, strings.Join(msgs, ";")).Ctx(errs).Throw()
	}

	mod.FliterIn(row) // 入库前输入数据预处理

	// 更新
	if row.Has(mod.PrimaryKey) {

		if mod.MetaData.Option.Timestamps {
			row.Set("updated_at", dbal.Raw("CURRENT_TIMESTAMP"))
			row.Del("deleted_at") // 忽略删除字段
			row.Del("created_at") // 忽略创建字段
		}

		id := row.Get(mod.PrimaryKey)
		_, err := mod.Query(ctx...).
			Table(mod.MetaData.Table.Name).
			Where(mod.PrimaryKey, id).
			Limit(1).
			Update(row)

		if err != nil {
			return 0, err
		}

		return id, nil
	}

	// 创建
	if mod.MetaData.Option.Timestamps {
		row.Set("created_at", dbal.Raw("CURRENT_TIMESTAMP"))
		row.Del("deleted_at") // 忽略删除字段
		row.Del("updated_at") // 忽略更新字段
	}

	id, err := mod.Query(ctx...).
		Table(mod.MetaData.Table.Name).
		InsertGetID(row)

	if err != nil {
		return 0, err
	}

	return id, err
}

// MustSave 保存单条数据, 返回数据ID, 失败抛出异常
func (mod *Model) MustSave(row maps.MapStrAny, ctx ...context.Context) interface{} {
	id, err := mod.Save(row, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return id
}

// Delete 删除单条记录
func (mod *Model) Delete(id interface{}, ctx ...context.Context) error {
	var c context.Context
	if len(ctx) > 0 && ctx[0] != nil {
		c = ctx[0]
	}
	_, err := mod.DeleteWhere(QueryParam{
		Context: c,
		Wheres: []QueryWhere{
			{
				Column: mod.PrimaryKey,
				Value:  id,
			},
		},
		Limit: 1,
	})
	return err
}

// MustDelete 删除单条记录, 失败抛出异常
func (mod *Model) MustDelete(id interface{}, ctx ...context.Context) {
	err := mod.Delete(id, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// Destroy 真删除单条记录
func (mod *Model) Destroy(id interface{}, ctx ...context.Context) error {
	_, err := mod.Query(ctx...).Table(mod.MetaData.Table.Name).Where(mod.PrimaryKey, id).Limit(1).Delete()
	return err
}

// MustDestroy 真删除单条记录, 失败抛出异常
func (mod *Model) MustDestroy(id interface{}, ctx ...context.Context) {
	err := mod.Destroy(id, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// Insert 插入多条数据
func (mod *Model) Insert(columns []string, rows [][]interface{}, ctx ...context.Context) error {

	// 数据校验
	errs := []ValidateResponse{}
	columnCnt := len(columns)
	for rid, values := range rows {

		if len(values) != columnCnt {
			errs = append(errs, ValidateResponse{
				Line:     rid,
				Column:   "*",
				Messages: []string{fmt.Sprintf("第%d条数据，字段数量与提供字段清单不符.", rid+1)},
			})
		}

		row := maps.MakeMapStr()
		for cid, name := range columns {
			row[name] = values[cid]
		}

		rowerrs := mod.Validate(row) // 输入数据校验
		if len(rowerrs) > 0 {
			for _, err := range rowerrs {
				err.Line = rid
				errs = append(errs, err)
			}
		}

		// 入库前输入数据预处理
		mod.FliterIn(row)
		values := []interface{}{}
		for _, name := range columns {
			values = append(values, row[name])
		}
		rows[rid] = values
	}

	if len(errs) > 0 {
		for _, err := range errs {
			log.Error("[Model] %s Insert %v", mod.ID, err)
		}
		exception.New("%v", 400, errs).Ctx(errs).Throw()
	}

	// 添加创建时间戳
	if mod.MetaData.Option.Timestamps {
		columns = append(columns, "created_at")
		for i := range rows {
			rows[i] = append(rows[i], dbal.Raw("CURRENT_TIMESTAMP"))
		}
	}

	// 写入到数据库
	return mod.Query(ctx...).
		Table(mod.MetaData.Table.Name).
		Insert(rows, columns)

}

// MustInsert 插入多条数据, 失败抛出异常
func (mod *Model) MustInsert(columns []string, rows [][]interface{}, ctx ...context.Context) {
	err := mod.Insert(columns, rows, ctx...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
}

// UpdateWhere 按条件更新记录, 返回更新行数
func (mod *Model) UpdateWhere(param QueryParam, row maps.MapStrAny) (int, error) {

	errs := mod.Validate(row) // 输入数据校验
	if len(errs) > 0 {
		msgs := []string{}
		for _, err := range errs {
			msgs = append(msgs, err.Column, strings.Join(err.Messages, ","))
			log.Error("[Model] %s UpdateWhere %v", mod.ID, err)
		}
		exception.New("%s", 400, strings.Join(msgs, ";")).Ctx(errs).Throw()
	}

	mod.FliterIn(row) // 入库前输入数据预处理

	if mod.MetaData.Option.Timestamps {
		row.Set("updated_at", dbal.Raw("CURRENT_TIMESTAMP"))
	}

	// 如果不是 SQLite3 添加字段
	if mod.Driver != "sqlite3" {
		for name, value := range row {
			if !strings.Contains(name, ".") {
				new := fmt.Sprintf("%s.%s", mod.MetaData.Table.Name, name)
				row.Set(new, value)
				row.Del(name)
			}
		}
	}

	param.Model = mod.Name
	stack := NewQueryStack(param)
	qb := stack.FirstQuery()
	effect, err := qb.Update(row)
	if err != nil {
		return 0, err
	}

	return int(effect), err
}

// MustUpdateWhere 按条件更新记录, 返回更新行数, 失败抛出异常
func (mod *Model) MustUpdateWhere(param QueryParam, row maps.MapStrAny) int {
	effect, err := mod.UpdateWhere(param, row)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return effect
}

// DeleteWhere 批量删除数据, 返回更新行数
func (mod *Model) DeleteWhere(param QueryParam) (int, error) {

	// 软删除
	if mod.MetaData.Option.SoftDeletes {

		// 兼容 SQLite3
		if mod.Driver == "sqlite3" {
			return mod.sqlite3DeleteWhere(param)
		}

		data := maps.MapStrAny{}
		columns := []string{}
		baseTimestamp := time.Now().UnixNano()

		quote := "`"
		if mod.Driver == "postgres" {
			quote = `"`
		}

		for i, col := range mod.UniqueColumns {
			typ := strings.ToLower(col.Type)
			if typ == "string" {
				// For batch soft delete, use a unique timestamp per row via SQL expression
				// The formula generates unique values for each row: original_value + current_timestamp + column_offset
				data[col.Name] = dbal.Raw(fmt.Sprintf("CONCAT_WS('_', %s, '%d', '%d')", col.Name, baseTimestamp, i))
				columns = append(
					columns,
					fmt.Sprintf("CONCAT('\"%s\":\"', %s%s%s, '\"')", col.Name, quote, col.Name, quote),
				)
			} else { // 数字, 布尔型等
				columns = append(
					columns,
					fmt.Sprintf("CONCAT('\"%s\":', %s%s%s)", col.Name, quote, col.Name, quote),
				)
			}
			if col.Nullable {
				data[col.Name] = nil
			}
		}

		param.Model = mod.Name
		stack := NewQueryStack(param)
		qb := stack.FirstQuery()

		// 备份唯一数据
		if len(columns) > 0 {
			restore := dbal.Raw("CONCAT('{'," + strings.Join(columns, ",',',") + ",'}')")
			_, err := qb.Update(maps.MapStr{"__restore_data": restore})
			if err != nil {
				return 0, err
			}
		}

		// 删除数据
		field := fmt.Sprintf("%s.%s", mod.MetaData.Table.Name, "deleted_at")
		// data["deleted_at"] = dbal.Raw("CURRENT_TIMESTAMP")
		data[field] = dbal.Raw("CURRENT_TIMESTAMP")
		effect, err := qb.Update(data)
		if err != nil {
			return 0, err
		}
		return int(effect), nil
	}

	return mod.DestroyWhere(param)
}

// sqliteDeleteWhere SQLite
func (mod *Model) sqlite3DeleteWhere(param QueryParam) (int, error) {
	data := maps.MapStrAny{}
	param.Model = mod.Name
	stack := NewQueryStack(param)
	qb := stack.FirstQuery()

	// 删除数据
	// field := fmt.Sprintf("%s.%s", mod.MetaData.Table.Name, "deleted_at")
	// data[field] = dbal.Raw("CURRENT_TIMESTAMP")
	data["deleted_at"] = dbal.Raw("CURRENT_TIMESTAMP")
	baseTimestamp := time.Now().UnixNano()

	for i, col := range mod.UniqueColumns {
		typ := strings.ToLower(col.Type)
		if typ == "string" {
			// For batch soft delete, use a unique timestamp per row via SQL expression
			// The formula generates unique values for each row: original_value + current_timestamp + column_offset
			data[col.Name] = dbal.Raw(fmt.Sprintf("%s || '_' || '%d' || '_' || '%d'", col.Name, baseTimestamp, i))
		}
	}

	effect, err := qb.Update(data)
	if err != nil {
		return 0, err
	}
	return int(effect), nil
}

// MustDeleteWhere 批量删除数据, 返回更新行数, 失败抛出异常
func (mod *Model) MustDeleteWhere(param QueryParam) int {
	effect, err := mod.DeleteWhere(param)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return effect
}

// DestroyWhere 批量真删除数据, 返回更新行数
func (mod *Model) DestroyWhere(param QueryParam) (int, error) {
	param.Model = mod.Name
	qb := capsule.Query().Table(mod.MetaData.Table.Name)
	for _, where := range param.Wheres {
		param.Where(where, qb, mod)
	}
	effect, err := qb.Delete()
	if err != nil {
		return 0, err
	}
	return int(effect), nil
}

// MustDestroyWhere 批量真删除数据, 返回更新行数, 失败抛出异常
func (mod *Model) MustDestroyWhere(param QueryParam) int {
	effect, err := mod.DestroyWhere(param)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return effect
}

// EachSave 批量保存数据, 返回数据ID集合
func (mod *Model) EachSave(rows []map[string]interface{}, eachrow ...maps.MapStrAny) ([]interface{}, error) {
	return mod.EachSaveWithContext(context.Background(), rows, eachrow...)
}

// EachSaveWithContext 批量保存数据(支持 Context 传递), 返回数据ID集合
func (mod *Model) EachSaveWithContext(ctx context.Context, rows []map[string]interface{}, eachrow ...maps.MapStrAny) ([]interface{}, error) {
	messages := []string{}
	ids := []interface{}{}
	for i, row := range rows {

		if len(eachrow) > 0 {
			for k, v := range eachrow[0] {
				if v == "$index" {
					row[k] = i
				} else {
					row[k] = v
				}
			}
		}

		// check primary
		if id, has := row[mod.PrimaryKey]; has {
			findParam := QueryParam{Select: []interface{}{mod.PrimaryKey}}
			if ctx != nil {
				findParam.Context = ctx
			}
			_, err := mod.Find(id, findParam)
			if err != nil { // id does not exists & create
				_, err := mod.Create(row, ctx)
				if err != nil {
					messages = append(messages, fmt.Sprintf("rows[%d]: %s", i, err.Error()))
					continue
				}
				ids = append(ids, id)
				continue
			}
		}

		id, err := mod.Save(row, ctx)
		if err != nil {
			messages = append(messages, fmt.Sprintf("rows[%d]: %s", i, err.Error()))
			continue
		}
		ids = append(ids, id)
	}

	if len(messages) > 0 {
		return ids, fmt.Errorf("%s", messages)
	}
	return ids, nil
}

// MustEachSave 批量保存数据, 返回数据ID集合, 失败抛出异常
func (mod *Model) MustEachSave(rows []map[string]interface{}, eachrow ...maps.MapStrAny) []interface{} {
	ids, err := mod.EachSave(rows, eachrow...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return ids
}

// MustEachSaveWithContext 批量保存数据(支持 Context 传递), 失败抛出异常
func (mod *Model) MustEachSaveWithContext(ctx context.Context, rows []map[string]interface{}, eachrow ...maps.MapStrAny) []interface{} {
	ids, err := mod.EachSaveWithContext(ctx, rows, eachrow...)
	if err != nil {
		exception.Err(err, 500).Throw()
	}
	return ids
}
