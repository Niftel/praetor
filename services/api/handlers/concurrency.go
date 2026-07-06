package handlers

import (
	"errors"

	"github.com/lib/pq"
)

// isActiveRunConflict reports whether err is the unique-violation raised by the
// active-per-template concurrency index (uq_unified_jobs_active_concurrency) —
// i.e. a concurrent launch lost the race to an already-active run of a template
// that disallows simultaneous runs. Callers translate this into their normal
// "already active" response (409 for a manual launch, a skip for webhook/event)
// instead of a 500. See migration 000043.
func isActiveRunConflict(err error) bool {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.Constraint == "uq_unified_jobs_active_concurrency"
	}
	return false
}
