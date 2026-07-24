package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
	"github.com/praetordev/praetor/services/api/dto"
	"github.com/praetordev/praetor/services/api/middleware"
	storepkg "github.com/praetordev/store"
)

const (
	maxBulkHostCreateItems = 100
	maxBulkHostCreateBody  = 512 << 10
)

type bulkHostCreateItem struct {
	Identifier    string          `json:"identifier,omitempty"`
	InventoryID   int64           `json:"inventory_id"`
	Name          string          `json:"name"`
	Description   *string         `json:"description,omitempty"`
	Variables     json.RawMessage `json:"variables,omitempty"`
	IsControlNode bool            `json:"is_control_node,omitempty"`
}

type bulkHostCreateRequest struct {
	Items []bulkHostCreateItem `json:"items"`
}

type bulkHostCreateResult struct {
	Index      int    `json:"index"`
	Identifier string `json:"identifier,omitempty"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
	HostID     int64  `json:"host_id,omitempty"`
	Code       string `json:"code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type bulkHostCreateResponse struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Complete       bool                   `json:"complete"`
	Results        []bulkHostCreateResult `json:"results"`
}

// transactionalHostStore makes an accepted host mutation atomic with its
// idempotency result and activity record. Reads and RBAC continue through the
// canonical production store and authorizer.
type transactionalHostStore struct {
	HostStore
	tx *sqlx.Tx
}

func (s *transactionalHostStore) Create(ctx context.Context, input models.Host) (models.Host, error) {
	var created models.Host
	err := s.tx.QueryRowxContext(ctx, `
		INSERT INTO hosts (inventory_id, name, description, variables, enabled, is_control_node)
		VALUES ($1,$2,$3,$4,TRUE,$5)
		RETURNING `+storepkg.HostCols,
		input.InventoryID, input.Name, input.Description, input.Variables, input.IsControlNode,
	).StructScan(&created)
	return created, err
}

// BulkCreateHosts handles POST /api/v1/bulk/hosts/create. Items are processed
// sequentially so results are deterministic, bounded, and durably resumable.
// Every item passes through CreateHost, including its inventory-admin check.
func (rs *HostsResource) BulkCreateHosts(w http.ResponseWriter, r *http.Request) {
	var input bulkHostCreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBulkHostCreateBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			rs.renderBulkHostError(w, r, &ErrResponse{
				Err: err, HTTPStatusCode: http.StatusRequestEntityTooLarge,
				StatusText: "Request Entity Too Large", ErrorText: "bulk host payload exceeds 512 KiB",
			})
			return
		}
		rs.renderBulkHostError(w, r, ErrInvalidRequest(err))
		return
	}
	if err := requireJSONEOF(decoder); err != nil {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(err))
		return
	}
	if len(input.Items) == 0 || len(input.Items) > maxBulkHostCreateItems {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf(
			"items must contain between 1 and %d hosts", maxBulkHostCreateItems,
		)))
		return
	}
	for index, item := range input.Items {
		if len(item.Identifier) > maxBulkItemIdentifier {
			rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf(
				"items[%d].identifier exceeds %d characters", index, maxBulkItemIdentifier,
			)))
			return
		}
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		rs.renderBulkHostError(w, r, ErrInvalidRequest(fmt.Errorf(
			"Idempotency-Key header is required and must be 1-128 safe characters",
		)))
		return
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	sum := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(sum[:])
	user := currentUser(r)
	lockName := fmt.Sprintf("bulk-host-create:%d:%s", user.UserID, idempotencyKey)

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

	if _, err := conn.ExecContext(r.Context(), `
		INSERT INTO bulk_host_create_requests (user_id, idempotency_key, request_hash)
		VALUES ($1,$2,$3)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING`,
		user.UserID, idempotencyKey, requestHash); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}

	var stored struct {
		RequestHash string          `db:"request_hash"`
		Results     json.RawMessage `db:"results"`
		Complete    bool            `db:"complete"`
	}
	if err := conn.GetContext(r.Context(), &stored, `
		SELECT request_hash, results, complete
		FROM bulk_host_create_requests
		WHERE user_id=$1 AND idempotency_key=$2`,
		user.UserID, idempotencyKey); err != nil {
		rs.renderBulkHostError(w, r, ErrInternal(err))
		return
	}
	if stored.RequestHash != requestHash {
		rs.renderBulkHostError(w, r, ErrConflict(errors.New(
			"idempotency key already used for a different bulk host request",
		)))
		return
	}

	results := []bulkHostCreateResult{}
	if err := json.Unmarshal(stored.Results, &results); err != nil || len(results) > len(input.Items) {
		rs.renderBulkHostError(w, r, ErrInternal(errors.New("invalid stored bulk host state")))
		return
	}
	if !stored.Complete {
		for index := len(results); index < len(input.Items); index++ {
			result, err := rs.createBulkHostItem(r, user, idempotencyKey, index, input.Items[index])
			if err != nil {
				rs.renderBulkHostError(w, r, ErrInternal(err))
				return
			}
			results = append(results, result)
		}
		if _, err := conn.ExecContext(r.Context(), `
			UPDATE bulk_host_create_requests
			   SET complete=TRUE, completed_at=now()
			 WHERE user_id=$1 AND idempotency_key=$2`,
			user.UserID, idempotencyKey); err != nil {
			rs.renderBulkHostError(w, r, ErrInternal(err))
			return
		}
	}

	middleware.SetActivityMetadata(r, middleware.ActivityMetadata{
		Action:       "bulk_create",
		ResourceType: "host",
	})
	rs.renderBulkHostResponse(w, r, idempotencyKey, results)
}

