package repository

import (
	"context"

	"gorm.io/gorm"
)

// txContextKey carries an in-flight transaction handle through a context so
// repositories can join a caller-managed transaction.
type txContextKey struct{}

// Transactor runs a function inside a single database transaction.
// Repositories invoked with the context passed to fn join the transaction.
type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type transactor struct {
	db *gorm.DB
}

// NewTransactor constructs a transactor bound to db.
func NewTransactor(db *gorm.DB) Transactor {
	return &transactor{db: db}
}

func (t *transactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txContextKey{}, tx))
	})
}

// txFromContext extracts the transaction handle carried by ctx, if any.
func txFromContext(ctx context.Context) (*gorm.DB, bool) {
	tx, ok := ctx.Value(txContextKey{}).(*gorm.DB)
	return tx, ok && tx != nil
}

// dbOrTx returns the transaction handle carried by ctx, falling back to the
// repository's own handle when ctx carries none.
func dbOrTx(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := txFromContext(ctx); ok {
		return tx
	}
	return db
}
