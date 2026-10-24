package bridge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"rogchap.com/v8go"
)

type PatientRecord struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Age        int     `json:"age"`
	IsVip      bool    `json:"is_vip"`
	Cost       float64 `json:"cost"`
	ExpireTime int64   `json:"expire_time"`
}

func TestUnmarshalTo(t *testing.T) {
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	ctx := v8go.NewContext(iso)
	defer ctx.Close()

	jsVal, err := ctx.RunScript(`({
		id: 10001,
		name: "王五",
		age: 42,
		is_vip: true,
		cost: 299.88,
		expire_time: 1792984236522
	})`, "test.js")
	assert.NoError(t, err)

	record, err := UnmarshalTo[PatientRecord](jsVal)
	assert.NoError(t, err)
	assert.NotNil(t, record)

	assert.Equal(t, int64(10001), record.ID)
	assert.Equal(t, "王五", record.Name)
	assert.Equal(t, 42, record.Age)
	assert.True(t, record.IsVip)
	assert.Equal(t, 299.88, record.Cost)
	assert.Equal(t, int64(1792984236522), record.ExpireTime)

	// Test nil/null/undefined
	nilRecord, err := UnmarshalTo[PatientRecord](nil)
	assert.NoError(t, err)
	assert.Nil(t, nilRecord)

	nullVal := v8go.Null(iso)
	nullRecord, err := UnmarshalTo[PatientRecord](nullVal)
	assert.NoError(t, err)
	assert.Nil(t, nullRecord)
}