func (rs *HostsResource) createBulkHostItem(
	r *http.Request,
	user middleware.UserContext,
	idempotencyKey string,
	index int,
	item bulkHostCreateItem,
) (bulkHostCreateResult, error) {
	if item.InventoryID <= 0 {
		result := rejectedBulkHostResult(index, item.Identifier, http.StatusBadRequest, "invalid_request", "inventory_id must be positive")
		return result, rs.appendBulkHostResult(r.Context(), user.UserID, idempotencyKey, result)
	}

	tx, err := rs.DB.BeginTxx(r.Context(), nil)
	if err != nil {
		return bulkHostCreateResult{}, err
	}
	itemResource := *rs
	itemResource.store = &transactionalHostStore{HostStore: rs.store, tx: tx}

	hostBody := dto.Host{
		Name: item.Name, Description: item.Description, Variables: item.Variables,
		IsControlNode: item.IsControlNode,
	}
	body, err := json.Marshal(hostBody)
	if err != nil {
		_ = tx.Rollback()
		return bulkHostCreateResult{}, err
	}
	itemRequest := r.Clone(r.Context())
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("inventoryId", strconv.FormatInt(item.InventoryID, 10))
	itemRequest = itemRequest.WithContext(context.WithValue(itemRequest.Context(), chi.RouteCtxKey, routeContext))
	itemRequest.Body = io.NopCloser(bytes.NewReader(body))
	itemRequest.ContentLength = int64(len(body))
	itemRequest.Header = r.Header.Clone()
	itemRequest.Header.Set("Content-Type", "application/json")
	recorder := newCaptureResponseWriter()
	itemResource.CreateHost(recorder, itemRequest)
	result := normalizeBulkHostCreateResult(index, item.Identifier, recorder)

	if result.Status != "created" {
		if err := tx.Rollback(); err != nil {
			return bulkHostCreateResult{}, err
		}
		if err := rs.appendBulkHostResult(r.Context(), user.UserID, idempotencyKey, result); err != nil {
			return bulkHostCreateResult{}, err
		}
		return result, nil
	}

	if err := insertBulkHostCreateActivity(r.Context(), tx, r, user, item.InventoryID, result.HostID); err != nil {
		_ = tx.Rollback()
		return bulkHostCreateResult{}, err
	}
	if err := appendBulkHostResultTx(r.Context(), tx, user.UserID, idempotencyKey, result); err != nil {
		_ = tx.Rollback()
		return bulkHostCreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return bulkHostCreateResult{}, err
	}
	return result, nil
}

