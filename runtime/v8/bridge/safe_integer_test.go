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
