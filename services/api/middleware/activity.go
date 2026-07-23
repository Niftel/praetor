package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

const defaultActivityWriteTimeout = 2 * time.Second

type activityExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

// ActivityRecorder owns the detached, bounded writes performed by
// ActivityCapture. Close prevents new work, cancels active writes, and waits for
// them to finish so API shutdown cannot leave audit goroutines behind.
type ActivityRecorder struct {
	executor activityExecutor
	timeout  time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	logf     func(string, ...interface{})

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// NewActivityRecorder creates a service-owned recorder. Its context is detached
// from individual requests so a successful mutation can still be audited after
// the response completes, but every write has timeout as an upper bound.
func NewActivityRecorder(parent context.Context, db *sqlx.DB, timeout time.Duration) *ActivityRecorder {
	return newActivityRecorder(parent, db, timeout, log.Printf)
}

func newActivityRecorder(parent context.Context, executor activityExecutor, timeout time.Duration, logf func(string, ...interface{})) *ActivityRecorder {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultActivityWriteTimeout
	}
	ctx, cancel := context.WithCancel(parent)
	return &ActivityRecorder{executor: executor, timeout: timeout, ctx: ctx, cancel: cancel, logf: logf}
}

// Close stops and drains asynchronous audit writes until ctx expires.
func (a *ActivityRecorder) Close(ctx context.Context) error {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		a.cancel()
	}
	a.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("drain activity recorder: %w", ctx.Err())
	}
}

type activityRecord struct {
	userID              int64
	username            string
	principalKind       PrincipalKind
	servicePrincipalID  int64
	serviceCredentialID int64
	action              string
	resourceType        string
	resourceID          *int64
	organizationID      *int64
	method              string
	path                string
	statusCode          int
	outcome             string
	failureCode         string
}

type activityMetadataKey struct{}

// ActivityMetadata lets a handler enrich the request audit record without
// exposing request bodies or secret-bearing configuration. The recorder owns
// the value and handlers may only supply bounded identifiers and reason codes.
type ActivityMetadata struct {
	OrganizationID int64
	ResourceID     int64
	ResourceType   string
	Action         string
	FailureCode    string
}

// SetActivityMetadata enriches the current mutation's audit record. It is a
// no-op outside ActivityRecorder middleware.
func SetActivityMetadata(r *http.Request, metadata ActivityMetadata) {
	current, _ := r.Context().Value(activityMetadataKey{}).(*ActivityMetadata)
	if current == nil {
		return
	}
	if metadata.OrganizationID > 0 {
		current.OrganizationID = metadata.OrganizationID
	}
	if metadata.ResourceID > 0 {
		current.ResourceID = metadata.ResourceID
	}
	if metadata.ResourceType != "" {
		current.ResourceType = metadata.ResourceType
	}
	if metadata.Action != "" {
		current.Action = metadata.Action
	}
	if metadata.FailureCode != "" {
		current.FailureCode = metadata.FailureCode
	}
}

func (a *ActivityRecorder) record(record activityRecord) {
	if a.executor == nil {
		a.logf("activity audit unavailable: method=%s path=%s", record.method, record.path)
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		a.logf("activity audit dropped after recorder shutdown: method=%s path=%s", record.method, record.path)
		return
	}
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(a.ctx, a.timeout)
		defer cancel()
		_, err := a.executor.ExecContext(ctx, `
			INSERT INTO activity_stream (
			    user_id, username, principal_kind, service_principal_id,
			    service_credential_id, action, resource_type, resource_id,
			    organization_id, method, path, status_code, outcome, failure_code
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,''))`,
			nullableInt(record.userID), record.username, record.principalKind,
			nullableInt(record.servicePrincipalID), nullableInt(record.serviceCredentialID),
			record.action, record.resourceType, record.resourceID, record.organizationID,
			record.method, record.path, record.statusCode, record.outcome, record.failureCode)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				a.logf("activity audit write timed out: method=%s path=%s", record.method, record.path)
				return
			}
			a.logf("activity audit write failed: method=%s path=%s error=%v", record.method, record.path, err)
		}
	}()
}

// statusWriter captures the response status code so the activity recorder can
// distinguish successful, denied, and failed mutations.
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

func isNotificationMutationPath(path string) bool {
	return strings.Contains(path, "/notification-templates") ||
		strings.Contains(path, "/notification-policies") ||
		strings.Contains(path, "/notifications")
}

// Middleware records every successful mutating request (who, what, when) into
// activity_stream. Notification mutations additionally retain denied and failed
// outcomes because those security boundaries are operator-visible evidence.
// Reads/queries and high-volume machine endpoints are ignored.
func (a *ActivityRecorder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutating := r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodPatch || r.Method == http.MethodDelete
		if !mutating || skipActivity(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		metadata := &ActivityMetadata{}
		r = r.WithContext(context.WithValue(r.Context(), activityMetadataKey{}, metadata))
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		success := sw.status >= 200 && sw.status < 300
		notificationMutation := isNotificationMutationPath(r.URL.Path)
		if !success && !notificationMutation {
			return
		}
		uc, _ := r.Context().Value(UserContextKey).(UserContext)
		if uc.Kind == "" {
			uc.Kind = HumanPrincipal
		}
		action, resourceType, resourceID := classify(r.Method, r.URL.Path)
		if metadata.Action != "" {
			action = metadata.Action
		}
		if metadata.ResourceType != "" {
			resourceType = metadata.ResourceType
		}
		if metadata.ResourceID > 0 {
			id := metadata.ResourceID
			resourceID = &id
		}
		var organizationID *int64
		if metadata.OrganizationID > 0 {
			id := metadata.OrganizationID
			organizationID = &id
		}
		outcome := "success"
		if !success {
			outcome = "failed"
			if sw.status == http.StatusUnauthorized || sw.status == http.StatusForbidden {
				outcome = "denied"
			}
		}
		failureCode := metadata.FailureCode
		if !success && failureCode == "" {
			failureCode = activityFailureCode(sw.status)
		}
		a.record(activityRecord{
			userID: uc.UserID, username: uc.Username, principalKind: uc.Kind,
			servicePrincipalID: uc.ServicePrincipalID, serviceCredentialID: uc.ServiceCredentialID,
			action: action, resourceType: resourceType, resourceID: resourceID,
			organizationID: organizationID, method: r.Method, path: r.URL.Path,
			statusCode: sw.status, outcome: outcome, failureCode: failureCode,
		})
	})
}

func activityFailureCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authentication_required"
	case http.StatusForbidden:
		return "permission_denied"
	case http.StatusNotFound:
		return "resource_not_found"
	case http.StatusBadGateway:
		return "delivery_failed"
	default:
		return "request_failed"
	}
}

func nullableInt(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}
