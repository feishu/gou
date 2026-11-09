package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDefaultEqual(t *testing.T) {
	// 相同值或同为 nil
	assert.True(t, isDefaultEqual(nil, nil))
	assert.True(t, isDefaultEqual(0, 0))
	assert.True(t, isDefaultEqual("test", "test"))

	// 单边 nil
	assert.False(t, isDefaultEqual(0, nil))
	assert.False(t, isDefaultEqual(nil, "0"))

	// 数值相等但表现形式不同
	assert.True(t, isDefaultEqual(0, float64(0)))
	assert.True(t, isDefaultEqual(float64(0), "0.00"))
	assert.True(t, isDefaultEqual("0.00", "'0.00'"))
	assert.True(t, isDefaultEqual(float64(0), "'0.00'"))
	assert.True(t, isDefaultEqual(1, "1"))
	assert.True(t, isDefaultEqual(1, "'1'"))
	assert.True(t, isDefaultEqual(10.5, "10.50"))
	assert.False(t, isDefaultEqual(1, 2))
	assert.False(t, isDefaultEqual("1.0", "2.0"))

	// 布尔值与数值兼容
	assert.True(t, isDefaultEqual(false, "0"))
	assert.True(t, isDefaultEqual(false, 0))
	assert.True(t, isDefaultEqual(false, "false"))
	assert.True(t, isDefaultEqual(true, "1"))
	assert.True(t, isDefaultEqual(true, 1))
	assert.True(t, isDefaultEqual(true, "true"))
	assert.False(t, isDefaultEqual(true, false))

	// 字符串单引号去除
	assert.True(t, isDefaultEqual("pending", "'pending'"))
	assert.False(t, isDefaultEqual("pending", "'active'"))
}

