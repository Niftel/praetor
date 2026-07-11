package store

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// wrap annotates a store error with the operation that produced it, so a bubbled
// DB error carries context ("list templates: pq: ...") instead of a bare,
// context-free driver message. It is nil-safe (returns nil for a nil error) and
// uses %w, so callers' errors.Is / errors.As — e.g. errors.Is(err, sql.ErrNoRows)
// — keep working through the wrap.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// runInTx runs fn inside a transaction, committing on success and rolling back on any
// error (or panic). It saves each multi-statement store method from repeating the
// begin/defer-rollback/commit dance.
func runInTx(ctx context.Context, db *sqlx.DB, fn func(tx *sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
