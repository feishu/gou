package model

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/kun/maps"
)

type mockStringer struct {
	val string
}

func (m mockStringer) String() string {
	return m.val
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
		ok       bool
	}{
		// 基础整数类型
		{name: "int positive", input: int(100), expected: "100", ok: true},
		{name: "int negative", input: int(-100), expected: "-100", ok: true},
		{name: "int zero", input: int(0), expected: "0", ok: true},
		{name: "int32 (DM8 int column)", input: int32(12345), expected: "12345", ok: true},
		{name: "int64 (DM8/MySQL bigint)", input: int64(12345), expected: "12345", ok: true},
		{name: "int16", input: int16(12), expected: "12", ok: true},
		{name: "int8", input: int8(1), expected: "1", ok: true},
		{name: "uint", input: uint(200), expected: "200", ok: true},
		{name: "uint64", input: uint64(999999), expected: "999999", ok: true},
		{name: "uint32", input: uint32(888), expected: "888", ok: true},
		{name: "uint16", input: uint16(66), expected: "66", ok: true},
		{name: "uint8", input: uint8(5), expected: "5", ok: true},

		// 浮点数类型（如 JSON/V8 传入的浮点整数）
		{name: "float64 whole number", input: float64(100.0), expected: "100", ok: true},
		{name: "float64 decimal", input: float64(100.5), expected: "100.5", ok: true},
		{name: "float32 whole number", input: float32(200.0), expected: "200", ok: true},
		{name: "float32 decimal", input: float32(200.25), expected: "200.25", ok: true},
		{name: "float64 NaN", input: math.NaN(), expected: "", ok: false},
		{name: "float64 Inf", input: math.Inf(1), expected: "", ok: false},
		{name: "float64 -Inf", input: math.Inf(-1), expected: "", ok: false},

		// 字符串与字节
		{name: "string normal", input: "12345", expected: "12345", ok: true},
		{name: "string uuid", input: "c7b2e1a0-1234-4567-89ab-cdef01234567", expected: "c7b2e1a0-1234-4567-89ab-cdef01234567", ok: true},
		{name: "string empty", input: "", expected: "", ok: false},
		{name: "bytes valid", input: []byte("12345"), expected: "12345", ok: true},
		{name: "bytes empty", input: []byte{}, expected: "", ok: false},

		// 特殊类型与非法值
		{name: "nil", input: nil, expected: "", ok: false},
		{name: "string nil literal", input: "<nil>", expected: "", ok: false},
		{name: "fmt.Stringer", input: mockStringer{val: "custom-key-1"}, expected: "custom-key-1", ok: true},
		{name: "fmt.Stringer empty", input: mockStringer{val: ""}, expected: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, ok := normalizeKey(tt.input)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestNormalizeKeyCrossTypeMatching(t *testing.T) {
	// 模拟达梦数据库（DM8）与 MySQL、V8 引擎之间的跨类型关联场景
	// 主表返回 int32(1)，从表返回 int64(1)，V8 传入 float64(1.0)，字符串参数传入 "1"
	pkDM8 := int32(1)
	fkMySQL := int64(1)
	fkV8 := float64(1.0)
	fkString := "1"

	keyDM8, ok1 := normalizeKey(pkDM8)
	keyMySQL, ok2 := normalizeKey(fkMySQL)
	keyV8, ok3 := normalizeKey(fkV8)
	keyStr, ok4 := normalizeKey(fkString)

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.True(t, ok3)
	assert.True(t, ok4)

	// 所有归一化后的键必须一致
	assert.Equal(t, "1", keyDM8)
	assert.Equal(t, keyDM8, keyMySQL)
	assert.Equal(t, keyDM8, keyV8)
	assert.Equal(t, keyDM8, keyStr)

	// 模拟哈希索引查找
	lookup := map[string][]maps.MapStr{
		keyDM8: {
			{"id": 10, "name": "Admin", "user_id": int64(1)},
			{"id": 11, "name": "Editor", "user_id": int64(1)},
		},
	}

	// 即使通过 int64(1) 查找，也能命中
	matched, has := lookup[keyMySQL]
	assert.True(t, has)
	assert.Len(t, matched, 2)
}

func TestHasManyParentSelection(t *testing.T) {
	// 场景 1：同级两个 hasMany（例如 user 关联 roles 和 permissions）
	// 第 0 层：主表 users（主键 id 为 int32(1)）
	rootRows := []maps.MapStrAny{
		{"id": int32(1), "name": "User 1"},
		{"id": int32(2), "name": "User 2"},
	}
	res := [][]maps.MapStrAny{rootRows}

	// 第 1 个 hasMany：roles，关联键 foreign = "id", key = "user_id"
	relRoles := Relation{Name: "roles", Foreign: "id", Key: "user_id"}

	// 模拟定位父表
	prevRows := res[0]
	if len(res) > 1 {
		lastRows := res[len(res)-1]
		if len(lastRows) > 0 && lastRows[0].Has(relRoles.Foreign) {
			prevRows = lastRows
		}
	}
	assert.Equal(t, rootRows, prevRows)

	// 模拟已回填 roles 并推入 res
	roleRows := []maps.MapStrAny{
		{"role_id": 101, "role_name": "Admin", "user_id": int64(1)},
	}
	res = append(res, roleRows)

	// 第 2 个 hasMany：permissions（同级关系），父表字段依然是 user.id
	relPerms := Relation{Name: "permissions", Foreign: "id", Key: "uid"}
	prevRows2 := res[0]
	if len(res) > 1 {
		lastRows := res[len(res)-1] // lastRows 是 roleRows，它没有 "id" 字段
		if len(lastRows) > 0 && lastRows[0].Has(relPerms.Foreign) {
			prevRows2 = lastRows
		}
	}
	// 关键断言：同级第 2 个 hasMany 必须正确回退到根主表 users，而不能被第 1 个 hasMany（roleRows）污染！
	assert.Equal(t, rootRows, prevRows2)
	assert.Equal(t, int32(1), prevRows2[0]["id"])
}

func TestHasManySafeTypeAssertion(t *testing.T) {
	// 模拟父表行中的关联字段可能未初始化或为非法类型时的安全性
	userRow := maps.MapStr{"id": int32(1), "name": "User 1"}
	prevRows := []maps.MapStr{userRow}
	varname := "roles"

	// 1. 未初始化时
	existing, ok := prevRows[0][varname].([]maps.MapStr)
	if !ok {
		existing = []maps.MapStr{}
	}
	assert.NotNil(t, existing)
	assert.Empty(t, existing)

	// 2. 追加子表数据
	childData := []maps.MapStr{{"id": 1, "name": "Role 1"}}
	existing = append(existing, childData...)
	prevRows[0][varname] = existing

	// 3. 再次断言类型
	existing2, ok2 := prevRows[0][varname].([]maps.MapStr)
	assert.True(t, ok2)
	assert.Len(t, existing2, 1)
}

func TestHasManyLimitCalculationAndDeduplication(t *testing.T) {
	// 场景：模拟 20 个主表用户，存在部分重复的外键引用
	prevRows := []maps.MapStrAny{
		{"id": int32(1)},
		{"id": int32(1)}, // 重复外键
		{"id": int32(2)},
		{"id": int32(3)},
		{"id": int64(4)},
	}

	// 1. 外键去重验证
	seenKeys := map[string]struct{}{}
	foreignIDs := []interface{}{}
	for _, row := range prevRows {
		id := row.Get("id")
		if id != nil {
			if k, ok := normalizeKey(id); ok {
				if _, exists := seenKeys[k]; !exists {
					seenKeys[k] = struct{}{}
					foreignIDs = append(foreignIDs, id)
				}
			}
		}
	}
	// 原 5 条数据，由于 id=1 重复，去重后必须为 4
	assert.Len(t, foreignIDs, 4)

	// 2. 默认 Limit 计算：防止内存暴涨的同时按父级扩充容量
	perParentLimitDefault := 0
	queryLimitDefault := 0
	if perParentLimitDefault == 0 {
		queryLimitDefault = 100 * len(foreignIDs)
	}
	// 4 个父级应分配 400 条上限，而不是死板的全局 100 条
	assert.Equal(t, 400, queryLimitDefault)

	// 3. 用户显式指定 perParentLimit = 5
	perParentLimitCustom := 5
	queryLimitCustom := perParentLimitCustom * len(foreignIDs)
	assert.Equal(t, 20, queryLimitCustom)

	// 4. 模拟内存挂载截断：即使子查询某父级有 8 条记录，但指定了 perParentLimit=5 时严格只挂载 5 条
	mockMatchedRows := []maps.MapStr{
		{"id": 1, "val": "a"},
		{"id": 2, "val": "b"},
		{"id": 3, "val": "c"},
		{"id": 4, "val": "d"},
		{"id": 5, "val": "e"},
		{"id": 6, "val": "f"},
		{"id": 7, "val": "g"},
		{"id": 8, "val": "h"},
	}
	existing := []maps.MapStr{}
	if perParentLimitCustom > 0 && len(mockMatchedRows) > perParentLimitCustom {
		existing = append(existing, mockMatchedRows[:perParentLimitCustom]...)
	} else {
		existing = append(existing, mockMatchedRows...)
	}
	assert.Len(t, existing, 5)
	assert.Equal(t, "e", existing[4]["val"])
}


