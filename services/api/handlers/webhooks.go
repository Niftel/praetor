package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/services/api/render"
)

// WebhooksResource handles inbound (provider -> Praetor) webhooks that launch a
// job template. There is no user auth: the per-template shared secret is the
// authorization, verified per provider.
type WebhooksResource struct {
	DB *sqlx.DB
}

func NewWebhooksResource(db *sqlx.DB) *WebhooksResource {
	return &WebhooksResource{DB: db}
}

// Handle POST /api/v1/webhooks/job-templates/{id}/{service}
func (rs *WebhooksResource) Handle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	service := chi.URLParam(r, "service")

	var t struct {
		Name              string `db:"name"`
		UJTID             *int64 `db:"unified_job_template_id"`
		WebhookEnabled    bool   `db:"webhook_enabled"`
		WebhookKey        string `db:"webhook_key"`
		AllowSimultaneous bool   `db:"allow_simultaneous"`
	}
	// Not-found and verification-failure are deliberately indistinguishable from
	// the outside (don't reveal which templates have webhooks).
	if err := rs.DB.Get(&t,
		`SELECT name, unified_job_template_id, webhook_enabled, webhook_key, allow_simultaneous
		 FROM job_templates WHERE id = $1`, id); err != nil || !t.WebhookEnabled || t.UJTID == nil {
		http.NotFound(w, r)
		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // cap at 1MB
	if !verifyWebhook(service, t.WebhookKey, r, body) {
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	// Inject the payload (and convenience ref/commit vars) as extra_vars.
	vars := map[string]interface{}{}
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) == nil {
		vars["webhook_payload"] = payload
		if ref, ok := payload["ref"].(string); ok {
			vars["webhook_ref"] = ref
		}
		for _, k := range []string{"after", "checkout_sha"} {
			if c, ok := payload[k].(string); ok {
				vars["webhook_commit"] = c
				break
			}
		}
	}
	jobArgs, _ := json.Marshal(map[string]interface{}{"extra_vars": vars})

	// Concurrency guard: unless the template allows simultaneous runs, skip this
	// webhook trigger while a prior run is still active (webhooks can fire in
	// bursts; skip rather than queue an overlapping run).
	if !t.AllowSimultaneous {
		var active int
		if err := rs.DB.Get(&active,
			`SELECT count(*) FROM unified_jobs
			 WHERE unified_job_template_id = $1 AND status NOT IN ('successful','failed','canceled','error')`,
			*t.UJTID); err == nil && active > 0 {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "skipped", "reason": "a run of this template is already active"})
			return
		}
	}

	var jobID int64
	if err := rs.DB.QueryRowx(`
		INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
		VALUES ($1, $2, 'pending', now(), $3) RETURNING id`,
		t.Name+" (webhook)", *t.UJTID, jobArgs).Scan(&jobID); err != nil {
		if isActiveRunConflict(err) {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "skipped", "reason": "a run of this template is already active"})
			return
		}
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"job_id": jobID, "status": "pending"})
}

// HandleWorkflow POST /api/v1/webhooks/workflow-templates/{id}/{service}
// A remote event launches a whole workflow run (the workflow equivalent of Handle).
func (rs *WebhooksResource) HandleWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	service := chi.URLParam(r, "service")

	var t struct {
		WebhookEnabled bool   `db:"webhook_enabled"`
		WebhookKey     string `db:"webhook_key"`
	}
	if err := rs.DB.Get(&t,
		`SELECT webhook_enabled, webhook_key FROM workflow_templates WHERE id = $1`, id); err != nil || !t.WebhookEnabled {
		http.NotFound(w, r)
		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if !verifyWebhook(service, t.WebhookKey, r, body) {
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	// Snapshot the template's nodes into a running workflow_jobs run, exactly as
	// LaunchWorkflow does; the scheduler's workflow runner then walks it.
	tx, err := rs.DB.Beginx()
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer tx.Rollback()
	var wjID int64
	if err := tx.QueryRowx(
		`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1, 'running') RETURNING id`, id).Scan(&wjID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	// Snapshot nodes and edges into the run so later template edits don't affect it.
	if _, err := tx.Exec(
		`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body, status)
		 SELECT $1, node_key, node_type, job_template_id, name, webhook_url, webhook_body, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
		wjID, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if _, err := tx.Exec(
		`INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
		 SELECT $1, parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$2`,
		wjID, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"workflow_job_id": wjID, "status": "running"})
}

// HandlePack POST /api/v1/webhooks/execution-packs/{id}/{service}
// A git push re-queues a git-backed Execution Pack: the packbuilder then pulls the
// latest spec from the repo and rebuilds it.
func (rs *WebhooksResource) HandlePack(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	service := chi.URLParam(r, "service")
	var t struct {
		Name       string         `db:"name"`
		WebhookKey sql.NullString `db:"webhook_key"`
	}
	if err := rs.DB.Get(&t, `SELECT name, webhook_key FROM execution_packs WHERE id=$1`, id); err != nil || !t.WebhookKey.Valid || t.WebhookKey.String == "" {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if !verifyWebhook(service, t.WebhookKey.String, r, body) {
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}
	if _, err := rs.DB.Exec(`UPDATE execution_packs SET status='pending' WHERE id=$1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"pack": t.Name, "status": "pending"})
}

// HandleNodeCallback POST /api/v1/webhooks/workflow-job-nodes/{id}/callback
// Releases a 'webhook_in' node that is waiting at 'awaiting_event'. The caller
// authenticates with the node's per-run event_token (header X-Praetor-Token or
// ?token=). An optional {"status":"failed"} body (or ?result=failed) sends the
// workflow down the node's failure edges instead of success.
func (rs *WebhooksResource) HandleNodeCallback(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	var n struct {
		Status     string `db:"status"`
		EventToken string `db:"event_token"`
	}
	if err := rs.DB.Get(&n,
		`SELECT status, COALESCE(event_token,'') AS event_token FROM workflow_job_nodes WHERE id=$1`, id); err != nil {
		http.NotFound(w, r)
		return
	}

	tok := r.Header.Get("X-Praetor-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	// Constant-time compare; a blank stored token can never be matched.
	if n.EventToken == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(n.EventToken)) != 1 {
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}
	if n.Status != "awaiting_event" {
		http.Error(w, "node is not awaiting an event", http.StatusConflict)
		return
	}

	result := "successful"
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var payload struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(body, &payload)
	if payload.Status == "failed" || r.URL.Query().Get("result") == "failed" {
		result = "failed"
	}
	if _, err := rs.DB.Exec(
		`UPDATE workflow_job_nodes SET status=$1 WHERE id=$2 AND status='awaiting_event'`, result, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"node_id": id, "status": result})
}

// verifyWebhook checks the request against the template's shared secret using
// each provider's scheme: GitHub HMAC-SHA256, GitLab token header, or a generic
// token (header or query). All comparisons are constant-time.
func verifyWebhook(service, key string, r *http.Request, body []byte) bool {
	switch service {
	case "github":
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Hub-Signature-256")), []byte(expected)) == 1
	case "gitlab":
		return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), []byte(key)) == 1
	default: // generic
		tok := r.Header.Get("X-Praetor-Token")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		return subtle.ConstantTimeCompare([]byte(tok), []byte(key)) == 1
	}
}
