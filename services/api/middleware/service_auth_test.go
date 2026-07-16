package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestServiceCredentialAuthentication(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	defer db.Close()

	uniq := time.Now().UnixNano()
	var orgID, userID, principalID, credentialID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, fmt.Sprintf("service-auth-org-%d", uniq)).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO users (username, password_hash, email, is_active)
		VALUES ($1, 'x', $2, TRUE) RETURNING id`,
		fmt.Sprintf("service-auth-user-%d", uniq), fmt.Sprintf("service-auth-%d@example.com", uniq)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO service_principals (organization_id, name, created_by_user_id)
		VALUES ($1, $2, $3) RETURNING id`, orgID, fmt.Sprintf("service-auth-%d", uniq), userID).Scan(&principalID); err != nil {
		t.Fatal(err)
	}
	token := ServiceTokenPrefix + strings.Repeat("a", 43)
	if err := db.QueryRow(`
		INSERT INTO service_credentials
		    (service_principal_id, name, token_hash, expires_at, created_by_user_id)
		VALUES ($1, 'test', $2, now()+interval '1 hour', $3) RETURNING id`,
		principalID, HashToken(token), userID).Scan(&credentialID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})

	var got UserContext
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Context().Value(UserContextKey).(UserContext)
		w.WriteHeader(http.StatusNoContent)
	})
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		AuthMiddleware(db)(next).ServeHTTP(rec, req)
		return rec
	}

	if rec := request(); rec.Code != http.StatusNoContent {
		t.Fatalf("authenticate service credential: want 204, got %d (%s)", rec.Code, rec.Body)
	}
	if got.Kind != ServicePrincipal || got.ServicePrincipalID != principalID ||
		got.ServiceCredentialID != credentialID || got.OrganizationID != orgID || got.UserID != 0 {
		t.Fatalf("unexpected service context: %+v", got)
	}

	if _, err := db.ExecContext(context.Background(),
		`UPDATE service_credentials SET revoked_at=now() WHERE id=$1`, credentialID); err != nil {
		t.Fatal(err)
	}
	if rec := request(); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked service credential: want 401, got %d", rec.Code)
	}
}
