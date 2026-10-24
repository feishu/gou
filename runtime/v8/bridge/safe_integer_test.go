package bridge

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"rogchap.com/v8go"
)

func TestSafeIntegerMapping(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	tests := []struct {
		name         string
		input        interface{}
		expectTypeof string
		expectNumber float64
		isBigInt     bool
		expectBigInt int64
	}{
		{
			name:         "Standard Millisecond Timestamp",
			input:        int64(1789307032000),
			expectTypeof: "number",
			expectNumber: 1789307032000,
		},
		{
			name:         "Past Timestamp (Negative)",
			input:        int64(-1789307032000),
			expectTypeof: "number",
			expectNumber: -1789307032000,
		},
		{
			name:         "Max Safe Integer",
			input:        maxSafeInteger,
			expectTypeof: "number",
			expectNumber: 9007199254740991,
		},
		{
			name:         "Min Safe Integer",
			input:        minSafeInteger,
			expectTypeof: "number",
			expectNumber: -9007199254740991,
		},
		{
			name:         "Small Integer within Int32",
			input:        int64(12345),
			expectTypeof: "number",
			expectNumber: 12345,
		},
		{
			name:         "Uint64 Safe Timestamp",
			input:        uint64(1789307032000),
			expectTypeof: "number",
			expectNumber: 1789307032000,
		},
		{
			name:         "Int within 64-bit safe range",
			input:        int(1789307032000),
			expectTypeof: "number",
			expectNumber: 1789307032000,
		},
		{
			name:         "Exceeds Safe Range -> BigInt",
			input:        int64(1 << 60),
			expectTypeof: "bigint",
			isBigInt:     true,
			expectBigInt: 1 << 60,
		},
		{
			name:         "Negative Exceeds Safe Range -> BigInt",
			input:        -int64(1 << 60),
			expectTypeof: "bigint",
			isBigInt:     true,
			expectBigInt: -(1 << 60),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := JsValue(ctx, tt.input)
			assert.NoError(t, err)
			assert.NotNil(t, val)
			defer val.Release()

			// Check in JS runtime
			ctx.Global().Set("__test_val", val)
			defer ctx.Global().Delete("__test_val")

			typeofVal, err := ctx.RunScript("typeof __test_val", "typeof.js")
			assert.NoError(t, err)
			assert.Equal(t, tt.expectTypeof, typeofVal.String())

			if !tt.isBigInt {
				assert.True(t, val.IsNumber())
				assert.Equal(t, tt.expectNumber, val.Number())

				// Verify in JavaScript arithmetic
				jsCmp, err := ctx.RunScript(
					"__test_val === "+val.String(),
					"cmp.js",
				)
				assert.NoError(t, err)
				assert.True(t, jsCmp.Boolean())

				// Verify GoValue round-trip
				roundTrip, err := GoValue(val, ctx)
				assert.NoError(t, err)
				if val.IsInt32() {
					assert.Equal(t, int(tt.expectNumber), roundTrip)
				} else {
					assert.Equal(t, tt.expectNumber, roundTrip)
				}
			} else {
				assert.True(t, val.IsBigInt())
				roundTrip, err := GoValue(val, ctx)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectBigInt, roundTrip)
			}
		})
	}
}

func TestInt32Boundaries(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// Exactly math.MaxInt32
	maxInt32Val, err := JsValue(ctx, int64(math.MaxInt32))
	assert.NoError(t, err)
	assert.True(t, maxInt32Val.IsInt32())
	assert.Equal(t, int32(math.MaxInt32), maxInt32Val.Int32())

	// Exactly math.MaxInt32 + 1 (should become float64 number, NOT overflow to negative int32!)
	overflowVal, err := JsValue(ctx, int64(math.MaxInt32)+1)
	assert.NoError(t, err)
	assert.True(t, overflowVal.IsNumber())
	assert.False(t, overflowVal.IsInt32())
	assert.Equal(t, float64(math.MaxInt32)+1, overflowVal.Number())
	assert.Greater(t, overflowVal.Number(), float64(0)) // Absolutely not negative!
}

