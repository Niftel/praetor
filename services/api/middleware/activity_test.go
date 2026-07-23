package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type activityExecutorFunc func(context.Context, string, ...interface{}) (sql.Result, error)

func (f activityExecutorFunc) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return f(ctx, query, args...)
}

func activityRequest(t *testing.T, recorder *ActivityRecorder, requestContext context.Context, handler http.HandlerFunc) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/job-templates/42/launch", nil)
	request = request.WithContext(context.WithValue(requestContext, UserContextKey, UserContext{UserID: 7, Username: "operator"}))
	recorder.Middleware(handler).ServeHTTP(httptest.NewRecorder(), request)
}

func closeActivityRecorder(t *testing.T, recorder *ActivityRecorder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestActivityRecorderPersistsAfterRequestCancellation(t *testing.T) {
	called := make(chan []interface{}, 1)
	executor := activityExecutorFunc(func(ctx context.Context, _ string, args ...interface{}) (sql.Result, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		called <- args
		return nil, nil
	})
	recorder := newActivityRecorder(context.Background(), executor, time.Second, t.Logf)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	activityRequest(t, recorder, requestCtx, func(w http.ResponseWriter, _ *http.Request) {
		cancelRequest()
		w.WriteHeader(http.StatusNoContent)
	})

	select {
	case args := <-called:
		resourceID, ok := args[7].(*int64)
		if args[0] != int64(7) || args[1] != "operator" || args[2] != HumanPrincipal ||
			args[5] != "launch" || !ok || *resourceID != 42 || args[12] != "success" {
			t.Fatalf("unexpected audit args: %#v", args)
		}
	case <-time.After(time.Second):
		t.Fatal("audit write did not survive request cancellation")
	}
	closeActivityRecorder(t, recorder)
}

func TestActivityRecorderTimesOutAndReportsFailure(t *testing.T) {
	logs := make(chan string, 1)
	executor := activityExecutorFunc(func(ctx context.Context, _ string, _ ...interface{}) (sql.Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	recorder := newActivityRecorder(context.Background(), executor, 20*time.Millisecond, func(format string, args ...interface{}) {
		logs <- format
	})
	activityRequest(t, recorder, context.Background(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	select {
	case message := <-logs:
		if !strings.Contains(message, "timed out") {
			t.Fatalf("timeout was not reported: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed-out audit write was not reported")
	}
	closeActivityRecorder(t, recorder)
}

func TestActivityRecorderShutdownCancelsAndDrainsWorkers(t *testing.T) {
	started := make(chan struct{}, 1)
	var active atomic.Int64
	executor := activityExecutorFunc(func(ctx context.Context, _ string, _ ...interface{}) (sql.Result, error) {
		active.Add(1)
		defer active.Add(-1)
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var logMu sync.Mutex
	var logs []string
	recorder := newActivityRecorder(context.Background(), executor, time.Minute, func(format string, _ ...interface{}) {
		logMu.Lock()
		logs = append(logs, format)
		logMu.Unlock()
	})
	activityRequest(t, recorder, context.Background(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("audit worker did not start")
	}
	closeActivityRecorder(t, recorder)
	if got := active.Load(); got != 0 {
		t.Fatalf("active audit workers after shutdown = %d", got)
	}

	activityRequest(t, recorder, context.Background(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	time.Sleep(10 * time.Millisecond)
	if got := active.Load(); got != 0 {
		t.Fatalf("audit worker started after shutdown: %d", got)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if !containsLog(logs, "failed") || !containsLog(logs, "dropped after recorder shutdown") {
		t.Fatalf("shutdown failures were not observable: %v", logs)
	}
}

func TestActivityRecorderReportsDatabaseFailure(t *testing.T) {
	logs := make(chan string, 1)
	executor := activityExecutorFunc(func(context.Context, string, ...interface{}) (sql.Result, error) {
		return nil, errors.New("database unavailable")
	})
	recorder := newActivityRecorder(context.Background(), executor, time.Second, func(format string, _ ...interface{}) {
		logs <- format
	})
	activityRequest(t, recorder, context.Background(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	select {
	case message := <-logs:
		if !strings.Contains(message, "write failed") {
			t.Fatalf("database failure was not reported: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("database failure was not reported")
	}
	closeActivityRecorder(t, recorder)
}

func TestActivityRecorderReportsUnavailableExecutor(t *testing.T) {
	logs := make(chan string, 1)
	recorder := newActivityRecorder(context.Background(), nil, time.Second, func(format string, _ ...interface{}) {
		logs <- format
	})
	activityRequest(t, recorder, context.Background(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	select {
	case message := <-logs:
		if !strings.Contains(message, "audit unavailable") {
			t.Fatalf("missing executor was not reported: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("missing executor was not reported")
	}
	closeActivityRecorder(t, recorder)
}

func TestActivityRecorderCapturesDeniedNotificationBoundary(t *testing.T) {
	called := make(chan []interface{}, 1)
	executor := activityExecutorFunc(func(_ context.Context, _ string, args ...interface{}) (sql.Result, error) {
		called <- args
		return nil, nil
	})
	recorder := newActivityRecorder(context.Background(), executor, time.Second, t.Logf)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notification-templates/91/test", strings.NewReader(`{"config":{"url":"https://secret.invalid/token"}}`))
	request = request.WithContext(context.WithValue(request.Context(), UserContextKey, UserContext{
		Kind: HumanPrincipal, UserID: 12, Username: "auditor",
	}))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetActivityMetadata(r, ActivityMetadata{
			OrganizationID: 8,
			ResourceID:     91,
			ResourceType:   "notification_template",
			Action:         "test",
			FailureCode:    "notification_admin_required",
		})
		w.WriteHeader(http.StatusForbidden)
	})
	recorder.Middleware(handler).ServeHTTP(httptest.NewRecorder(), request)

	select {
	case args := <-called:
		orgID, orgOK := args[8].(*int64)
		resourceID, resourceOK := args[7].(*int64)
		if !orgOK || *orgID != 8 || !resourceOK || *resourceID != 91 ||
			args[5] != "test" || args[6] != "notification_template" ||
			args[12] != "denied" || args[13] != "notification_admin_required" {
			t.Fatalf("unexpected denied notification audit args: %#v", args)
		}
		for _, arg := range args {
			if strings.Contains(fmt.Sprint(arg), "secret.invalid") {
				t.Fatalf("audit args exposed request secret: %#v", args)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("denied notification mutation was not audited")
	}
	closeActivityRecorder(t, recorder)
}

func TestNotificationMutationPathsIncludeLegacyAttachments(t *testing.T) {
	for _, path := range []string{
		"/api/v1/notification-templates",
		"/api/v1/notification-templates/7/test",
		"/api/v1/notification-policies/9",
		"/api/v1/job-templates/2/notifications",
		"/api/v1/workflow-templates/3/notifications/4/approval",
	} {
		if !isNotificationMutationPath(path) {
			t.Errorf("notification mutation path was not recognized: %s", path)
		}
	}
	if isNotificationMutationPath("/api/v1/workflow-templates/3/launch") {
		t.Fatal("ordinary workflow launch classified as notification mutation")
	}
}

func containsLog(logs []string, fragment string) bool {
	for _, message := range logs {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
