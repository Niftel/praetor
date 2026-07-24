package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/accesscontrol"
	"github.com/praetordev/praetor/services/api/middleware"
)

const (
	maxBulkHostDeleteItems = 100
	maxBulkHostDeleteBody  = 128 << 10
	bulkHostPreviewTTL     = 5 * time.Minute
)

type bulkHostDeleteItem struct {
	Identifier string `json:"identifier,omitempty"`
	HostID     int64  `json:"host_id"`
}

type bulkHostDeletePreviewRequest struct {
	Items []bulkHostDeleteItem `json:"items"`
}

type bulkHostDeleteConfirmRequest struct {
	ConfirmationToken string `json:"confirmation_token"`
}

type bulkHostDeleteBlocker struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type bulkHostDeleteEffect struct {
	Code   string `json:"code"`
	Count  int    `json:"count"`
	Effect string `json:"effect"`
}

type bulkHostDeletePreviewResult struct {
	Index        int                     `json:"index"`
	Identifier   string                  `json:"identifier,omitempty"`
	Status       string                  `json:"status"`
	HTTPStatus   int                     `json:"http_status"`
	HostID       int64                   `json:"host_id,omitempty"`
	Name         string                  `json:"name,omitempty"`
	InventoryID  int64                   `json:"inventory_id,omitempty"`
	Blockers     []bulkHostDeleteBlocker `json:"blocking_relationships"`
	Effects      []bulkHostDeleteEffect  `json:"affected_relationships"`
	Code         string                  `json:"code,omitempty"`
	Error        string                  `json:"error,omitempty"`
	SnapshotHash string                  `json:"snapshot_hash,omitempty"`
}

type bulkHostDeletePreviewPublicResult struct {
	Index       int                     `json:"index"`
	Identifier  string                  `json:"identifier,omitempty"`
	Status      string                  `json:"status"`
	HTTPStatus  int                     `json:"http_status"`
	HostID      int64                   `json:"host_id,omitempty"`
	Name        string                  `json:"name,omitempty"`
	InventoryID int64                   `json:"inventory_id,omitempty"`
	Blockers    []bulkHostDeleteBlocker `json:"blocking_relationships"`
	Effects     []bulkHostDeleteEffect  `json:"affected_relationships"`
	Code        string                  `json:"code,omitempty"`
	Error       string                  `json:"error,omitempty"`
}

type bulkHostDeletePreviewResponse struct {
	ConfirmationToken string                              `json:"confirmation_token"`
	ExpiresAt         time.Time                           `json:"expires_at"`
	Results           []bulkHostDeletePreviewPublicResult `json:"results"`
}

type bulkHostDeleteResult struct {
	Index      int    `json:"index"`
	Identifier string `json:"identifier,omitempty"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
	HostID     int64  `json:"host_id,omitempty"`
	Code       string `json:"code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type bulkHostDeleteResponse struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Complete       bool                   `json:"complete"`
	Results        []bulkHostDeleteResult `json:"results"`
}

type bulkHostState struct {
	ID              int64     `db:"id"`
	InventoryID     int64     `db:"inventory_id"`
	Name            string    `db:"name"`
	IsRunnerHost    bool      `db:"is_runner_host"`
	ModifiedAt      time.Time `db:"modified_at"`
	DelegatedCount  int       `db:"delegated_count"`
	FactCacheCount  int       `db:"fact_cache_count"`
	GroupCount      int       `db:"group_count"`
	JobSummaryCount int       `db:"job_summary_count"`
	JobEventCount   int       `db:"job_event_count"`
	RunnerRunCount  int       `db:"runner_run_count"`
}

