package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	rbac "github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/render"
)

const maxDelegatedGrantLifetime = 366 * 24 * time.Hour
const maxDelegatedScopeEntries = 10000
const maxDelegatedExtraVarKeys = 128

var extraVarKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type delegatedLaunchGrantView struct {
	ID                  int64          `db:"id" json:"id"`
	OrganizationID      int64          `db:"organization_id" json:"organization_id"`
	ServicePrincipalID  int64          `db:"service_principal_id" json:"service_principal_id"`
	WorkflowTemplateID  int64          `db:"workflow_template_id" json:"workflow_template_id"`
	InventoryID         int64          `db:"inventory_id" json:"inventory_id"`
	AllowedHostIDs      pq.Int64Array  `db:"allowed_host_ids" json:"allowed_host_ids"`
	AllowedGroupIDs     pq.Int64Array  `db:"allowed_group_ids" json:"allowed_group_ids"`
	MaxHosts            *int           `db:"max_hosts" json:"max_hosts,omitempty"`
	AllowedExtraVarKeys pq.StringArray `db:"allowed_extra_var_keys" json:"allowed_extra_var_keys"`
	ApprovalTeamID      *int64         `db:"approval_team_id" json:"approval_team_id,omitempty"`
	NotBefore           time.Time      `db:"not_before" json:"not_before"`
	ExpiresAt           time.Time      `db:"expires_at" json:"expires_at"`
	CreatedByUserID     *int64         `db:"created_by_user_id" json:"created_by_user_id,omitempty"`
	UpdatedByUserID     *int64         `db:"updated_by_user_id" json:"updated_by_user_id,omitempty"`
	CreatedAt           time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at" json:"updated_at"`
	RevokedAt           *time.Time     `db:"revoked_at" json:"revoked_at,omitempty"`
}

type delegatedLaunchGrantInput struct {
	WorkflowTemplateID  int64     `json:"workflow_template_id"`
	InventoryID         int64     `json:"inventory_id"`
	AllowedHostIDs      []int64   `json:"allowed_host_ids,omitempty"`
	AllowedGroupIDs     []int64   `json:"allowed_group_ids,omitempty"`
	MaxHosts            *int      `json:"max_hosts,omitempty"`
	AllowedExtraVarKeys []string  `json:"allowed_extra_var_keys,omitempty"`
	ApprovalTeamID      *int64    `json:"approval_team_id,omitempty"`
	NotBefore           time.Time `json:"not_before"`
	ExpiresAt           time.Time `json:"expires_at"`
}

const delegatedGrantColumns = `
	id, organization_id, service_principal_id, workflow_template_id,
	inventory_id, allowed_host_ids, allowed_group_ids, max_hosts,
	allowed_extra_var_keys, approval_team_id, not_before, expires_at,
	created_by_user_id, updated_by_user_id, created_at, updated_at, revoked_at`

