package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
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
		Name           string `db:"name"`
		UJTID          *int64 `db:"unified_job_template_id"`
		WebhookEnabled bool   `db:"webhook_enabled"`
		WebhookKey     string `db:"webhook_key"`
	}
	// Not-found and verification-failure are deliberately indistinguishable from
	// the outside (don't reveal which templates have webhooks).
	if err := rs.DB.Get(&t,
		`SELECT name, unified_job_template_id, webhook_enabled, webhook_key
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

	var jobID int64
	if err := rs.DB.QueryRowx(`
		INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
		VALUES ($1, $2, 'pending', now(), $3) RETURNING id`,
		t.Name+" (webhook)", *t.UJTID, jobArgs).Scan(&jobID); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"job_id": jobID, "status": "pending"})
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
