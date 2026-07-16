package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/praetordev/launch"
	"github.com/praetordev/praetor/services/api/middleware"
	"github.com/praetordev/render"
)

var delegatedHostNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var externalRequesterPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:/-]{0,254}$`)

type DelegatedLaunchResource struct {
	DB *sqlx.DB
}

func NewDelegatedLaunchResource(db *sqlx.DB) *DelegatedLaunchResource {
	return &DelegatedLaunchResource{DB: db}
}

type delegatedLaunchRequest struct {
	ExternalRequester string                 `json:"external_requester"`
	InventoryID       int64                  `json:"inventory_id"`
	HostIDs           []int64                `json:"host_ids"`
	ExtraVars         map[string]interface{} `json:"extra_vars,omitempty"`
}

type activeDelegatedGrant struct {
	ID                  int64          `db:"id"`
	InventoryID         int64          `db:"inventory_id"`
	AllowedHostIDs      pq.Int64Array  `db:"allowed_host_ids"`
	AllowedGroupIDs     pq.Int64Array  `db:"allowed_group_ids"`
	MaxHosts            *int           `db:"max_hosts"`
	AllowedExtraVarKeys pq.StringArray `db:"allowed_extra_var_keys"`
	ApprovalTeamID      *int64         `db:"approval_team_id"`
}

type delegatedHost struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

func (rs *DelegatedLaunchResource) LaunchWorkflow(w http.ResponseWriter, r *http.Request) {
	workflowID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || workflowID <= 0 {
		render.ErrInvalidRequest(fmt.Errorf("invalid workflow id")).Render(w, r)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		render.ErrInvalidRequest(fmt.Errorf("a valid Idempotency-Key is required")).Render(w, r)
		return
	}
	var body delegatedLaunchRequest
	if err := decodeStrictJSON(r, &body); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	body.ExternalRequester = strings.TrimSpace(body.ExternalRequester)
	if !externalRequesterPattern.MatchString(body.ExternalRequester) || body.InventoryID <= 0 {
		render.ErrInvalidRequest(fmt.Errorf("external_requester and inventory_id are required")).Render(w, r)
		return
	}
	body.HostIDs, err = normalizePositiveIDs(body.HostIDs)
	if err != nil || len(body.HostIDs) == 0 || len(body.HostIDs) > maxDelegatedScopeEntries {
		render.ErrInvalidRequest(fmt.Errorf("one or more valid host_ids are required")).Render(w, r)
		return
	}

	principal := r.Context().Value(middleware.UserContextKey).(middleware.UserContext)
	tx, err := rs.DB.BeginTxx(r.Context(), nil)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()

	var grant activeDelegatedGrant
	err = tx.GetContext(r.Context(), &grant, `
		SELECT id, inventory_id, allowed_host_ids, allowed_group_ids, max_hosts,
		       allowed_extra_var_keys, approval_team_id
		FROM delegated_launch_grants
		WHERE service_principal_id=$1
		  AND workflow_template_id=$2
		  AND inventory_id=$3
		  AND organization_id=$4
		  AND revoked_at IS NULL
		  AND not_before <= now()
		  AND expires_at > now()
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR UPDATE`,
		principal.ServicePrincipalID, workflowID, body.InventoryID, principal.OrganizationID)
	if errors.Is(err, sql.ErrNoRows) {
		render.ErrForbidden(fmt.Errorf("no active delegated launch grant")).Render(w, r)
		return
	}
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	if len(body.ExtraVars) > len(grant.AllowedExtraVarKeys) {
		render.ErrForbidden(fmt.Errorf("launch variable outside grant")).Render(w, r)
		return
	}
	allowedVars := make(map[string]struct{}, len(grant.AllowedExtraVarKeys))
	for _, key := range grant.AllowedExtraVarKeys {
		allowedVars[key] = struct{}{}
	}
	for key := range body.ExtraVars {
		if _, ok := allowedVars[key]; !ok {
			render.ErrForbidden(fmt.Errorf("launch variable outside grant")).Render(w, r)
			return
		}
	}
	if grant.MaxHosts != nil && len(body.HostIDs) > *grant.MaxHosts {
		render.ErrForbidden(fmt.Errorf("requested host count exceeds grant")).Render(w, r)
		return
	}

	hosts := []delegatedHost{}
	if err := tx.SelectContext(r.Context(), &hosts, `
		SELECT id, name FROM hosts
		WHERE inventory_id=$1 AND enabled AND id=ANY($2)
		ORDER BY id`, body.InventoryID, pq.Array(body.HostIDs)); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if len(hosts) != len(body.HostIDs) {
		render.ErrForbidden(fmt.Errorf("host outside inventory or disabled")).Render(w, r)
		return
	}

	allowedHosts := make(map[int64]struct{}, len(grant.AllowedHostIDs))
	for _, id := range grant.AllowedHostIDs {
		allowedHosts[id] = struct{}{}
	}
	if len(grant.AllowedGroupIDs) > 0 {
		var groupHostIDs []int64
		if err := tx.SelectContext(r.Context(), &groupHostIDs, `
			SELECT DISTINCT hg.host_id FROM host_groups hg
			JOIN hosts h ON h.id=hg.host_id
			WHERE hg.group_id=ANY($1) AND h.inventory_id=$2`,
			pq.Array(grant.AllowedGroupIDs), body.InventoryID); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		for _, id := range groupHostIDs {
			allowedHosts[id] = struct{}{}
		}
	}
	if len(grant.AllowedHostIDs) > 0 || len(grant.AllowedGroupIDs) > 0 {
		for _, host := range hosts {
			if _, ok := allowedHosts[host.ID]; !ok {
				render.ErrForbidden(fmt.Errorf("host outside delegated scope")).Render(w, r)
				return
			}
		}
	}

	hostNames := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if !delegatedHostNamePattern.MatchString(host.Name) {
			render.ErrConflict(fmt.Errorf("host name cannot be represented safely as an Ansible limit")).Render(w, r)
			return
		}
		hostNames = append(hostNames, host.Name)
	}
	sort.Strings(hostNames)
	effectiveLimit := strings.Join(hostNames, ",")

	var approvalNodes bool
	if err := tx.GetContext(r.Context(), &approvalNodes, `
		SELECT EXISTS (SELECT 1 FROM workflow_nodes
			WHERE workflow_template_id=$1 AND node_type='approval')`, workflowID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if approvalNodes && grant.ApprovalTeamID == nil {
		render.ErrConflict(fmt.Errorf("approval workflow requires a fixed grant approval team")).Render(w, r)
		return
	}

	hashInput, _ := json.Marshal(struct {
		WorkflowID        int64                  `json:"workflow_id"`
		ExternalRequester string                 `json:"external_requester"`
		InventoryID       int64                  `json:"inventory_id"`
		HostIDs           []int64                `json:"host_ids"`
		ExtraVars         map[string]interface{} `json:"extra_vars,omitempty"`
	}{
		workflowID, body.ExternalRequester, body.InventoryID, body.HostIDs, body.ExtraVars,
	})
	sum := sha256.Sum256(hashInput)
	requestHash := hex.EncodeToString(sum[:])
	result, err := tx.ExecContext(r.Context(), `
		INSERT INTO delegated_launch_idempotency
		    (service_principal_id, idempotency_key, request_hash)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		principal.ServicePrincipalID, idempotencyKey, requestHash)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if inserted, _ := result.RowsAffected(); inserted == 0 {
		var existing struct {
			RequestHash   string `db:"request_hash"`
			WorkflowJobID *int64 `db:"workflow_job_id"`
		}
		if err := tx.GetContext(r.Context(), &existing, `
			SELECT request_hash, workflow_job_id FROM delegated_launch_idempotency
			WHERE service_principal_id=$1 AND idempotency_key=$2 FOR UPDATE`,
			principal.ServicePrincipalID, idempotencyKey); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if existing.RequestHash != requestHash || existing.WorkflowJobID == nil {
			render.ErrConflict(fmt.Errorf("idempotency key already used for a different request")).Render(w, r)
			return
		}
		render.JSON(w, r, map[string]interface{}{
			"workflow_job_id": *existing.WorkflowJobID, "status": "running", "replayed": true,
		})
		return
	}

	var allowSimultaneous bool
	if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock($1)`, workflowID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if err := tx.GetContext(r.Context(), &allowSimultaneous,
		`SELECT allow_simultaneous FROM workflow_templates WHERE id=$1`, workflowID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if !allowSimultaneous {
		var active int
		if err := tx.GetContext(r.Context(), &active, `
			SELECT count(*) FROM workflow_jobs
			WHERE workflow_template_id=$1 AND status IN ('running','pending')`, workflowID); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if active > 0 {
			render.ErrConflict(fmt.Errorf("workflow is already running")).Render(w, r)
			return
		}
	}

	wjID, err := launch.Workflow(r.Context(), tx, workflowID, launch.Options{
		ExtraVars: body.ExtraVars,
		Limit:     &effectiveLimit,
	})
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE workflow_jobs SET
		    launched_by_service_principal_id=$1,
		    launched_by_service_credential_id=$2,
		    delegated_launch_grant_id=$3,
		    delegated_external_requester=$4,
		    delegated_inventory_id=$5,
		    delegated_host_ids=$6,
		    approval_team_id=$7
		WHERE id=$8`,
		principal.ServicePrincipalID, principal.ServiceCredentialID, grant.ID,
		body.ExternalRequester, body.InventoryID, pq.Array(body.HostIDs),
		grant.ApprovalTeamID, wjID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO activity_stream
		    (username, action, resource_type, resource_id, method, path, status_code,
		     service_principal_id, service_credential_id, delegated_launch_grant_id,
		     external_requester)
		VALUES ($1,'launch','workflow_template',$2,$3,$4,201,$5,$6,$7,$8)`,
		principal.Username, workflowID, r.Method, r.URL.Path,
		principal.ServicePrincipalID, principal.ServiceCredentialID, grant.ID,
		body.ExternalRequester); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE delegated_launch_idempotency SET workflow_job_id=$1
		WHERE service_principal_id=$2 AND idempotency_key=$3`,
		wjID, principal.ServicePrincipalID, idempotencyKey); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.Created(w, r, map[string]interface{}{
		"workflow_job_id": wjID, "status": "running", "replayed": false,
	})
}