func normalizePositiveIDs(values []int64) ([]int64, error) {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("resource IDs must be positive")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func normalizeExtraVarKeys(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !extraVarKeyPattern.MatchString(value) {
			return nil, fmt.Errorf("extra variable keys must be valid identifiers")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func (rs *ServicePrincipalsResource) validateGrantInput(w http.ResponseWriter, r *http.Request, organizationID int64, input *delegatedLaunchGrantInput) bool {
	var err error
	if input.WorkflowTemplateID <= 0 || input.InventoryID <= 0 {
		render.ErrInvalidRequest(fmt.Errorf("workflow_template_id and inventory_id are required")).Render(w, r)
		return false
	}
	input.AllowedHostIDs, err = normalizePositiveIDs(input.AllowedHostIDs)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return false
	}
	input.AllowedGroupIDs, err = normalizePositiveIDs(input.AllowedGroupIDs)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return false
	}
	input.AllowedExtraVarKeys, err = normalizeExtraVarKeys(input.AllowedExtraVarKeys)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return false
	}
	now := rs.now().UTC()
	if input.NotBefore.IsZero() {
		input.NotBefore = now
	} else {
		input.NotBefore = input.NotBefore.UTC()
	}
	input.ExpiresAt = input.ExpiresAt.UTC()
	if len(input.AllowedHostIDs) > maxDelegatedScopeEntries ||
		len(input.AllowedGroupIDs) > maxDelegatedScopeEntries ||
		len(input.AllowedExtraVarKeys) > maxDelegatedExtraVarKeys {
		render.ErrInvalidRequest(fmt.Errorf("delegated grant scope is too large")).Render(w, r)
		return false
	}
	if !input.ExpiresAt.After(now) ||
		!input.ExpiresAt.After(input.NotBefore) ||
		input.ExpiresAt.After(input.NotBefore.Add(maxDelegatedGrantLifetime)) {
		render.ErrInvalidRequest(fmt.Errorf("grant expiry must follow activation and be within 366 days")).Render(w, r)
		return false
	}
	if input.MaxHosts != nil && *input.MaxHosts <= 0 {
		render.ErrInvalidRequest(fmt.Errorf("max_hosts must be positive")).Render(w, r)
		return false
	}

	var resources struct {
		WorkflowOrganizationID  int64 `db:"workflow_organization_id"`
		InventoryOrganizationID int64 `db:"inventory_organization_id"`
	}
	err = rs.DB.GetContext(r.Context(), &resources, `
		SELECT wt.organization_id AS workflow_organization_id,
		       i.organization_id AS inventory_organization_id
		FROM workflow_templates wt CROSS JOIN inventories i
		WHERE wt.id=$1 AND i.id=$2`, input.WorkflowTemplateID, input.InventoryID)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrInvalidRequest(fmt.Errorf("workflow or inventory not found")).Render(w, r)
		return false
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return false
	}
	if resources.WorkflowOrganizationID != organizationID || resources.InventoryOrganizationID != organizationID {
		render.ErrInvalidRequest(fmt.Errorf("workflow and inventory must belong to the service principal organization")).Render(w, r)
		return false
	}
	if !rs.authorize(w, r, rbac.WorkflowTemplate, input.WorkflowTemplateID, actExecute) {
		return false
	}
	if !rs.authorize(w, r, rbac.Inventory, input.InventoryID, actUse) {
		return false
	}
	if input.ApprovalTeamID != nil {
		var teamOrganizationID int64
		if err := rs.DB.GetContext(r.Context(), &teamOrganizationID,
			`SELECT organization_id FROM teams WHERE id=$1`, *input.ApprovalTeamID); err != nil {
			render.ErrInvalidRequest(fmt.Errorf("approval team not found")).Render(w, r)
			return false
		}
		if teamOrganizationID != organizationID {
			render.ErrInvalidRequest(fmt.Errorf("approval team must belong to the service principal organization")).Render(w, r)
			return false
		}
		if !rs.authorize(w, r, rbac.Team, *input.ApprovalTeamID, actAdmin) {
			return false
		}
	}
	return true
}

func (rs *ServicePrincipalsResource) ListGrants(w http.ResponseWriter, r *http.Request) {
	principalID := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, principalID); !ok {
		return
	}
	rows := []delegatedLaunchGrantView{}
	if err := rs.DB.SelectContext(r.Context(), &rows, `
		SELECT `+delegatedGrantColumns+`
		FROM delegated_launch_grants WHERE service_principal_id=$1
		ORDER BY created_at DESC, id DESC`, principalID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, rows)
}

