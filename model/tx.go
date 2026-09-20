package model

import (
	"context"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
	"github.com/yaoapp/xun/capsule"
	"github.com/yaoapp/xun/dbal/query"
)

// TxContextKey context key for transaction session
type txContextKey struct{}

// TxContextKeyInstance transaction context key instance
var TxContextKeyInstance = txContextKey{}

// TxSession represents an active, physical transaction session
type TxSession struct {
	Tx     *sqlx.Tx
	Query  query.Query
	closed bool
	mu     sync.Mutex
}

// BeginTx starts a physical database transaction
func BeginTx(ctx ...context.Context) (*TxSession, error) {
	qb := capsule.Query()
	db := qb.DB(true)
	if db == nil {
		return nil, fmt.Errorf("database write connection not available")
	}

	var tx *sqlx.Tx
	var err error
	if len(ctx) > 0 && ctx[0] != nil {
		tx, err = db.BeginTxx(ctx[0], nil)
	} else {
		tx, err = db.Beginx()
	}
	if err != nil {
		return nil, err
	}

	return &TxSession{
		Tx:    tx,
		Query: qb.WithTx(tx),
	}, nil
}

// Commit commits the active transaction
func (sess *TxSession) Commit() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return nil
	}
	sess.closed = true
	return sess.Tx.Commit()
}

// Rollback rolls back the active transaction
func (sess *TxSession) Rollback() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return nil
	}
	sess.closed = true
	return sess.Tx.Rollback()
}

// WithTxContext injects a transaction session into context
func WithTxContext(ctx context.Context, sess *TxSession) context.Context {
	return context.WithValue(ctx, TxContextKeyInstance, sess)
}

// TxFromContext extracts transaction session from context
func TxFromContext(ctx context.Context) (*TxSession, bool) {
	if ctx == nil {
		return nil, false
	}
	sess, ok := ctx.Value(TxContextKeyInstance).(*TxSession)
	return sess, ok && sess != nil && !sess.closed
}

// Transaction executes a function within a physical database transaction.
// If fn returns an error or panics, the transaction is automatically rolled back.
// If fn completes successfully, the transaction is automatically committed.
// Prevents connection pool contamination and dangling row locks.
func Transaction(fn func(ctx context.Context) error, ctxs ...context.Context) (err error) {
	var ctx context.Context = context.Background()
	if len(ctxs) > 0 && ctxs[0] != nil {
		ctx = ctxs[0]
	}

	sess, err := BeginTx(ctx)
	if err != nil {
		log.Error("[Model.Transaction] BeginTx failed: %v", err)
		return err
	}

	txCtx := WithTxContext(ctx, sess)

	defer func() {
		if r := recover(); r != nil {
			_ = sess.Rollback()
			err = exception.Catch(r)
			if err != nil {
				exception.DebugPrint(err, "Transaction Panic Rollback")
			}
		} else if err != nil {
			_ = sess.Rollback()
		} else {
			commitErr := sess.Commit()
			if commitErr != nil {
				err = commitErr
			}
		}
	}()

	err = fn(txCtx)
	return err
}

// Query returns the query builder for this model (supports transaction seam)
func (mod *Model) Query(ctx ...context.Context) query.Query {
	if len(ctx) > 0 && ctx[0] != nil {
		if sess, ok := TxFromContext(ctx[0]); ok && sess != nil {
			if sess.Query != nil {
				return sess.Query.WithContext(ctx[0])
			}
			return nil
		}
		if capsule.Global != nil {
			return capsule.Query().WithContext(ctx[0])
		}
	}
	return capsule.Query()
}