// PreviewBulkDeleteHosts resolves and authorizes every host before issuing a
// short-lived opaque confirmation token. Inaccessible and nonexistent hosts
// intentionally produce the same result and disclose no host attributes.
func (rs *HostsResource) PreviewBulkDeleteHosts(w http.ResponseWriter, r *http.Request) {
	var input bulkHostDeletePreviewRequest
	if !rs.decodeBulkHostDeleteBody(w, r, &input) {
		return
	}
	if len(input.Items) == 0 || len(input.Items) > maxBulkHostDeleteItems {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf(
			"items must contain between 1 and %d hosts", maxBulkHostDeleteItems,
		)))
		return
	}
	seen := make(map[int64]struct{}, len(input.Items))
	for index, item := range input.Items {
		if item.HostID <= 0 {
			rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf("items[%d].host_id must be positive", index)))
			return
		}
		if len(item.Identifier) > maxBulkItemIdentifier {
			rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf(
				"items[%d].identifier exceeds %d characters", index, maxBulkItemIdentifier,
			)))
			return
		}
		if _, duplicate := seen[item.HostID]; duplicate {
			rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf("items[%d].host_id is duplicated", index)))
			return
		}
		seen[item.HostID] = struct{}{}
	}

	results := make([]bulkHostDeletePreviewResult, 0, len(input.Items))
	for index, item := range input.Items {
		result, err := rs.previewBulkHostDeleteItem(r, index, item)
		if err != nil {
			rs.renderBulkHostError(w, r, ErrInternal(err))
			return
		}
		results = append(results, result)
	}
	token, tokenHash, err := newBulkHostConfirmationToken()
	if err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	stored, err := json.Marshal(results)
	if err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	expiresAt := time.Now().UTC().Add(bulkHostPreviewTTL)
	user := currentUser(r)
	if _, err := rs.DB.ExecContext(r.Context(), `
		INSERT INTO bulk_host_delete_previews (token_hash,user_id,items,expires_at)
		VALUES ($1,$2,$3,$4)`, tokenHash, user.UserID, string(stored), expiresAt); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}

	middleware.SetActivityMetadata(r, middleware.ActivityMetadata{
		Action: "preview_delete", ResourceType: "host",
	})
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, bulkHostDeletePreviewResponse{
		ConfirmationToken: token, ExpiresAt: expiresAt, Results: publicBulkHostDeletePreview(results),
	})
}

func (rs *HostsResource) previewBulkHostDeleteItem(
	r *http.Request, index int, item bulkHostDeleteItem,
) (bulkHostDeletePreviewResult, error) {
	result := bulkHostDeletePreviewResult{
		Index: index, Identifier: item.Identifier, Status: "rejected",
		HTTPStatus: http.StatusForbidden, Blockers: []bulkHostDeleteBlocker{},
		Effects: []bulkHostDeleteEffect{},
		Code:    "not_found_or_forbidden", Error: "host not found or deletion not permitted",
	}
	state, err := rs.bulkHostState(r.Context(), rs.DB, item.HostID, false)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	allowed, err := rs.canAuthorize(r, accesscontrol.Inventory, state.InventoryID, actAdmin)
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, nil
	}

	result.HostID = state.ID
	result.Name = state.Name
	result.InventoryID = state.InventoryID
	result.Blockers = blockersForBulkHost(state)
	result.Effects = effectsForBulkHost(state)
	result.SnapshotHash = bulkHostStateHash(state)
	result.HTTPStatus = http.StatusOK
	result.Code, result.Error = "", ""
	if len(result.Blockers) == 0 {
		result.Status = "ready"
	} else {
		result.Status = "blocked"
		result.HTTPStatus = http.StatusConflict
		result.Code = "blocking_relationships"
		result.Error = "host has relationships that must be removed before deletion"
	}
	return result, nil
}

