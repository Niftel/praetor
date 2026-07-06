// Package core implements pull-based reconciliation: harvesting a run's WAL from
// the host it ran on when the host-runner's push never reached the control plane
// (host unreachable at sync time, or the control plane was down for the whole
// run). It SSHes to the host, reads status.json + events.jsonl + stdout.log, and
// re-feeds them through the same ingestion endpoints a push uses, so projection
// is idempotent (consumer ON CONFLICT (execution_run_id, seq)). See
// host_side_runner_spec.md §5.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/praetordev/praetor/pkg/credentials"
	"github.com/praetordev/praetor/pkg/events"
	"github.com/praetordev/praetor/pkg/hostconn"
	"golang.org/x/crypto/ssh"
)

// maxLogChunk matches the host-runner's LogSyncer so pulled stdout is chunked the
// same way; combined with a byte offset from already-stored chunks this yields
// gap-free, non-overlapping chunk seqs.
const maxLogChunk = 256 * 1024

// Reconciler periodically resolves runs parked in 'reconciling'.
type Reconciler struct {
	DB       *sqlx.DB
	APIURL   string
	Interval time.Duration
	Client   *http.Client

	Batch       int           // runs processed per tick
	MaxAttempts int           // unproductive attempts before declaring a run lost
	MaxBackoff  time.Duration // cap on the retry interval

	stop chan struct{}
}

func NewReconciler(db *sqlx.DB, interval time.Duration, apiURL string) *Reconciler {
	return &Reconciler{
		DB:          db,
		APIURL:      apiURL,
		Interval:    interval,
		Client:      &http.Client{Timeout: 15 * time.Second},
		Batch:       10,
		MaxAttempts: 15,
		MaxBackoff:  5 * time.Minute,
		stop:        make(chan struct{}),
	}
}

func (r *Reconciler) Start() {
	log.Printf("Reconciler started (interval %s, api %s)", r.Interval, r.APIURL)
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.tick()
		}
	}
}

func (r *Reconciler) Stop() { close(r.stop) }

// candidate is a run due for reconciliation plus everything needed to reach its
// host — resolved from the snapshotted runner_host_id and the job's credential.
type candidate struct {
	RunID        uuid.UUID `db:"id"`
	UnifiedJobID int64     `db:"unified_job_id"`
	PersistedSeq int64     `db:"persisted_event_seq"`
	Attempts     int       `db:"reconcile_attempts"`
	HostName     string    `db:"host_name"`
	HostVars     []byte    `db:"host_vars"`
	CredentialID *int64    `db:"credential_id"`
}

func (r *Reconciler) tick() {
	defer func(start time.Time) { ReconcileTick.Observe(time.Since(start).Seconds()) }(time.Now())
	ctx := context.Background()
	var cs []candidate
	err := r.DB.SelectContext(ctx, &cs, `
		SELECT er.id, er.unified_job_id, er.persisted_event_seq,
		       er.reconcile_attempts, h.name AS host_name, h.variables AS host_vars,
		       jt.credential_id
		FROM execution_runs er
		JOIN hosts h ON h.id = er.runner_host_id
		JOIN unified_jobs uj ON uj.id = er.unified_job_id
		LEFT JOIN job_templates jt ON jt.unified_job_template_id = uj.unified_job_template_id
		WHERE er.state = 'reconciling'
		  AND (er.reconcile_after IS NULL OR er.reconcile_after <= now())
		ORDER BY er.reconcile_after NULLS FIRST
		LIMIT $1`, r.Batch)
	if err != nil {
		log.Printf("Reconciler: candidate query failed: %v", err)
		return
	}
	for _, c := range cs {
		r.processRun(ctx, c)
	}
}