func normalizeBulkHostCreateResult(index int, identifier string, recorder *captureResponseWriter) bulkHostCreateResult {
	result := bulkHostCreateResult{
		Index: index, Identifier: identifier, Status: "rejected", HTTPStatus: recorder.status,
	}
	var response struct {
		ID    int64  `json:"id"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(recorder.body.Bytes(), &response)
	if recorder.status == http.StatusCreated && response.ID > 0 {
		result.Status = "created"
		result.HostID = response.ID
		return result
	}
	switch recorder.status {
	case http.StatusForbidden, http.StatusNotFound:
		result.HTTPStatus = http.StatusForbidden
		result.Code = "not_found_or_forbidden"
		result.Error = "inventory not found or host creation not permitted"
	case http.StatusBadRequest:
		result.Code = "invalid_request"
		result.Error = response.Error
		if result.Error == "" {
			result.Error = "host request is invalid"
		}
	case http.StatusConflict:
		result.Code = "duplicate"
		result.Error = "a host with this name already exists in the inventory"
	default:
		result.HTTPStatus = http.StatusInternalServerError
		result.Code = "internal_error"
		result.Error = "host could not be created"
	}
	return result
}

func rejectedBulkHostResult(index int, identifier string, status int, code, message string) bulkHostCreateResult {
	return bulkHostCreateResult{
		Index: index, Identifier: identifier, Status: "rejected",
		HTTPStatus: status, Code: code, Error: message,
	}
}

func (rs *HostsResource) appendBulkHostResult(
	ctx context.Context,
	userID int64,
	idempotencyKey string,
	result bulkHostCreateResult,
) error {
	return appendBulkHostResultWith(rs.DB, ctx, userID, idempotencyKey, result)
}

type bulkHostResultExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func appendBulkHostResultWith(
	executor bulkHostResultExecutor,
	ctx context.Context,
	userID int64,
	idempotencyKey string,
	result bulkHostCreateResult,
) error {
	payload, err := json.Marshal([]bulkHostCreateResult{result})
	if err != nil {
		return err
	}
	update, err := executor.ExecContext(ctx, `
		UPDATE bulk_host_create_requests
		   SET results = results || $1::jsonb
		 WHERE user_id=$2 AND idempotency_key=$3`,
		string(payload), userID, idempotencyKey)
	if err != nil {
		return err
	}
	count, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("bulk host idempotency ledger row is missing")
	}
	return nil
}

func appendBulkHostResultTx(
	ctx context.Context,
	tx *sqlx.Tx,
	userID int64,
	idempotencyKey string,
	result bulkHostCreateResult,
) error {
	return appendBulkHostResultWith(tx, ctx, userID, idempotencyKey, result)
}

func insertBulkHostCreateActivity(
	ctx context.Context,
	tx *sqlx.Tx,
	r *http.Request,
	user middleware.UserContext,
	inventoryID, hostID int64,
) error {
	var organizationID int64
	if err := tx.GetContext(ctx, &organizationID, `
		SELECT organization_id FROM inventories WHERE id=$1`,
		inventoryID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO activity_stream (
		    user_id, username, principal_kind, action, resource_type, resource_id,
		    organization_id, method, path, status_code, outcome
		) VALUES ($1,$2,'human','create','host',$3,$4,$5,$6,$7,'success')`,
		user.UserID, user.Username, hostID, organizationID,
		http.MethodPost, r.URL.Path, http.StatusCreated)
	return err
}

func unlockBulkHostCreate(conn *sqlx.Conn, lockName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockName); err != nil {
		logger.Error("release bulk host advisory lock", "err", err)
	}
}

func (rs *HostsResource) renderBulkHostError(w http.ResponseWriter, r *http.Request, response render.Renderer) {
	if err := render.Render(w, r, response); err != nil {
		logger.Error("render bulk host response", "err", err)
	}
}

func (rs *HostsResource) renderBulkHostResponse(
	w http.ResponseWriter,
	r *http.Request,
	idempotencyKey string,
	results []bulkHostCreateResult,
) {
	status := http.StatusCreated
	for _, result := range results {
		if result.Status != "created" {
			status = http.StatusMultiStatus
			break
		}
	}
	render.Status(r, status)
	render.JSON(w, r, bulkHostCreateResponse{
		IdempotencyKey: idempotencyKey,
		Complete:       true,
		Results:        results,
	})
}