// BulkDeleteHosts consumes a preview token through a durable idempotency ledger.
// Each ready item is re-resolved and re-authorized inside its own transaction;
// stale or newly blocked items fail closed without affecting other items.
func (rs *HostsResource) BulkDeleteHosts(w http.ResponseWriter, r *http.Request) {
	var input bulkHostDeleteConfirmRequest
	if !rs.decodeBulkHostDeleteBody(w, r, &input) {
		return
	}
	input.ConfirmationToken = strings.TrimSpace(input.ConfirmationToken)
	decodedToken, err := base64.RawURLEncoding.DecodeString(input.ConfirmationToken)
	if err != nil || len(decodedToken) != 32 {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(errors.New("confirmation_token is invalid")))
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(errors.New(
			"Idempotency-Key header is required and must be 1-128 safe characters",
		)))
		return
	}
	tokenSum := sha256.Sum256(decodedToken)
	tokenHash := hex.EncodeToString(tokenSum[:])
	user := currentUser(r)
	lockName := fmt.Sprintf("bulk-host-delete:%d:%s", user.UserID, tokenHash)

	conn, err := rs.DB.Connx(r.Context())
	if err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	defer conn.Close()
	if _, err := conn.ExecContext(r.Context(), `SELECT pg_advisory_lock(hashtextextended($1, 0))`, lockName); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	defer unlockBulkHostCreate(conn, lockName)

	var preview struct {
		Items                  json.RawMessage `db:"items"`
		ExpiresAt              time.Time       `db:"expires_at"`
		ConsumedIdempotencyKey sql.NullString  `db:"consumed_idempotency_key"`
	}
	if err := conn.GetContext(r.Context(), &preview, `
		SELECT items,expires_at,consumed_idempotency_key
		  FROM bulk_host_delete_previews
		 WHERE token_hash=$1 AND user_id=$2`, tokenHash, user.UserID); errors.Is(err, sql.ErrNoRows) {
		rs.renderBulkHostError(w, r, ErrConflict(errors.New("confirmation preview is invalid or unavailable")))
		return
	} else if err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	if time.Now().After(preview.ExpiresAt) &&
		(!preview.ConsumedIdempotencyKey.Valid || preview.ConsumedIdempotencyKey.String != idempotencyKey) {
		rs.renderBulkHostError(w, r, ErrConflict(errors.New("confirmation preview has expired")))
		return
	}
	if preview.ConsumedIdempotencyKey.Valid && preview.ConsumedIdempotencyKey.String != idempotencyKey {
		rs.renderBulkHostError(w, r, ErrConflict(errors.New("confirmation preview was already consumed")))
		return
	}

	if _, err := conn.ExecContext(r.Context(), `
		INSERT INTO bulk_host_delete_requests (user_id,idempotency_key,token_hash)
		VALUES ($1,$2,$3)
		ON CONFLICT (user_id,idempotency_key) DO NOTHING`,
		user.UserID, idempotencyKey, tokenHash); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	var stored struct {
		TokenHash string          `db:"token_hash"`
		Results   json.RawMessage `db:"results"`
		Complete  bool            `db:"complete"`
	}
	if err := conn.GetContext(r.Context(), &stored, `
		SELECT token_hash,results,complete FROM bulk_host_delete_requests
		 WHERE user_id=$1 AND idempotency_key=$2`, user.UserID, idempotencyKey); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	if stored.TokenHash != tokenHash {
		rs.renderBulkHostError(w, r, ErrConflict(errors.New(
			"idempotency key already used for a different bulk host deletion",
		)))
		return
	}
	if _, err := conn.ExecContext(r.Context(), `
		UPDATE bulk_host_delete_previews
		   SET consumed_idempotency_key=$1
		 WHERE token_hash=$2 AND user_id=$3 AND consumed_idempotency_key IS NULL`,
		idempotencyKey, tokenHash, user.UserID); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}

	var previewItems []bulkHostDeletePreviewResult
	var results []bulkHostDeleteResult
	if err := json.Unmarshal(preview.Items, &previewItems); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(errors.New("invalid stored bulk host preview")))
		return
	}
	if err := json.Unmarshal(stored.Results, &results); err != nil || len(results) > len(previewItems) {
		rs.renderBulkHostError(w, r, ErrInternal(errors.New("invalid stored bulk host deletion state")))
		return
	}
	if !stored.Complete {
		for index := len(results); index < len(previewItems); index++ {
			result, err := rs.deleteBulkHostItem(r, user, idempotencyKey, previewItems[index])
			if err != nil {
				rs.renderBulkHostError(w, r, ErrInternal(err))
				return
			}
			results = append(results, result)
		}
		if _, err := conn.ExecContext(r.Context(), `
			UPDATE bulk_host_delete_requests SET complete=TRUE,completed_at=now()
			 WHERE user_id=$1 AND idempotency_key=$2`,
			user.UserID, idempotencyKey); err != nil {
			rs.renderBulkHostError(w, r, ErrInternal(err))
			return
		}
	}
	middleware.SetActivityMetadata(r, middleware.ActivityMetadata{
		Action: "bulk_delete", ResourceType: "host",
	})
	rs.renderBulkHostDeleteResponse(w, r, idempotencyKey, results)
}

