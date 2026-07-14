package authorization

import (
	"context"
	"time"
)

// RefreshEvery periodically checks the configured source. Refresh failures are
// reported while RBAC v4 keeps serving the last-known-good snapshot.
func (a *Authorizer) RefreshEvery(ctx context.Context, interval time.Duration, report func(error)) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.RefreshPolicy(ctx); err != nil && report != nil {
				report(err)
			}
		}
	}
}