func (r *Reconciler) processRun(ctx context.Context, c candidate) {
	client, sudo, err := r.connect(ctx, c)
	if err != nil {
		r.backoff(ctx, c, "connect: "+err.Error())
		return
	}
	defer client.Close()

	jobDir := "/var/lib/praetor/jobs/" + c.RunID.String()

	// Host reachable but the job directory is gone (host rebooted and lost its
	// WAL): the run is genuinely unrecoverable (spec §5.3).
	if out, _ := hostconn.Run(client, "test -d "+hostconn.Quote(jobDir)+" && echo yes || echo no"); strings.TrimSpace(out) == "no" {
		r.markLost(ctx, c, "job directory gone on host")
		return
	}

	// Read the terminal marker first so we know whether the job is done.
	statusRaw, _ := hostconn.Run(client, sudo+"cat "+hostconn.Quote(jobDir+"/status.json")+" 2>/dev/null")
	var st struct {
		State       string     `json:"state"`
		MaxSeq      int64      `json:"max_seq"`
		CompletedAt *time.Time `json:"completed_at"`
	}
	hasStatus := json.Unmarshal([]byte(strings.TrimSpace(statusRaw)), &st) == nil && st.State != ""

	// Project any events the control plane hasn't seen, then the log tail. On a
	// projection error we back off and retry rather than advancing state.
	newSeq, err := r.projectEvents(ctx, client, sudo, jobDir, c)
	if err != nil {
		r.backoff(ctx, c, "project events: "+err.Error())
		return
	}
	if err := r.projectLogs(ctx, client, sudo, jobDir, c.RunID); err != nil {
		log.Printf("Reconciler: run %s log projection failed (non-fatal): %v", c.RunID, err)
	}

	if hasStatus && isTerminal(st.State) {
		r.finalize(ctx, c, st.State, st.MaxSeq, st.CompletedAt)
		log.Printf("Reconciler: run %s recovered as %q (max_seq %d)", c.RunID, st.State, st.MaxSeq)
		return
	}

	// Not terminal: the job may still be running on the host. Keep monitoring so
	// long as we're making progress; give up only when a reachable host stops
	// producing new events for MaxAttempts consecutive checks (hung runner).
	if newSeq > c.PersistedSeq {
		r.advance(ctx, c, newSeq)
		return
	}
	r.backoff(ctx, c, "reachable but no progress and no terminal status")
}

