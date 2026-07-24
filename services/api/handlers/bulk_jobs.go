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
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/launch"
	"github.com/praetordev/praetor/services/api/middleware"
)

const (
	maxBulkJobLaunchItems = 25
	maxBulkJobLaunchBody  = 256 << 10
	maxBulkItemIdentifier = 64
)

type bulkJobLaunchItem struct {
	Identifier           string                 `json:"identifier,omitempty"`
	UnifiedJobTemplateID int64                  `json:"unified_job_template_id"`
	Name                 string                 `json:"name"`
	ExtraVars            map[string]interface{} `json:"extra_vars,omitempty"`
	Limit                *string                `json:"limit,omitempty"`
	RelaunchSourceJobID  *int64                 `json:"relaunch_source_job_id,omitempty"`
}

type bulkJobLaunchRequest struct {
	Items []bulkJobLaunchItem `json:"items"`
}

type bulkJobLaunchResult struct {
	Index      int    `json:"index"`
	Identifier string `json:"identifier,omitempty"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status"`
	JobID      int64  `json:"job_id,omitempty"`
	Code       string `json:"code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type bulkJobLaunchResponse struct {
	IdempotencyKey string                `json:"idempotency_key"`
	Complete       bool                  `json:"complete"`
	Results        []bulkJobLaunchResult `json:"results"`
}

type captureResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newCaptureResponseWriter() *captureResponseWriter {
	return &captureResponseWriter{header: make(http.Header)}
}

func (w *captureResponseWriter) Header() http.Header { return w.header }

func (w *captureResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

// transactionalJobStore overrides only the job mutations used by LaunchJob.
// Every read and authorization decision still delegates to the production
// JobStore; accepted writes join the idempotency-result transaction.
type transactionalJobStore struct {
	JobStore
	tx *sqlx.Tx
}

func (s *transactionalJobStore) InsertPendingJob(ctx context.Context, name string, unifiedTemplateID int64, opts launch.Options) (int64, error) {
	id, err := launch.Job(ctx, s.tx, name, &unifiedTemplateID, opts)
	if err != nil {
		return 0, fmt.Errorf("insert pending job: %w", err)
	}
	return id, nil
}

func (s *transactionalJobStore) SetRelaunchSource(ctx context.Context, jobID, sourceJobID, unifiedTemplateID int64) error {
	result, err := s.tx.ExecContext(ctx, `
		UPDATE unified_jobs target SET source_job_id = source.id
		FROM unified_jobs source
		WHERE target.id=$1 AND source.id=$2
		  AND target.unified_job_template_id=$3
		  AND source.unified_job_template_id=$3`,
		jobID, sourceJobID, unifiedTemplateID)
	if err != nil {
		return fmt.Errorf("set relaunch source: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read relaunch update count: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("source job does not belong to the governing template")
	}
	return nil
}

// BulkLaunchJobs handles POST /api/v1/bulk/jobs/launch. It is deliberately
// sequential and bounded: each item runs through LaunchJob, and one item's
// result is durably recorded before the next item starts.
func (rs *JobsResource) BulkLaunchJobs(w http.ResponseWriter, r *http.Request) {
	var input bulkJobLaunchRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBulkJobLaunchBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			rs.renderBulkError(w, r, &ErrResponse{
				Err: err, HTTPStatusCode: http.StatusRequestEntityTooLarge,
				StatusText: "Request Entity Too Large", ErrorText: "bulk launch payload exceeds 256 KiB",
			})
			return
		}
		rs.renderBulkError(w, r, ErrInvalidRequest(err))
		return
	}
	if err := requireJSONEOF(decoder); err != nil {
		rs.renderBulkError(w, r, ErrInvalidRequest(err))
		return
	}
	if len(input.Items) == 0 || len(input.Items) > maxBulkJobLaunchItems {
		rs.renderBulkError(w, r, ErrInvalidRequest(fmt.Errorf("items must contain between 1 and %d launches", maxBulkJobLaunchItems)))
		return
	}
	for i, item := range input.Items {
		if item.UnifiedJobTemplateID <= 0 {
			rs.renderBulkError(w, r, ErrInvalidRequest(fmt.Errorf("items[%d].unified_job_template_id must be positive", i)))
			return
		}
		if len(item.Identifier) > maxBulkItemIdentifier {
			rs.renderBulkError(w, r, ErrInvalidRequest(fmt.Errorf("items[%d].identifier exceeds %d characters", i, maxBulkItemIdentifier)))
			return
		}
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		rs.renderBulkError(w, r, ErrInvalidRequest(fmt.Errorf("Idempotency-Key header is required and must be 1-128 safe characters")))
		return
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		rs.renderBulkError(w, r, ErrInternal(err))
		return
	}
	sum := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(sum[:])
	user := currentUser(r)
	lockName := fmt.Sprintf("bulk-job-launch:%d:%s", user.UserID, idempotencyKey)

	conn, err := rs.DB.Connx(r.Context())
	if err != nil {
		rs.renderBulkError(w, r, ErrInternal(err))
		return
	}
	defer conn.Close()
	if _, err := conn.ExecContext(r.Context(), `SELECT pg_advisory_lock(hashtextextended($1, 0))`, lockName); err != nil {
		rs.renderBulkError(w, r, ErrInternal(err))
		return
	}
	defer unlockBulkLaunch(conn, lockName, rs.log)

	if _, err := conn.ExecContext(r.Context(), `
		INSERT INTO bulk_job_launch_requests (user_id, idempotency_key, request_hash)
		VALUES ($1,$2,$3)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING`,
		user.UserID, idempotencyKey, requestHash); err != nil {
		rs.renderBulkError(w, r, ErrInternal(err))
		return
	}

	var stored struct {
		RequestHash string          `db:"request_hash"`
		Results     json.RawMessage `db:"results"`
		Complete    bool            `db:"complete"`
	}
	if err := conn.GetContext(r.Context(), &stored, `
		SELECT request_hash, results, complete
		FROM bulk_job_launch_requests
		WHERE user_id=$1 AND idempotency_key=$2`,
		user.UserID, idempotencyKey); err != nil {
		rs.renderBulkError(w, r, ErrInternal(err))
		return
	}
	if stored.RequestHash != requestHash {
		rs.renderBulkError(w, r, ErrConflict(fmt.Errorf("idempotency key already used for a different bulk launch request")))
		return
	}

	results := []bulkJobLaunchResult{}
	if err := json.Unmarshal(stored.Results, &results); err != nil || len(results) > len(input.Items) {
		rs.renderBulkError(w, r, ErrInternal(fmt.Errorf("invalid stored bulk launch state")))
		return
	}
	if !stored.Complete {
		for index := len(results); index < len(input.Items); index++ {
			result, err := rs.launchBulkItem(r, user, idempotencyKey, index, input.Items[index])
			if err != nil {
				rs.renderBulkError(w, r, ErrInternal(err))
				return
			}
			results = append(results, result)
		}
		if _, err := conn.ExecContext(r.Context(), `
			UPDATE bulk_job_launch_requests
			   SET complete=TRUE, completed_at=now()
			 WHERE user_id=$1 AND idempotency_key=$2`,
			user.UserID, idempotencyKey); err != nil {
			rs.renderBulkError(w, r, ErrInternal(err))
			return
		}
	}

	middleware.SetActivityMetadata(r, middleware.ActivityMetadata{
		Action:       "bulk_launch",
		ResourceType: "job",
	})
	rs.renderBulkLaunchResponse(w, r, idempotencyKey, results)
}

func (rs *JobsResource) renderBulkError(w http.ResponseWriter, r *http.Request, response render.Renderer) {
	if err := render.Render(w, r, response); err != nil {
		rs.log.Error("render bulk job launch response", "err", err)
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request body must contain exactly one JSON object")
		}
		return err
	}
	return nil
}

func unlockBulkLaunch(conn *sqlx.Conn, lockName string, log interface{ Error(string, ...any) }) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockName); err != nil {
		log.Error("release bulk launch advisory lock", "err", err)
	}
}

func (rs *JobsResource) launchBulkItem(r *http.Request, user middleware.UserContext, idempotencyKey string, index int, item bulkJobLaunchItem) (bulkJobLaunchResult, error) {
	tx, err := rs.DB.BeginTxx(r.Context(), nil)
	if err != nil {
		return bulkJobLaunchResult{}, err
	}
	itemResource := *rs
	itemResource.store = &transactionalJobStore{JobStore: rs.store, tx: tx}

	body, err := json.Marshal(item)
	if err != nil {
		_ = tx.Rollback()
		return bulkJobLaunchResult{}, err
	}
	itemRequest := r.Clone(r.Context())
	itemRequest.Body = io.NopCloser(bytes.NewReader(body))
	itemRequest.ContentLength = int64(len(body))
	itemRequest.Header = r.Header.Clone()
	itemRequest.Header.Set("Content-Type", "application/json")
	recorder := newCaptureResponseWriter()
	itemResource.LaunchJob(recorder, itemRequest)
	result := normalizeBulkLaunchResult(index, item.Identifier, recorder)

	if result.Status != "accepted" {
		if err := tx.Rollback(); err != nil {
			return bulkJobLaunchResult{}, err
		}
		if err := rs.appendBulkLaunchResult(r.Context(), user.UserID, idempotencyKey, result); err != nil {
			return bulkJobLaunchResult{}, err
		}
		return result, nil
	}

	if err := insertBulkJobLaunchActivity(r.Context(), tx, r, user, item.UnifiedJobTemplateID, result.JobID); err != nil {
		_ = tx.Rollback()
		return bulkJobLaunchResult{}, err
	}
	if err := appendBulkLaunchResultTx(r.Context(), tx, user.UserID, idempotencyKey, result); err != nil {
		_ = tx.Rollback()
		return bulkJobLaunchResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return bulkJobLaunchResult{}, err
	}
	return result, nil
}

func normalizeBulkLaunchResult(index int, identifier string, recorder *captureResponseWriter) bulkJobLaunchResult {
	result := bulkJobLaunchResult{
		Index: index, Identifier: identifier, Status: "rejected", HTTPStatus: recorder.status,
	}
	var response struct {
		ID    int64  `json:"id"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(recorder.body.Bytes(), &response)
	if recorder.status == http.StatusCreated && response.ID > 0 {
		result.Status = "accepted"
		result.JobID = response.ID
		return result
	}
	switch {
	case recorder.status == http.StatusForbidden ||
		(recorder.status == http.StatusBadRequest && response.Error == "unknown job template"):
		result.HTTPStatus = http.StatusForbidden
		result.Code = "not_found_or_forbidden"
		result.Error = "job template not found or launch not permitted"
	case recorder.status == http.StatusBadRequest:
		result.Code = "invalid_request"
		result.Error = response.Error
	case recorder.status == http.StatusConflict:
		result.Code = "conflict"
		result.Error = response.Error
	default:
		result.HTTPStatus = http.StatusInternalServerError
		result.Code = "internal_error"
		result.Error = "launch could not be processed"
	}
	return result
}

