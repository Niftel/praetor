package core

import (
	"context"

	"github.com/praetordev/praetor/pkg/credentials"
)

// ctxGetter and resolveCredentialInjectors now live in pkg/credentials so the
// reconciler can reconstruct the same SSH identity the scheduler baked into a
// job's manifest. These thin aliases keep the scheduler's existing call sites.
type ctxGetter = credentials.CtxGetter

func resolveCredentialInjectors(ctx context.Context, q ctxGetter, credID int64) (map[string]string, map[string]string, error) {
	return credentials.ResolveInjectors(ctx, q, credID)
}