func (rs *HostsResource) deleteBulkHostItem(
	r *http.Request,
	user middleware.UserContext,
	idempotencyKey string,
	preview bulkHostDeletePreviewResult,
) (bulkHostDeleteResult, error) {
	result := bulkHostDeleteResult{
		Index: preview.Index, Identifier: preview.Identifier, Status: "rejected",
		HTTPStatus: preview.HTTPStatus, HostID: preview.HostID,
		Code: preview.Code, Error: preview.Error,
	}
	if preview.Status != "ready" {
		return result, rs.appendBulkHostDeleteResult(r.Context(), user.UserID, idempotencyKey, result)
	}
	tx, err := rs.DB.BeginTxx(r.Context(), nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	state, err := rs.bulkHostState(r.Context(), tx, preview.HostID, true)
	if errors.Is(err, sql.ErrNoRows) {
		result.HTTPStatus, result.Code, result.Error = http.StatusConflict, "already_deleted", "host was deleted after preview"
		return result, rs.appendBulkHostDeleteResult(r.Context(), user.UserID, idempotencyKey, result)
	}
	if err != nil {
		return result, err
	}
	allowed, err := rs.canAuthorize(r, accesscontrol.Inventory, state.InventoryID, actAdmin)
	if err != nil {
		return result, err
	}
	if !allowed {
		result.HostID = 0
		result.HTTPStatus, result.Code, result.Error = http.StatusForbidden, "not_found_or_forbidden", "host not found or deletion not permitted"
		return result, rs.appendBulkHostDeleteResult(r.Context(), user.UserID, idempotencyKey, result)
	}
	if bulkHostStateHash(state) != preview.SnapshotHash {
		result.HTTPStatus, result.Code, result.Error = http.StatusConflict, "stale_preview", "host or blocking relationships changed after preview"
		return result, rs.appendBulkHostDeleteResult(r.Context(), user.UserID, idempotencyKey, result)
	}

	itemResource := *rs
	itemResource.store = &transactionalHostStore{HostStore: rs.store, tx: tx}
	itemRequest := r.Clone(r.Context())
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("hostId", fmt.Sprintf("%d", state.ID))
	itemRequest = itemRequest.WithContext(context.WithValue(itemRequest.Context(), chi.RouteCtxKey, routeContext))
	recorder := newCaptureResponseWriter()
	itemResource.DeleteHost(recorder, itemRequest)
	if recorder.status != http.StatusNoContent {
		return result, fmt.Errorf("canonical host deletion returned status %d", recorder.status)
	}
	if err := insertBulkHostDeleteActivity(r.Context(), tx, r, user, state.InventoryID, state.ID); err != nil {
		return result, err
	}
	result.Status, result.HTTPStatus, result.Code, result.Error = "deleted", http.StatusNoContent, "", ""
	if err := appendBulkHostDeleteResultWith(tx, r.Context(), user.UserID, idempotencyKey, result); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

type bulkHostStateQueryer interface {
	GetContext(context.Context, interface{}, string, ...interface{}) error
}

func (rs *HostsResource) bulkHostState(
	ctx context.Context, queryer bulkHostStateQueryer, hostID int64, forUpdate bool,
) (bulkHostState, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var state bulkHostState
	err := queryer.GetContext(ctx, &state, `
		SELECT h.id,h.inventory_id,h.name,h.is_runner_host,h.modified_at,
		       (SELECT count(*) FROM delegated_launch_grants g
		         WHERE h.id=ANY(g.allowed_host_ids)) AS delegated_count,
		       (SELECT count(*) FROM host_facts f WHERE f.host_id=h.id) AS fact_cache_count,
		       (SELECT count(*) FROM host_groups hg WHERE hg.host_id=h.id) AS group_count,
		       (SELECT count(*) FROM job_host_summaries s WHERE s.host_id=h.id) AS job_summary_count,
		       (SELECT count(*) FROM job_events e WHERE e.host_id=h.id) AS job_event_count,
		       (SELECT count(*) FROM execution_runs er WHERE er.runner_host_id=h.id) AS runner_run_count
		  FROM hosts h WHERE h.id=$1`+lock, hostID)
	return state, err
}

func blockersForBulkHost(state bulkHostState) []bulkHostDeleteBlocker {
	blockers := []bulkHostDeleteBlocker{}
	if state.IsRunnerHost {
		blockers = append(blockers, bulkHostDeleteBlocker{Code: "inventory_runner", Count: 1})
	}
	if state.DelegatedCount > 0 {
		blockers = append(blockers, bulkHostDeleteBlocker{Code: "delegated_launch_grant", Count: state.DelegatedCount})
	}
	return blockers
}

func effectsForBulkHost(state bulkHostState) []bulkHostDeleteEffect {
	effects := []bulkHostDeleteEffect{}
	values := []bulkHostDeleteEffect{
		{Code: "fact_cache", Count: state.FactCacheCount, Effect: "delete"},
		{Code: "group_membership", Count: state.GroupCount, Effect: "delete"},
		{Code: "job_host_summary", Count: state.JobSummaryCount, Effect: "delete"},
		{Code: "job_event_host_reference", Count: state.JobEventCount, Effect: "detach"},
		{Code: "execution_runner_reference", Count: state.RunnerRunCount, Effect: "detach"},
	}
	for _, effect := range values {
		if effect.Count > 0 {
			effects = append(effects, effect)
		}
	}
	return effects
}

func publicBulkHostDeletePreview(items []bulkHostDeletePreviewResult) []bulkHostDeletePreviewPublicResult {
	public := make([]bulkHostDeletePreviewPublicResult, 0, len(items))
	for _, item := range items {
		public = append(public, bulkHostDeletePreviewPublicResult{
			Index: item.Index, Identifier: item.Identifier, Status: item.Status,
			HTTPStatus: item.HTTPStatus, HostID: item.HostID, Name: item.Name,
			InventoryID: item.InventoryID, Blockers: item.Blockers,
			Effects: item.Effects,
			Code:    item.Code, Error: item.Error,
		})
	}
	return public
}

func bulkHostStateHash(state bulkHostState) string {
	value, _ := json.Marshal(state)
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func newBulkHostConfirmationToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), hex.EncodeToString(sum[:]), nil
}

func (rs *HostsResource) decodeBulkHostDeleteBody(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBulkHostDeleteBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			rs.renderBulkHostError(w, r, &ErrResponse{
				Err: err, HTTPStatusCode: http.StatusRequestEntityTooLarge,
				StatusText: "Request Entity Too Large", ErrorText: "bulk host deletion payload exceeds 128 KiB",
			})
		} else {
			rs.renderBulkHostError(w, r, ErrInvalidRequest(err))
		}
		return false
	}
	if err := requireJSONEOF(decoder); err != nil {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(err))
		return false
	}
	return true
}

