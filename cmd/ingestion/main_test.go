package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/runtoken"
)

const testSecret = "test-internal-secret"

// mount wraps h with mw on a run-scoped route so chi.URLParam("run_id") resolves
// exactly as it does in production.
func mount(mw func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.With(mw).Post("/api/v1/runs/{run_id}/events", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
}

func do(h http.Handler, authz string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-abc/events", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRunTokenAuth(t *testing.T) {
	h := mount(runTokenAuth(testSecret))

	cases := []struct {
		name  string
		authz string
		want  int
	}{
		{"full internal token", "Bearer " + testSecret, http.StatusOK},
		{"valid per-run token", "Bearer " + runtoken.Mint(testSecret, "run-abc"), http.StatusOK},
		{"per-run token for another run", "Bearer " + runtoken.Mint(testSecret, "run-xyz"), http.StatusUnauthorized},
		{"garbage token", "Bearer nope", http.StatusUnauthorized},
		{"missing header", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := do(h, c.authz); got != c.want {
				t.Fatalf("authz %q: got %d, want %d", c.authz, got, c.want)
			}
		})
	}
}

// An unset secret must reject every request (fail closed), including one bearing
// the empty per-run token that Mint returns for an empty secret.
func TestRunTokenAuthEmptySecretFailsClosed(t *testing.T) {
	h := mount(runTokenAuth(""))
	for _, authz := range []string{"", "Bearer ", "Bearer " + runtoken.Mint("", "run-abc")} {
		if got := do(h, authz); got != http.StatusUnauthorized {
			t.Fatalf("empty secret must reject authz %q, got %d", authz, got)
		}
	}
}

func TestInternalAuth(t *testing.T) {
	h := mount(internalAuth(testSecret))
	if got := do(h, "Bearer "+testSecret); got != http.StatusOK {
		t.Fatalf("full token must pass internalAuth, got %d", got)
	}
	// A per-run token must NOT open an internal-only endpoint.
	if got := do(h, "Bearer "+runtoken.Mint(testSecret, "run-abc")); got != http.StatusUnauthorized {
		t.Fatalf("per-run token must not pass internalAuth, got %d", got)
	}
	if got := do(h, ""); got != http.StatusUnauthorized {
		t.Fatalf("missing header must fail internalAuth, got %d", got)
	}
}

func TestInternalAuthEmptySecretFailsClosed(t *testing.T) {
	h := mount(internalAuth(""))
	if got := do(h, "Bearer "); got != http.StatusUnauthorized {
		t.Fatalf("empty secret must reject, got %d", got)
	}
}
