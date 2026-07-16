package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireHuman(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	tests := []struct {
		name      string
		principal *UserContext
		want      int
	}{
		{"human", &UserContext{Kind: HumanPrincipal, UserID: 7}, http.StatusNoContent},
		{"service", &UserContext{Kind: ServicePrincipal, ServicePrincipalID: 9}, http.StatusForbidden},
		{"missing", nil, http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.principal != nil {
				req = req.WithContext(context.WithValue(req.Context(), UserContextKey, *test.principal))
			}
			rec := httptest.NewRecorder()
			RequireHuman(next).ServeHTTP(rec, req)
			if rec.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, test.want, rec.Body.String())
			}
		})
	}
}

func TestRequireService(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	tests := []struct {
		name      string
		principal *UserContext
		want      int
	}{
		{"service", &UserContext{Kind: ServicePrincipal, ServicePrincipalID: 9}, http.StatusNoContent},
		{"human", &UserContext{Kind: HumanPrincipal, UserID: 7}, http.StatusForbidden},
		{"missing", nil, http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.principal != nil {
				req = req.WithContext(context.WithValue(req.Context(), UserContextKey, *test.principal))
			}
			rec := httptest.NewRecorder()
			RequireService(next).ServeHTTP(rec, req)
			if rec.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, test.want, rec.Body.String())
			}
		})
	}
}