func (rs *JobsResource) appendBulkLaunchResult(ctx context.Context, userID int64, idempotencyKey string, result bulkJobLaunchResult) error {
	payload, err := json.Marshal([]bulkJobLaunchResult{result})
	if err != nil {
		return err
	}
	resultUpdate, err := rs.DB.ExecContext(ctx, `
		UPDATE bulk_job_launch_requests
		   SET results = results || $1::jsonb
		 WHERE user_id=$2 AND idempotency_key=$3`,
		string(payload), userID, idempotencyKey)
	if err != nil {
		return err
	}
	return requireOneBulkLedgerRow(resultUpdate)
}

func appendBulkLaunchResultTx(ctx context.Context, tx *sqlx.Tx, userID int64, idempotencyKey string, result bulkJobLaunchResult) error {
	payload, err := json.Marshal([]bulkJobLaunchResult{result})
	if err != nil {
		return err
	}
	resultUpdate, err := tx.ExecContext(ctx, `
		UPDATE bulk_job_launch_requests
		   SET results = results || $1::jsonb
		 WHERE user_id=$2 AND idempotency_key=$3`,
		string(payload), userID, idempotencyKey)
	if err != nil {
		return err
	}
	return requireOneBulkLedgerRow(resultUpdate)
}

func requireOneBulkLedgerRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("bulk launch idempotency ledger row is missing")
	}
	return nil
}

func insertBulkJobLaunchActivity(ctx context.Context, tx *sqlx.Tx, r *http.Request, user middleware.UserContext, unifiedTemplateID, jobID int64) error {
	var organizationID int64
	if err := tx.GetContext(ctx, &organizationID, `
		SELECT organization_id FROM job_templates WHERE unified_job_template_id=$1`,
		unifiedTemplateID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO activity_stream (
		    user_id, username, principal_kind, action, resource_type, resource_id,
		    organization_id, method, path, status_code, outcome
		) VALUES ($1,$2,'human','launch','unified_job',$3,$4,$5,$6,$7,'success')`,
		user.UserID, user.Username, jobID, organizationID,
		http.MethodPost, r.URL.Path, http.StatusCreated)
	return err
}

func (rs *JobsResource) renderBulkLaunchResponse(w http.ResponseWriter, r *http.Request, idempotencyKey string, results []bulkJobLaunchResult) {
	status := http.StatusCreated
	for _, result := range results {
		if result.Status != "accepted" {
			status = http.StatusMultiStatus
			break
		}
	}
	render.Status(r, status)
	render.JSON(w, r, bulkJobLaunchResponse{
		IdempotencyKey: idempotencyKey,
		Complete:       true,
		Results:        results,
	})
}
