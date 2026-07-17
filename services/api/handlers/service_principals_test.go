package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/handlers"
	"github.com/praetordev/praetor/services/api/middleware"
)

func TestServicePrincipalAdministration(t *testing.T) {
	db := rbacTestDB(t)
	defer db.Close()
	resource := handlers.NewServicePrincipalsResource(db, handlers.NewAuthorizer(db))
	access := rbac.NewStore(db, testResourceTables)

	uniq := time.Now().UnixNano()
	orgID := createOrg(t, db, fmt.Sprintf("service-principal-org-%d", uniq))
	adminID := createUser(t, db, fmt.Sprintf("service-principal-admin-%d", uniq))
	outsiderID := createUser(t, db, fmt.Sprintf("service-principal-outsider-%d", uniq))
	grantObjectRole(t, access, rbac.Organization, orgID, rbac.AdminRole, adminID)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, adminID, outsiderID)
	})

	admin := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: adminID}
	outsider := middleware.UserContext{Kind: middleware.HumanPrincipal, UserID: outsiderID}
	orgParams := map[string]string{"id": fmt.Sprint(orgID)}

	rec := callJSON(t, resource.Create, http.MethodPost,
		`{"name":"deployment-portal","description":"restricted integration"}`, admin, orgParams)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create principal: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var principal struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &principal); err != nil || principal.ID == 0 {
		t.Fatalf("decode principal: %v (%s)", err, rec.Body)
	}
	if principal.Name != "deployment-portal" || !principal.Enabled {
		t.Fatalf("unexpected principal: %+v", principal)
	}

	rec = callJSON(t, resource.List, http.MethodGet, "", outsider, orgParams)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list: want 403, got %d", rec.Code)
	}

	principalParams := map[string]string{"id": fmt.Sprint(principal.ID)}
	rec = callJSON(t, resource.CreateCredential, http.MethodPost,
		`{"name":"missing-expiry"}`, admin, principalParams)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("credential without expiry: want 400, got %d (%s)", rec.Code, rec.Body)
	}

	expiry := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	rec = callJSON(t, resource.CreateCredential, http.MethodPost,
		fmt.Sprintf(`{"name":"primary","expires_at":%q}`, expiry), admin, principalParams)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create credential: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var credential struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &credential); err != nil || credential.ID == 0 {
		t.Fatalf("decode credential: %v (%s)", err, rec.Body)
	}
	if !strings.HasPrefix(credential.Token, middleware.ServiceTokenPrefix) {
		t.Fatalf("credential prefix=%q", credential.Token)
	}

	rec = callJSON(t, resource.ListCredentials, http.MethodGet, "", admin, principalParams)
	if rec.Code != http.StatusOK {
		t.Fatalf("list credentials: want 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), credential.Token) || strings.Contains(rec.Body.String(), "token_hash") {
		t.Fatalf("credential list exposed secret material: %s", rec.Body)
	}

	rotateParams := map[string]string{
		"id":           fmt.Sprint(principal.ID),
		"credentialID": fmt.Sprint(credential.ID),
	}
	rec = callJSON(t, resource.RotateCredential, http.MethodPost,
		fmt.Sprintf(`{"name":"rotated","expires_at":%q}`, expiry), admin, rotateParams)
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate credential: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	var rotated struct {
		ID                   int64  `json:"id"`
		Token                string `json:"token"`
		ReplacesCredentialID int64  `json:"replaces_credential_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rotated); err != nil || rotated.ID == 0 {
		t.Fatalf("decode rotated credential: %v (%s)", err, rec.Body)
	}
	if rotated.ReplacesCredentialID != credential.ID || !strings.HasPrefix(rotated.Token, middleware.ServiceTokenPrefix) {
		t.Fatalf("unexpected rotation response: %+v", rotated)
	}
	var oldRevoked bool
	if err := db.Get(&oldRevoked, `SELECT revoked_at IS NOT NULL FROM service_credentials WHERE id=$1`, credential.ID); err != nil || !oldRevoked {
		t.Fatalf("old credential was not revoked atomically: revoked=%v err=%v", oldRevoked, err)
	}
	var newActive bool
	if err := db.Get(&newActive, `SELECT revoked_at IS NULL FROM service_credentials WHERE id=$1`, rotated.ID); err != nil || !newActive {
		t.Fatalf("replacement credential is not active: active=%v err=%v", newActive, err)
	}

	revokeParams := map[string]string{
		"id":           fmt.Sprint(principal.ID),
		"credentialID": fmt.Sprint(rotated.ID),
	}
	rec = callJSON(t, resource.RevokeCredential, http.MethodDelete, "", admin, revokeParams)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke credential: want 204, got %d (%s)", rec.Code, rec.Body)
	}

	rec = callJSON(t, resource.Disable, http.MethodDelete, "", admin, principalParams)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("disable principal: want 204, got %d (%s)", rec.Code, rec.Body)
	}
	rec = callJSON(t, resource.CreateCredential, http.MethodPost,
		fmt.Sprintf(`{"name":"after-disable","expires_at":%q}`, expiry), admin, principalParams)
	if rec.Code != http.StatusConflict {
		t.Fatalf("credential for disabled principal: want 409, got %d (%s)", rec.Code, rec.Body)
	}
}