func TestSafeIntegerInSmallMap(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// Mimic volc.im.getAppToken result (small map <= 8 keys)
	payload := map[string]interface{}{
		"AppId":      910052,
		"ExpireTime": int64(1792984236522),
		"Token":      "D1RNa0HOTXuJrkUVUTO5KNbwuCi7FaSKUESn0Lom6hUnollvnxBFZR",
		"UserId":     int64(12222222),
	}

	val, err := JsValue(ctx, payload)
	assert.NoError(t, err)
	assert.NotNil(t, val)
	defer val.Release()

	// Verify in JS runtime
	ctx.Global().Set("__app_token", val)
	defer ctx.Global().Delete("__app_token")

	// Type checking in JS
	typeUserId, err := ctx.RunScript("typeof __app_token.UserId", "test.js")
	assert.NoError(t, err)
	assert.Equal(t, "number", typeUserId.String())

	typeExpireTime, err := ctx.RunScript("typeof __app_token.ExpireTime", "test.js")
	assert.NoError(t, err)
	assert.Equal(t, "number", typeExpireTime.String())

	// Arithmetic operations in JS (cannot mix BigInt and Number if it were BigInt)
	calcRes, err := ctx.RunScript("__app_token.UserId + 1", "calc.js")
	assert.NoError(t, err)
	assert.Equal(t, 12222223.0, calcRes.Number())

	// JSON stringify via v8go directly (never throws TypeError: Do not know how to serialize a BigInt)
	jsonStr, err := v8go.JSONStringify(ctx, val)
	assert.NoError(t, err)
	assert.Contains(t, jsonStr, `"UserId":12222222`)
	assert.Contains(t, jsonStr, `"ExpireTime":1792984236522`)
}

func Test16FieldsFastPath(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// Exactly 16 fields mimicking a complete medical patient consultation record
	payload := map[string]interface{}{
		"f01_id":          int64(10001),
		"f02_user_id":     int64(12222222),
		"f03_name":        "张三",
		"f04_age":         int32(35),
		"f05_gender":      "male",
		"f06_dept":        "内科",
		"f07_doctor":      "李医生",
		"f08_cost":        199.50,
		"f09_is_paid":     true,
		"f10_expire_time": int64(1792984236522),
		"f11_card_no":     "MED20261024001",
		"f12_status":      "consulting",
		"f13_remark":      "复诊患者，常规开药",
		"f14_items":       int32(4),
		"f15_discount":    float64(0.85),
		"f16_seq":         uint64(8888),
	}

	val, err := JsValue(ctx, payload)
	assert.NoError(t, err)
	assert.NotNil(t, val)
	defer val.Release()

	// In JS runtime
	ctx.Global().Set("__record", val)
	defer ctx.Global().Delete("__record")

	res, err := ctx.RunScript("__record.f01_id + __record.f04_age", "add.js")
	assert.NoError(t, err)
	assert.Equal(t, 10036.0, res.Number())

	jsonStr, err := v8go.JSONStringify(ctx, val)
	assert.NoError(t, err)
	assert.Contains(t, jsonStr, `"f01_id":10001`)
	assert.Contains(t, jsonStr, `"f03_name":"张三"`)
	assert.Contains(t, jsonStr, `"f10_expire_time":1792984236522`)
}

func TestSuperLargeIntegerAndBigIntSerialization(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	// 7685592449233781054 exceeds 2^53 - 1 (9007199254740991)
	const superLargeID = int64(7685592449233781054)

	payload := map[string]interface{}{
		"id":         superLargeID,
		"name":       "super_large_id_test",
		"expire_time": int64(1792984236522), // Safe int64
	}

	val, err := JsValue(ctx, payload)
	assert.NoError(t, err)
	assert.NotNil(t, val)
	defer val.Release()

	ctx.Global().Set("__payload", val)
	defer ctx.Global().Delete("__payload")

	// 1. Verify typeof: id must be bigint, expire_time must be number
	typeId, err := ctx.RunScript("typeof __payload.id", "test_types.js")
	assert.NoError(t, err)
	assert.Equal(t, "bigint", typeId.String())

	typeExpire, err := ctx.RunScript("typeof __payload.expire_time", "test_types.js")
	assert.NoError(t, err)
	assert.Equal(t, "number", typeExpire.String())

	// 2. In JavaScript, BigInt arithmetic should work with n suffix
	calcRes, err := ctx.RunScript("(__payload.id + 1n).toString()", "calc_bigint.js")
	assert.NoError(t, err)
	assert.Equal(t, "7685592449233781055", calcRes.String())

	// 3. JSON.stringify in JS runtime must NOT throw TypeError, but format id as string
	jsonInJs, err := ctx.RunScript("JSON.stringify(__payload)", "stringify.js")
	assert.NoError(t, err)
	assert.Contains(t, jsonInJs.String(), `"id":"7685592449233781054"`)
	assert.Contains(t, jsonInJs.String(), `"expire_time":1792984236522`)

	// 4. v8go.JSONStringify from Go side must also succeed without error
	jsonStr, err := v8go.JSONStringify(ctx, val)
	assert.NoError(t, err)
	assert.Contains(t, jsonStr, `"id":"7685592449233781054"`)
	assert.Contains(t, jsonStr, `"expire_time":1792984236522`)

	// 5. GoValue roundtrip
	goVal, err := GoValue(val, ctx)
	assert.NoError(t, err)
	goMap, ok := goVal.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "7685592449233781054", goMap["id"])
	assert.Equal(t, float64(1792984236522), goMap["expire_time"])
}
