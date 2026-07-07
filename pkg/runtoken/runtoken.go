// Package runtoken derives the per-run bearer token the host-runner presents on
// its ingestion calls (events/logs/heartbeat/facts).
//
// The token is an HMAC-SHA256 of the execution run id keyed by the shared
// internal secret (PRAETOR_INTERNAL_TOKEN). Two properties fall out of that:
//
//   - It is BOUND to the run id — a token minted for run A does not validate on
//     run B, so a leaked token cannot be replayed to forge events for another run.
//   - It is STATELESS to verify — ingestion recomputes it from the same secret +
//     the run id in the request URL and compares in constant time; there is no
//     per-run token table, column, or migration.
//
// Minting happens at dispatch in the executor (the service that holds the shared
// secret); the token travels only inside the 0600 manifest on the target.
package runtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// Mint returns the per-run token for runID under secret. An empty secret yields
// an empty token: minting is a no-op when no shared secret is configured, and the
// verifier fails closed on the empty result.
func Mint(secret, runID string) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(runID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
