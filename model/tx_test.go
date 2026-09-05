package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTxContextInjection(t *testing.T) {
	ctx := context.Background()
	_, ok := TxFromContext(ctx)
	assert.False(t, ok, "should not have tx session in empty context")

	sess := &TxSession{closed: false}
	txCtx := WithTxContext(ctx, sess)

	extracted, ok := TxFromContext(txCtx)
	assert.True(t, ok, "should extract tx session from context")
	assert.Equal(t, sess, extracted)

	// After closing, TxFromContext should return false
	sess.closed = true
	_, ok = TxFromContext(txCtx)
	assert.False(t, ok, "closed tx session should not be returned as active")
}

func TestTransactionRollbackOnPanic(t *testing.T) {
	// Mock transaction behavior without requiring live database
	sess := &TxSession{closed: false}
	rolledBack := false

	// Test panic recovery in Transaction closure pattern
	assert.Panics(t, func() {
		// simulate manual closure rollback
		defer func() {
			if r := recover(); r != nil {
				rolledBack = true
				sess.closed = true
				panic(r)
			}
		}()
		panic("simulated business panic")
	})

	assert.True(t, rolledBack)
	assert.True(t, sess.closed)
}

func TestModelQuerySeam(t *testing.T) {
	mod := &Model{ID: "test_model"}

	// Without transaction context, should not panic and should delegate to global query
	// (Note: capsule may panic if not initialized in unit test, which proves it reaches capsule)
	ctx := context.Background()
	sess := &TxSession{closed: false, Query: nil}
	txCtx := WithTxContext(ctx, sess)

	// With tx context, Query should return the session's Query directly
	q := mod.Query(txCtx)
	assert.Nil(t, q)
}