// connect resolves the SSH identity for a run the same way the executor did (host
// vars overlaid by the job's Machine credential) and dials the host.
func (r *Reconciler) connect(ctx context.Context, c candidate) (*ssh.Client, string, error) {
	var vars map[string]interface{}
	_ = json.Unmarshal(c.HostVars, &vars)
	getVar := func(k string) string {
		if v, ok := vars[k]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	var credEnv, credFiles map[string]string
	if c.CredentialID != nil {
		var err error
		credEnv, credFiles, err = credentials.ResolveInjectors(ctx, r.DB, *c.CredentialID)
		if err != nil {
			return nil, "", fmt.Errorf("resolve credential %d: %w", *c.CredentialID, err)
		}
	}

	addr := hostconn.FirstNonEmpty(getVar("ansible_host"), c.HostName)
	port := hostconn.FirstNonEmpty(getVar("ansible_port"), "22")
	user := hostconn.FirstNonEmpty(getVar("ansible_user"), credEnv["ANSIBLE_REMOTE_USER"])
	key := credFiles["ANSIBLE_PRIVATE_KEY_FILE"]
	if user == "" || key == "" {
		return nil, "", fmt.Errorf("no SSH user/key (assign a Machine credential to the template)")
	}
	if !strings.HasSuffix(key, "\n") {
		key += "\n"
	}
	client, err := hostconn.Dial(addr, port, user, []byte(key))
	if err != nil {
		return nil, "", err
	}
	// The host-runner writes the job dir as root; a non-root login reads it via sudo.
	sudo := ""
	if user != "root" {
		sudo = "sudo "
	}
	return client, sudo, nil
}

// projectEvents streams events.jsonl, POSTs every record with seq > the run's
// persisted_event_seq to the ingestion endpoint (idempotent downstream), and
// returns the highest seq observed in the log.
func (r *Reconciler) projectEvents(ctx context.Context, client *ssh.Client, sudo, jobDir string, c candidate) (int64, error) {
	raw, err := hostconn.Run(client, sudo+"cat "+hostconn.Quote(jobDir+"/events.jsonl")+" 2>/dev/null")
	if err != nil {
		return c.PersistedSeq, nil // no WAL yet is not an error
	}
	batch, maxSeq := filterNewEvents(raw, c.PersistedSeq)
	if len(batch) == 0 {
		return maxSeq, nil
	}
	if err := r.postJSON(fmt.Sprintf("%s/api/v1/runs/%s/events", r.APIURL, c.RunID), batch); err != nil {
		return c.PersistedSeq, err
	}
	ReconcileEventsProjected.Add(float64(len(batch)))
	return maxSeq, nil
}

// projectLogs pulls the not-yet-stored tail of stdout.log and uploads it as
// chunks continuing from the last stored (offset, seq).
func (r *Reconciler) projectLogs(ctx context.Context, client *ssh.Client, sudo, jobDir string, runID uuid.UUID) error {
	var stored struct {
		Bytes  int64 `db:"bytes"`
		MaxSeq int64 `db:"maxseq"`
	}
	if err := r.DB.GetContext(ctx, &stored,
		`SELECT COALESCE(SUM(byte_length),0) AS bytes, COALESCE(MAX(seq),-1) AS maxseq
		 FROM job_output_chunks WHERE execution_run_id = $1`, runID); err != nil {
		return err
	}
	// tail -c +N is 1-indexed: +(bytes+1) starts just after the stored bytes.
	tail, err := hostconn.Run(client, fmt.Sprintf("%stail -c +%d %s 2>/dev/null", sudo, stored.Bytes+1, hostconn.Quote(jobDir+"/stdout.log")))
	if err != nil || tail == "" {
		return nil
	}
	seq := stored.MaxSeq + 1
	for _, chunk := range splitChunks([]byte(tail), maxLogChunk) {
		url := fmt.Sprintf("%s/api/v1/runs/%s/logs?seq=%d", r.APIURL, runID, seq)
		if err := r.postBytes(url, chunk); err != nil {
			return err
		}
		ReconcileChunksProjected.Inc()
		seq++
	}
	return nil
}

// --- state transitions ---

// finalize records the authoritative terminal outcome from status.json. The
// consumer also transitions the run when it projects the terminal event; this
// makes the outcome deterministic even if events.jsonl lacked a terminal record,
// and always advances persisted_event_seq (spec §5.2 step 4).
func (r *Reconciler) finalize(ctx context.Context, c candidate, state string, maxSeq int64, completedAt *time.Time) {
	fin := time.Now()
	if completedAt != nil {
		fin = *completedAt
	}
	if _, err := r.DB.ExecContext(ctx, `
		UPDATE execution_runs SET
			persisted_event_seq = $2,
			state = CASE WHEN NOT run_is_terminal(state) THEN $3 ELSE state END,
			finished_at = COALESCE(finished_at, $4)
		WHERE id = $1`, c.RunID, maxSeq, state, fin); err != nil {
		log.Printf("Reconciler: finalize run %s failed: %v", c.RunID, err)
		return
	}
	if _, err := r.DB.ExecContext(ctx, `
		UPDATE unified_jobs SET status = $2, finished_at = COALESCE(finished_at, $3)
		WHERE id = $1 AND NOT job_is_terminal(status) AND status <> 'error'`,
		c.UnifiedJobID, state, fin); err != nil {
		log.Printf("Reconciler: finalize job %d failed: %v", c.UnifiedJobID, err)
	}
	ReconcileOutcomes.WithLabelValues("recovered_" + state).Inc()
}

// advance records progress on a still-running job: bump persisted_event_seq,
// reset the give-up counter, and re-check soon.
func (r *Reconciler) advance(ctx context.Context, c candidate, newSeq int64) {
	if _, err := r.DB.ExecContext(ctx, `
		UPDATE execution_runs
		SET persisted_event_seq = $2, reconcile_attempts = 0, reconcile_after = now() + interval '30 seconds'
		WHERE id = $1`, c.RunID, newSeq); err != nil {
		log.Printf("Reconciler: advance run %s failed: %v", c.RunID, err)
	}
	ReconcileOutcomes.WithLabelValues("still_running").Inc()
}

// backoff schedules the next attempt with exponential delay, giving up (marking
// the run lost) once MaxAttempts unproductive tries have elapsed.
func (r *Reconciler) backoff(ctx context.Context, c candidate, reason string) {
	attempts := c.Attempts + 1
	ReconcileAttempts.Inc()
	if attempts >= r.MaxAttempts {
		r.markLost(ctx, c, fmt.Sprintf("gave up after %d attempts (%s)", attempts, reason))
		return
	}
	delay := backoffDelay(attempts, r.MaxBackoff)
	log.Printf("Reconciler: run %s retry %d in %s (%s)", c.RunID, attempts, delay, reason)
	if _, err := r.DB.ExecContext(ctx, `
		UPDATE execution_runs
		SET reconcile_attempts = $2, reconcile_after = now() + ($3 || ' seconds')::interval
		WHERE id = $1`, c.RunID, attempts, strconv.Itoa(int(delay.Seconds()))); err != nil {
		log.Printf("Reconciler: backoff run %s failed: %v", c.RunID, err)
	}
}

// markLost declares a run unrecoverable: host is gone or persistently unreachable.
func (r *Reconciler) markLost(ctx context.Context, c candidate, reason string) {
	log.Printf("Reconciler: marking run %s lost: %s", c.RunID, reason)
	if _, err := r.DB.ExecContext(ctx, `
		UPDATE execution_runs SET state = 'lost', finished_at = now()
		WHERE id = $1 AND state NOT IN ('successful','failed','canceled')`, c.RunID); err != nil {
		log.Printf("Reconciler: markLost run %s failed: %v", c.RunID, err)
	}
	if _, err := r.DB.ExecContext(ctx, `
		UPDATE unified_jobs SET status = 'error', finished_at = now()
		WHERE id = $1 AND status NOT IN ('successful','failed','canceled','error')`, c.UnifiedJobID); err != nil {
		log.Printf("Reconciler: markLost job %d failed: %v", c.UnifiedJobID, err)
	}
	ReconcileOutcomes.WithLabelValues("lost").Inc()
}

// --- HTTP helpers (same endpoints the host-runner pushes to) ---

func (r *Reconciler) postJSON(url string, body interface{}) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return r.do(req)
}

