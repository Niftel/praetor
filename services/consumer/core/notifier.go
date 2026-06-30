package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/events"
)

// Notifier dispatches notifications when a job reaches a lifecycle event. It is
// driven off the event projection and fired only when a terminal event is newly
// projected, so each notification is sent exactly once despite at-least-once
// delivery. HTTP sends run in the background so projection latency is unaffected.
type Notifier struct {
	DB     *sqlx.DB
	key    string
	client *http.Client
}

func NewNotifier(db *sqlx.DB) *Notifier {
	return &Notifier{DB: db, key: notifierSecretKey(), client: &http.Client{Timeout: 10 * time.Second}}
}

func notifierSecretKey() string {
	if k := os.Getenv("PRAETOR_SECRET_KEY"); k != "" {
		return k
	}
	return "12345678901234567890123456789012"
}

// notifyEvent maps a job event type to a notification lifecycle event and a
// human verb, or ("","") if the event type doesn't trigger notifications.
func notifyEvent(eventType string) (event, verb string) {
	switch eventType {
	case "JOB_STARTED":
		return "started", "started"
	case "JOB_COMPLETED":
		return "success", "succeeded"
	case "JOB_FAILED":
		return "error", "failed"
	}
	return "", ""
}

// Dispatch fires any notifications attached to the job's template for evt's
// lifecycle event. Safe to call inline after a commit (and on a nil receiver).
func (n *Notifier) Dispatch(evt events.JobEvent) {
	if n == nil || n.DB == nil {
		return
	}
	ev, verb := notifyEvent(evt.EventType)
	if ev == "" {
		return
	}
	go n.send(evt.UnifiedJobID, ev, verb)
}

func (n *Notifier) send(jobID int64, ev, verb string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type row struct {
		Type    string          `db:"notification_type"`
		Config  json.RawMessage `db:"config"`
		JobName string          `db:"name"`
	}
	var rows []row
	if err := n.DB.SelectContext(ctx, &rows, `
		SELECT nt.notification_type, nt.config, uj.name
		FROM unified_jobs uj
		JOIN job_templates jt ON jt.unified_job_template_id = uj.unified_job_template_id
		JOIN job_template_notifications jtn ON jtn.job_template_id = jt.id AND jtn.event = $2
		JOIN notification_templates nt ON nt.id = jtn.notification_template_id
		WHERE uj.id = $1`, jobID, ev); err != nil {
		log.Printf("notifier: lookup failed for job %d: %v", jobID, err)
		return
	}

	for _, r := range rows {
		var cfg struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(r.Config, &cfg)
		url := cfg.URL
		if dec, err := crypto.Decrypt(url, n.key); err == nil {
			url = dec // stored encrypted; fall back to as-is if not
		}
		if url == "" {
			continue
		}

		var payload []byte
		switch r.Type {
		case "slack":
			payload, _ = json.Marshal(map[string]string{"text": fmt.Sprintf("Praetor job %q %s", r.JobName, verb)})
		default: // webhook
			payload, _ = json.Marshal(map[string]interface{}{
				"job_id": jobID, "job_name": r.JobName, "event": ev, "status": verb,
			})
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			log.Printf("notifier: build request failed: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := n.client.Do(req)
		if err != nil {
			log.Printf("notifier: POST (%s) for job %d failed: %v", r.Type, jobID, err)
			continue
		}
		resp.Body.Close()
		log.Printf("notifier: sent %s notification for job %d (%s) -> HTTP %d", r.Type, jobID, ev, resp.StatusCode)
	}
}
