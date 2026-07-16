package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/render"
)

const maxServiceCredentialLifetime = 366 * 24 * time.Hour

type servicePrincipalView struct {
	ID              int64      `db:"id" json:"id"`
	OrganizationID  int64      `db:"organization_id" json:"organization_id"`
	Name            string     `db:"name" json:"name"`
	Description     string     `db:"description" json:"description"`
	Enabled         bool       `db:"enabled" json:"enabled"`
	CreatedByUserID *int64     `db:"created_by_user_id" json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
	DisabledAt      *time.Time `db:"disabled_at" json:"disabled_at,omitempty"`
}

type serviceCredentialView struct {
	ID                 int64      `db:"id" json:"id"`
	ServicePrincipalID int64      `db:"service_principal_id" json:"service_principal_id"`
	Name               string     `db:"name" json:"name"`
	ExpiresAt          time.Time  `db:"expires_at" json:"expires_at"`
	LastUsedAt         *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
	CreatedByUserID    *int64     `db:"created_by_user_id" json:"created_by_user_id,omitempty"`
	CreatedAt          time.Time  `db:"created_at" json:"created_at"`
	RevokedAt          *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
}

// ServicePrincipalsResource administers non-human identities. All routes are
// mounted behind human authentication; every operation additionally requires
// manage permission on the owning organization.
type ServicePrincipalsResource struct {
	DB *sqlx.DB
	*Authorizer
	now func() time.Time
}

func NewServicePrincipalsResource(db *sqlx.DB, authz *Authorizer) *ServicePrincipalsResource {
	return &ServicePrincipalsResource{DB: db, Authorizer: authz, now: time.Now}
}

func (rs *ServicePrincipalsResource) OrganizationRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.List)
	r.Post("/", rs.Create)
	return r
}

func (rs *ServicePrincipalsResource) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{id}", rs.Get)
	r.Patch("/{id}", rs.Update)
	r.Delete("/{id}", rs.Disable)
	r.Get("/{id}/credentials", rs.ListCredentials)
	r.Post("/{id}/credentials", rs.CreateCredential)
	r.Post("/{id}/credentials/{credentialID}/rotate", rs.RotateCredential)
	r.Delete("/{id}/credentials/{credentialID}", rs.RevokeCredential)
	r.Get("/{id}/grants", rs.ListGrants)
	r.Post("/{id}/grants", rs.CreateGrant)
	r.Get("/{id}/grants/{grantID}", rs.GetGrant)
	r.Put("/{id}/grants/{grantID}", rs.UpdateGrant)
	r.Delete("/{id}/grants/{grantID}", rs.RevokeGrant)
	return r
}

func (rs *ServicePrincipalsResource) organizationID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

func (rs *ServicePrincipalsResource) principalOrganization(ctx context.Context, id int64) (int64, error) {
	var organizationID int64
	err := rs.DB.GetContext(ctx, &organizationID, `SELECT organization_id FROM service_principals WHERE id=$1`, id)
	return organizationID, err
}

func (rs *ServicePrincipalsResource) authorizePrincipalAdmin(w http.ResponseWriter, r *http.Request, id int64) (int64, bool) {
	organizationID, err := rs.principalOrganization(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrNotFound(nil).Render(w, r)
		return 0, false
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return 0, false
	}
	if !rs.authorize(w, r, rbac.Organization, organizationID, actAdmin) {
		return 0, false
	}
	return organizationID, true
}

func (rs *ServicePrincipalsResource) List(w http.ResponseWriter, r *http.Request) {
	organizationID, err := rs.organizationID(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Organization, organizationID, actAdmin) {
		return
	}
	rows := []servicePrincipalView{}
	if err := rs.DB.SelectContext(r.Context(), &rows, `
		SELECT id, organization_id, name, description, enabled, created_by_user_id,
		       created_at, updated_at, disabled_at
		FROM service_principals WHERE organization_id=$1 ORDER BY name, id`, organizationID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

func (rs *ServicePrincipalsResource) Create(w http.ResponseWriter, r *http.Request) {
	organizationID, err := rs.organizationID(r)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.authorize(w, r, rbac.Organization, organizationID, actAdmin) {
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeStrictJSON(r, &body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		render.ErrInvalidRequest(fmt.Errorf("name is required")).Render(w, r)
		return
	}
	var out servicePrincipalView
	err = rs.DB.GetContext(r.Context(), &out, `
		INSERT INTO service_principals
		    (organization_id, name, description, created_by_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, organization_id, name, description, enabled,
		          created_by_user_id, created_at, updated_at, disabled_at`,
		organizationID, body.Name, body.Description, currentUser(r).UserID)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, out)
}

func (rs *ServicePrincipalsResource) Get(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	var out servicePrincipalView
	if err := rs.DB.GetContext(r.Context(), &out, `
		SELECT id, organization_id, name, description, enabled, created_by_user_id,
		       created_at, updated_at, disabled_at
		FROM service_principals WHERE id=$1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *ServicePrincipalsResource) Update(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Enabled     *bool   `json:"enabled"`
	}
	if err := decodeStrictJSON(r, &body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var out servicePrincipalView
	err := rs.DB.GetContext(r.Context(), &out, `
		UPDATE service_principals
		SET name=COALESCE(NULLIF(BTRIM($2), ''), name),
		    description=COALESCE($3, description),
		    enabled=COALESCE($4, enabled),
		    disabled_at=CASE
		        WHEN COALESCE($4, enabled) THEN NULL
		        WHEN enabled OR disabled_at IS NULL THEN now()
		        ELSE disabled_at
		    END,
		    updated_at=now()
		WHERE id=$1
		RETURNING id, organization_id, name, description, enabled,
		          created_by_user_id, created_at, updated_at, disabled_at`,
		id, body.Name, body.Description, body.Enabled)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *ServicePrincipalsResource) Disable(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	result, err := rs.DB.ExecContext(r.Context(), `
		UPDATE service_principals
		SET enabled=FALSE, disabled_at=COALESCE(disabled_at, now()), updated_at=now()
		WHERE id=$1`, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	render.NoContent(w, r)
}

func (rs *ServicePrincipalsResource) ListCredentials(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	rows := []serviceCredentialView{}
	if err := rs.DB.SelectContext(r.Context(), &rows, `
		SELECT id, service_principal_id, name, expires_at, last_used_at,
		       created_by_user_id, created_at, revoked_at
		FROM service_credentials WHERE service_principal_id=$1
		ORDER BY created_at DESC, id DESC`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

func (rs *ServicePrincipalsResource) CreateCredential(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	var body serviceCredentialInput
	if err := decodeStrictJSON(r, &body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := rs.validateCredentialInput(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	plaintext, err := mintServiceToken()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	var out serviceCredentialView
	err = rs.DB.GetContext(r.Context(), &out, `
		INSERT INTO service_credentials
		    (service_principal_id, name, token_hash, expires_at, created_by_user_id)
		SELECT id, $2, $3, $4, $5 FROM service_principals
		WHERE id=$1 AND enabled
		RETURNING id, service_principal_id, name, expires_at, last_used_at,
		          created_by_user_id, created_at, revoked_at`,
		id, body.Name, middleware.HashToken(plaintext), body.ExpiresAt.UTC(), currentUser(r).UserID)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrConflict(fmt.Errorf("service principal is disabled")).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]any{
		"id": out.ID, "service_principal_id": out.ServicePrincipalID,
		"name": out.Name, "expires_at": out.ExpiresAt, "created_at": out.CreatedAt,
		"token": plaintext,
	})
}

type serviceCredentialInput struct {
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (rs *ServicePrincipalsResource) validateCredentialInput(body *serviceCredentialInput) error {
	body.Name = strings.TrimSpace(body.Name)
	now := rs.now().UTC()
	if body.Name == "" || !body.ExpiresAt.After(now) || body.ExpiresAt.After(now.Add(maxServiceCredentialLifetime)) {
		return fmt.Errorf("name and an expiry within 366 days are required")
	}
	return nil
}

func mintServiceToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return middleware.ServiceTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// RotateCredential atomically revokes one active credential and creates its
// replacement. A failed insert rolls the revocation back, avoiding lockout.
func (rs *ServicePrincipalsResource) RotateCredential(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	credentialID, err := strconv.ParseInt(chi.URLParam(r, "credentialID"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var body serviceCredentialInput
	if err := decodeStrictJSON(r, &body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := rs.validateCredentialInput(&body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	plaintext, err := mintServiceToken()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	tx, err := rs.DB.BeginTxx(r.Context(), nil)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `
		UPDATE service_credentials SET revoked_at=now()
		WHERE id=$1 AND service_principal_id=$2 AND revoked_at IS NULL`, credentialID, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	var out serviceCredentialView
	err = tx.GetContext(r.Context(), &out, `
		INSERT INTO service_credentials
		    (service_principal_id, name, token_hash, expires_at, created_by_user_id)
		SELECT id, $2, $3, $4, $5 FROM service_principals
		WHERE id=$1 AND enabled
		RETURNING id, service_principal_id, name, expires_at, last_used_at,
		          created_by_user_id, created_at, revoked_at`,
		id, body.Name, middleware.HashToken(plaintext), body.ExpiresAt.UTC(), currentUser(r).UserID)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrConflict(fmt.Errorf("service principal is disabled")).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]any{
		"id": out.ID, "service_principal_id": out.ServicePrincipalID,
		"name": out.Name, "expires_at": out.ExpiresAt, "created_at": out.CreatedAt,
		"token": plaintext, "replaces_credential_id": credentialID,
	})
}

func (rs *ServicePrincipalsResource) RevokeCredential(w http.ResponseWriter, r *http.Request) {
	id := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, id); !ok {
		return
	}
	credentialID, err := strconv.ParseInt(chi.URLParam(r, "credentialID"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	result, err := rs.DB.ExecContext(r.Context(), `
		UPDATE service_credentials SET revoked_at=COALESCE(revoked_at, now())
		WHERE id=$1 AND service_principal_id=$2`, credentialID, id)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	render.NoContent(w, r)
}