func (rs *HostsResource) appendBulkHostDeleteResult(
	ctx context.Context, userID int64, idempotencyKey string, result bulkHostDeleteResult,
) error {
	return appendBulkHostDeleteResultWith(rs.DB, ctx, userID, idempotencyKey, result)
}

func appendBulkHostDeleteResultWith(
	executor bulkHostResultExecutor,
	ctx context.Context,
	userID int64,
	idempotencyKey string,
	result bulkHostDeleteResult,
) error {
	payload, err := json.Marshal([]bulkHostDeleteResult{result})
	if err != nil {
		return err
	}
	update, err := executor.ExecContext(ctx, `
		UPDATE bulk_host_delete_requests SET results=results || $1::jsonb
		 WHERE user_id=$2 AND idempotency_key=$3`, string(payload), userID, idempotencyKey)
	if err != nil {
		return err
	}
	count, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("bulk host deletion ledger row is missing")
	}
	return nil
}

func insertBulkHostDeleteActivity(
	ctx context.Context,
	tx *sqlx.Tx,
	r *http.Request,
	user middleware.UserContext,
	inventoryID, hostID int64,
) error {
	var organizationID int64
	if err := tx.GetContext(ctx, &organizationID, `SELECT organization_id FROM inventories WHERE id=$1`, inventoryID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO activity_stream (
		    user_id,username,principal_kind,action,resource_type,resource_id,
		    organization_id,method,path,status_code,outcome
		) VALUES ($1,$2,'human','delete','host',$3,$4,$5,$6,$7,'success')`,
		user.UserID, user.Username, hostID, organizationID,
		http.MethodPost, r.URL.Path, http.StatusNoContent)
	return err
}

func (rs *HostsResource) renderBulkHostDeleteResponse(
	w http.ResponseWriter, r *http.Request, idempotencyKey string, results []bulkHostDeleteResult,
) {
	status := http.StatusOK
	for _, result := range results {
		if result.Status != "deleted" {
			status = http.StatusMultiStatus
			break
		}
	}
	render.Status(r, status)
	render.JSON(w, r, bulkHostDeleteResponse{
		IdempotencyKey: idempotencyKey, Complete: true, Results: results,
	})
}