func (r *Reconciler) postBytes(url string, data []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	return r.do(req)
}

func (r *Reconciler) do(req *http.Request) error {
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ingestion returned status %d", resp.StatusCode)
	}
	return nil
}

// --- pure helpers (unit-tested) ---

// isTerminal reports whether a status.json state is a finished outcome.
func isTerminal(state string) bool { return state == "successful" || state == "failed" }

// filterNewEvents parses an events.jsonl blob and returns the records with
// seq > persisted (to project) plus the highest seq seen across the whole log
// (to detect progress). Corrupt/partial lines are skipped, not fatal.
func filterNewEvents(raw string, persisted int64) (batch []events.JobEvent, maxSeq int64) {
	maxSeq = persisted
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev events.JobEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
		if ev.Seq > persisted {
			batch = append(batch, ev)
		}
	}
	return batch, maxSeq
}

// splitChunks slices data into pieces of at most max bytes, preserving order and
// total length (so re-chunked stdout lines up byte-for-byte with stored chunks).
func splitChunks(data []byte, max int) [][]byte {
	var out [][]byte
	for len(data) > 0 {
		n := len(data)
		if n > max {
			n = max
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

// backoffDelay is an exponential retry delay (30s doubling) capped at max.
func backoffDelay(attempts int, max time.Duration) time.Duration {
	shift := attempts
	if shift > 4 {
		shift = 4
	}
	delay := time.Duration(1<<shift) * 30 * time.Second
	if delay > max {
		delay = max
	}
	return delay
}
