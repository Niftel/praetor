package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// statusWriter captures the response status code so the activity recorder can
// log only successful mutations.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// skipActivity is true for high-volume machine/internal endpoints that would
// drown the audit log (events, heartbeats, log/fact/inventory data sync).
func skipActivity(path string) bool {
	for _, frag := range []string{"/heartbeat", "/events", "/logs", "/facts", "/sync-data", "/runner-heartbeat"} {
		if strings.Contains(path, frag) {
			return true
		}
	}
	return false
}

// classify derives (action, resourceType, resourceID) from the request method
// and path. Best-effort: the raw method/path are stored too.
func classify(method, path string) (action, resourceType string, resourceID *int64) {
	p := strings.TrimPrefix(path, "/api/v1/")
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) > 0 {
		resourceType = segs[0]
	}
	// Last numeric segment is the most specific resource id.
	for _, s := range segs {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			id := n
			resourceID = &id
		}
	}
	switch {
	case strings.HasSuffix(path, "/launch"):
		action = "launch"
	case strings.HasSuffix(path, "/sync"):
		action = "sync"
	case strings.HasSuffix(path, "/approve"):
		action = "approve"
	case strings.HasSuffix(path, "/deny"):
		action = "deny"
	case method == http.MethodPost:
		action = "create"
	case method == http.MethodPut, method == http.MethodPatch:
		action = "update"
	case method == http.MethodDelete:
		action = "delete"
	}
	return action, resourceType, resourceID
}

// ActivityCapture records every successful mutating request (who, what, when)
// into activity_stream. Reads/queries and machine endpoints are ignored. The
// insert is async so it never adds latency to the request.
func ActivityCapture(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutating := r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodPatch || r.Method == http.MethodDelete
			if !mutating || skipActivity(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)

			if sw.status < 200 || sw.status >= 300 {
				return // only successful mutations are audited
			}
			uc, _ := r.Context().Value(UserContextKey).(UserContext)
			action, resourceType, resourceID := classify(r.Method, r.URL.Path)

			go func(uid int64, uname, act, rtype string, rid *int64, method, path string, code int) {
				_, _ = db.ExecContext(context.Background(), `
					INSERT INTO activity_stream (user_id, username, action, resource_type, resource_id, method, path, status_code)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
					nullableInt(uid), uname, act, rtype, rid, method, path, code)
			}(uc.UserID, uc.Username, action, resourceType, resourceID, r.Method, r.URL.Path, sw.status)
		})
	}
}

func nullableInt(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}