func (rs *ServicePrincipalsResource) CreateGrant(w http.ResponseWriter, r *http.Request) {
	principalID := render.GetIDParam(r)
	organizationID, ok := rs.authorizePrincipalAdmin(w, r, principalID)
	if !ok {
		return
	}
	var input delegatedLaunchGrantInput
	if err := decodeStrictJSON(r, &input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.validateGrantInput(w, r, organizationID, &input) {
		return
	}
	var out delegatedLaunchGrantView
	err := rs.DB.GetContext(r.Context(), &out, `
		INSERT INTO delegated_launch_grants
		    (organization_id, service_principal_id, workflow_template_id,
		     inventory_id, allowed_host_ids, allowed_group_ids, max_hosts,
		     allowed_extra_var_keys, approval_team_id, not_before, expires_at,
		     created_by_user_id, updated_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
		RETURNING `+delegatedGrantColumns,
		organizationID, principalID, input.WorkflowTemplateID, input.InventoryID,
		pq.Array(input.AllowedHostIDs), pq.Array(input.AllowedGroupIDs), input.MaxHosts,
		pq.Array(input.AllowedExtraVarKeys), input.ApprovalTeamID, input.NotBefore,
		input.ExpiresAt, currentUser(r).UserID)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.Created(w, r, out)
}

func (rs *ServicePrincipalsResource) grantForPrincipal(r *http.Request, principalID int64) (delegatedLaunchGrantView, error) {
	grantID, err := strconv.ParseInt(chi.URLParam(r, "grantID"), 10, 64)
	if err != nil {
		return delegatedLaunchGrantView{}, err
	}
	var out delegatedLaunchGrantView
	err = rs.DB.GetContext(r.Context(), &out, `
		SELECT `+delegatedGrantColumns+`
		FROM delegated_launch_grants WHERE id=$1 AND service_principal_id=$2`,
		grantID, principalID)
	return out, err
}

func (rs *ServicePrincipalsResource) GetGrant(w http.ResponseWriter, r *http.Request) {
	principalID := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, principalID); !ok {
		return
	}
	out, err := rs.grantForPrincipal(r, principalID)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *ServicePrincipalsResource) UpdateGrant(w http.ResponseWriter, r *http.Request) {
	principalID := render.GetIDParam(r)
	organizationID, ok := rs.authorizePrincipalAdmin(w, r, principalID)
	if !ok {
		return
	}
	existing, err := rs.grantForPrincipal(r, principalID)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrNotFound(nil).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var input delegatedLaunchGrantInput
	if err := decodeStrictJSON(r, &input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if !rs.validateGrantInput(w, r, organizationID, &input) {
		return
	}
	if existing.RevokedAt != nil {
		render.ErrConflict(fmt.Errorf("revoked grants cannot be updated")).Render(w, r)
		return
	}
	var out delegatedLaunchGrantView
	err = rs.DB.GetContext(r.Context(), &out, `
		UPDATE delegated_launch_grants SET
		    workflow_template_id=$3, inventory_id=$4, allowed_host_ids=$5,
		    allowed_group_ids=$6, max_hosts=$7, allowed_extra_var_keys=$8,
		    approval_team_id=$9, not_before=$10, expires_at=$11,
		    updated_by_user_id=$12, updated_at=now()
		WHERE id=$1 AND service_principal_id=$2 AND revoked_at IS NULL
		RETURNING `+delegatedGrantColumns,
		existing.ID, principalID, input.WorkflowTemplateID, input.InventoryID,
		pq.Array(input.AllowedHostIDs), pq.Array(input.AllowedGroupIDs), input.MaxHosts,
		pq.Array(input.AllowedExtraVarKeys), input.ApprovalTeamID, input.NotBefore,
		input.ExpiresAt, currentUser(r).UserID)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *ServicePrincipalsResource) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	principalID := render.GetIDParam(r)
	if _, ok := rs.authorizePrincipalAdmin(w, r, principalID); !ok {
		return
	}
	grantID, err := strconv.ParseInt(chi.URLParam(r, "grantID"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	result, err := rs.DB.ExecContext(r.Context(), `
		UPDATE delegated_launch_grants
		SET revoked_at=COALESCE(revoked_at, now()), updated_by_user_id=$3, updated_at=now()
		WHERE id=$1 AND service_principal_id=$2`, grantID, principalID, currentUser(r).UserID)
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
