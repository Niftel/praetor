package store

import "fmt"

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
