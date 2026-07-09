package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

// TestUserCreateLogin verifies the create-user/login loop that was previously
// broken (CreateUser stored a fixed placeholder hash, so API-created users
// could never authenticate). It also covers the missing-password rejection,
// wrong-password rejection, and admin password reset via UpdateUser.
func TestUserCreateLogin(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	users := handlers.NewUsersResource(db)
	auth := handlers.NewAuthResource(db)

	uname := fmt.Sprintf("logintest-%d", time.Now().UnixNano())
	super := middleware.UserContext{UserID: 1, IsSuperuser: true}
	anon := middleware.UserContext{}

	// Missing password is rejected.
	rec := callJSON(t, users.CreateUser, http.MethodPost, fmt.Sprintf(`{"username":%q}`, uname), super, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create without password: want 400, got %d", rec.Code)
	}

	// Create with a real password.
	rec = callJSON(t, users.CreateUser, http.MethodPost,
		fmt.Sprintf(`{"username":%q,"password":"s3cret-pw","email":"lt@example.com","is_active":true}`, uname), super, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	uid := extractID(t, rec.Body.String())
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, uid) })

	// The created user can now log in (this is what was broken).
	rec = callJSON(t, auth.Login, http.MethodPost, fmt.Sprintf(`{"username":%q,"password":"s3cret-pw"}`, uname), anon, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("login with correct password: want 200+token, got %d (%s)", rec.Code, rec.Body)
	}

	// Wrong password is rejected.
	rec = callJSON(t, auth.Login, http.MethodPost, fmt.Sprintf(`{"username":%q,"password":"nope"}`, uname), anon, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with wrong password: want 401, got %d", rec.Code)
	}

	// Admin resets the password via UpdateUser; the new password works.
	rec = callJSON(t, users.UpdateUser, http.MethodPut,
		`{"password":"reset-pw","is_active":true,"email":"lt@example.com"}`, super, map[string]string{"id": fmt.Sprint(uid)})
	if rec.Code != http.StatusOK {
		t.Fatalf("password reset: want 200, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, auth.Login, http.MethodPost, fmt.Sprintf(`{"username":%q,"password":"reset-pw"}`, uname), anon, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login after reset: want 200, got %d", rec.Code)
	}
}
