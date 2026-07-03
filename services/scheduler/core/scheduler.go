package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/util"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/events"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/teambition/rrule-go"
)

type Scheduler struct {
	DB        *sqlx.DB
	Ticker    *time.Ticker
	Done      chan bool
	Publisher EventPublisher
}

func NewScheduler(db *sqlx.DB, interval time.Duration, publisher EventPublisher) *Scheduler {
	return &Scheduler{
		DB:        db,
		Ticker:    time.NewTicker(interval),
		Done:      make(chan bool),
		Publisher: publisher,
	}
}

func (s *Scheduler) Start() {
	log.Println("Scheduler started")
	for {
		select {
		case <-s.Done:
			return
		case <-s.Ticker.C:
			tickStart := time.Now()
			if err := s.processPendingJobs(); err != nil {
				log.Printf("Error processing jobs: %v", err)
			}
			if err := s.relayOutbox(); err != nil {
				log.Printf("Error relaying outbox: %v", err)
			}
			if err := s.processSchedules(); err != nil {
				log.Printf("Error processing schedules: %v", err)
			}
			if err := s.processTimedOutJobs(); err != nil {
				log.Printf("Error processing timed out jobs: %v", err)
			}
			s.processWorkflows()
			s.processEventTriggers()
			TickDuration.Observe(time.Since(tickStart).Seconds())
		}
	}
}

func (s *Scheduler) Stop() {
	s.Ticker.Stop()
	s.Done <- true
	log.Println("Scheduler stopped")
}

func (s *Scheduler) processPendingJobs() error {
	ctx := context.Background()

	// Transaction for atomic claim-and-schedule
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Fetch pending jobs with SKIP LOCKED
	query := `
		SELECT id, name, unified_job_template_id, status, job_args
		FROM unified_jobs
		WHERE status = 'pending' AND current_run_id IS NULL
		FOR UPDATE SKIP LOCKED
		LIMIT 10`

	var jobs []models.UnifiedJob
	if err := tx.SelectContext(ctx, &jobs, query); err != nil {
		return fmt.Errorf("failed to select pending jobs: %w", err)
	}

	if len(jobs) == 0 {
		return nil
	}

	for _, job := range jobs {
		// 3. Create Execution Run
		var runID uuid.UUID
		err := tx.QueryRowContext(ctx, `
			INSERT INTO execution_runs (unified_job_id, attempt_number, state) 
			VALUES ($1, 1, 'pending') 
			RETURNING id`, job.ID).Scan(&runID)

		if err != nil {
			log.Printf("Failed to create run for job %d: %v", job.ID, err)
			return err // Rollback
		}

		// 4. Update Job
		_, err = tx.ExecContext(ctx, `
			UPDATE unified_jobs 
			SET status = 'queued', current_run_id = $1 
			WHERE id = $2`, runID, job.ID)

		if err != nil {
			log.Printf("Failed to update job %d: %v", job.ID, err)
			return err // Rollback
		}

		// Inventory-sync jobs carry no template; they reference an
		// inventory_source and are dispatched to the executor to run
		// `ansible-inventory` and upsert the result into the inventory.
		if srcID := inventorySourceID(job.JobArgs); srcID > 0 {
			var src struct {
				InventoryID  int64  `db:"inventory_id"`
				Source       string `db:"source"`
				Kind         string `db:"source_kind"`
				CredentialID *int64 `db:"credential_id"`
			}
			if err := tx.GetContext(ctx, &src,
				`SELECT inventory_id, source, source_kind, credential_id FROM inventory_sources WHERE id = $1`, srcID); err != nil {
				log.Printf("sync job %d: source %d not found: %v", job.ID, srcID, err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status='failed' WHERE id=$1", job.ID)
				continue
			}
			syncManifest := events.JobManifest{
				InventorySync:       true,
				InventorySource:     src.Source,
				InventorySourceKind: src.Kind,
				SyncInventoryID:     src.InventoryID,
				APIURL:              os.Getenv("API_URL"),
			}
			// Resolve the source's cloud credential (if any) into injector env/files
			// so the inventory plugin can authenticate.
			if src.CredentialID != nil {
				env, files, cerr := resolveCredentialInjectors(ctx, tx, *src.CredentialID)
				if cerr != nil {
					log.Printf("sync job %d: credential %d resolve failed: %v", job.ID, *src.CredentialID, cerr)
				} else {
					syncManifest.CredentialEnv = env
					syncManifest.CredentialFiles = files
				}
			}
			req := &events.ExecutionRequest{ExecutionRunID: runID, UnifiedJobID: job.ID, JobManifest: syncManifest, CreatedAt: time.Now()}
			payload, perr := json.Marshal(req)
			if perr != nil {
				return perr
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO execution_outbox (execution_run_id, payload) VALUES ($1, $2)`, runID, payload); err != nil {
				return err
			}
			log.Printf("Enqueued inventory sync for job %d (run %s, source %d)", job.ID, runID, srcID)
			continue
		}

		// 5. Resolve Project from Template - REQUIRES a template with a project
		if job.UnifiedJobTemplateID == nil {
			log.Printf("Job %d has no template - skipping (template required)", job.ID)
			logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
			continue
		}

		// Look up Template
		var template models.JobTemplate
		err = tx.GetContext(ctx, &template, "SELECT * FROM job_templates WHERE id = $1", *job.UnifiedJobTemplateID)
		if err != nil {
			log.Printf("Failed to find template %d for job %d: %v", *job.UnifiedJobTemplateID, job.ID, err)
			logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
			continue
		}

		// Sync from Git project (if provided)
		var projectURL string
		if template.ProjectID != nil {
			var project models.Project
			err = tx.GetContext(ctx, &project, "SELECT * FROM projects WHERE id = $1", *template.ProjectID)
			if err != nil {
				log.Printf("Failed to find project %d for template %s: %v", *template.ProjectID, template.Name, err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}
			projectURL = project.SCMURL
			log.Printf("Using project %s (%s) for job %d", project.Name, project.SCMURL, job.ID)
		} else {
			log.Printf("Template %s has no project - using default/inline logic for job %d", template.Name, job.ID)
		}

		// 6. Generate inventory from structured hosts and groups
		var inventoryContent string
		if template.InventoryID != nil {
			var inventory models.Inventory
			err = tx.GetContext(ctx, &inventory, "SELECT * FROM inventories WHERE id = $1", *template.InventoryID)
			if err != nil {
				log.Printf("Failed to find inventory %d for template %s: %v", *template.InventoryID, template.Name, err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}

			// Fetch all hosts in this inventory
			var hosts []models.Host
			err = tx.SelectContext(ctx, &hosts, "SELECT * FROM hosts WHERE inventory_id = $1 AND enabled = true", *template.InventoryID)
			if err != nil {
				log.Printf("Failed to fetch hosts for inventory %d: %v", *template.InventoryID, err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}

			// Fetch all groups in this inventory
			var groups []models.Group
			err = tx.SelectContext(ctx, &groups, "SELECT * FROM groups WHERE inventory_id = $1", *template.InventoryID)
			if err != nil {
				log.Printf("Failed to fetch groups for inventory %d: %v", *template.InventoryID, err)
			}

			// Build INI inventory
			inventoryContent = generateInventoryINI(tx, ctx, hosts, groups)
			log.Printf("Generated inventory for %s (Job %d) Content:\n%s", inventory.Name, job.ID, inventoryContent)
			log.Printf("Generated inventory for %s with %d hosts and %d groups for job %d", inventory.Name, len(hosts), len(groups), job.ID)

			if len(hosts) == 0 {
				log.Printf("Inventory %s has no enabled hosts - proceeding anyway to allow Ansible to handle it (e.g. localhost or group vars)", inventory.Name)
			}
		} else {
			log.Printf("Template %s has no inventory - using default localhost for job %d", template.Name, job.ID)
			// inventoryContent remains empty, Executor will default to localhost
		}

		var pbContent string
		if template.PlaybookContent != nil {
			pbContent = *template.PlaybookContent
		}

		// 7. Find the designated runner host for this inventory
		var runnerHostName string
		var runnerHostID int64
		if template.InventoryID != nil {
			var runnerHost models.Host
			err = tx.GetContext(ctx, &runnerHost, `
				SELECT * FROM hosts 
				WHERE inventory_id = $1 AND is_runner_host = true AND enabled = true
				LIMIT 1`, *template.InventoryID)
			if err == nil {
				runnerHostName = runnerHost.Name
				runnerHostID = runnerHost.ID
				log.Printf("Using runner host '%s' (ID %d) for job %d", runnerHostName, runnerHostID, job.ID)
			} else {
				// Fallback to first enabled host if no explicit runner host is set
				var firstHost models.Host
				err = tx.GetContext(ctx, &firstHost, `
					SELECT * FROM hosts 
					WHERE inventory_id = $1 AND enabled = true
					ORDER BY id LIMIT 1`, *template.InventoryID)
				if err == nil {
					runnerHostName = firstHost.Name
					runnerHostID = firstHost.ID
					log.Printf("No runner host set - using first host '%s' (ID %d) for job %d", runnerHostName, runnerHostID, job.ID)
				}
			}
		}

		// Snapshot the runner host onto the run so the reconciler can SSH back to
		// the SAME host to harvest its WAL after an outage, even if the inventory
		// or runner-host designation changes later. A 0 id means localhost (nothing
		// to snapshot — those runs aren't reconcilable over SSH).
		if runnerHostID != 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE execution_runs SET runner_host_id = $1 WHERE id = $2`, runnerHostID, runID); err != nil {
				log.Printf("job %d: snapshot runner_host_id failed: %v", job.ID, err)
			}
		}

		// Effective variables and limit: the template's defaults, overlaid by any
		// prompt-on-launch overrides the launcher supplied. The launch handler has
		// already gated those overrides by the template's ask_* flags, so anything
		// present in job_args is allowed.
		extraVars := mergeExtraVars(template.ExtraVars, job.JobArgs)
		limit := effectiveLimit(template.JobLimit, job.JobArgs)

		// When fact caching is on, ship the inventory's stored facts so the
		// host-runner can preload them into Ansible's cache before the play.
		var cachedFacts map[string]json.RawMessage
		if template.UseFactCache && template.InventoryID != nil {
			cachedFacts = loadHostFacts(ctx, tx, *template.InventoryID)
		}

		manifest := events.JobManifest{
			Inventory:       inventoryContent,
			ProjectURL:      projectURL,
			Playbook:        template.Playbook,
			PlaybookContent: pbContent,
			ExtraVars:       extraVars,
			Limit:           limit,
			UseFactCache:    template.UseFactCache,
			CachedFacts:     cachedFacts,
			RunnerHost:      runnerHostName,
			RunnerHostID:    runnerHostID,
			APIURL:          os.Getenv("API_URL"),
			GalaxyServers:   s.resolveGalaxyServers(ctx, template.OrganizationID),
		}

		// Machine credential: resolve the template's SSH credential into the
		// injector env/files the executor and host-runner apply. The Machine
		// credential type's injectors render ANSIBLE_REMOTE_USER / ANSIBLE_PASSWORD
		// and the become settings (ANSIBLE_BECOME_METHOD / ANSIBLE_BECOME_USER) as
		// env, and ANSIBLE_PRIVATE_KEY_FILE / ANSIBLE_BECOME_PASSWORD_FILE as files.
		// This credential is how Praetor authenticates to managed hosts — there is
		// no shared platform key. A remote job with no Machine credential (and no
		// per-host ansible_user/key) fails at bootstrap with a clear error.
		if template.CredentialID != nil {
			env, files, cerr := resolveCredentialInjectors(ctx, tx, *template.CredentialID)
			if cerr != nil {
				log.Printf("job %d: machine credential %d resolve failed: %v", job.ID, *template.CredentialID, cerr)
			} else {
				manifest.CredentialEnv = env
				manifest.CredentialFiles = files
			}
		}

		// Execution Pack: which self-contained Python+Ansible runtime the executor
		// pushes and the host-runner runs in. Empty leaves the default.
		if template.ExecutionPackID != nil {
			var packName string
			if err := tx.GetContext(ctx, &packName, `SELECT name FROM execution_packs WHERE id = $1`, *template.ExecutionPackID); err == nil {
				manifest.ExecutionPack = packName
			}
		}

		req := &events.ExecutionRequest{
			ExecutionRunID: runID,
			UnifiedJobID:   job.ID,
			JobManifest:    manifest,
			CreatedAt:      time.Now(),
		}

		// Enqueue the launch in the transactional outbox rather than publishing
		// inline. Publishing inside the tx was a dual-write: a publish that
		// succeeded before a failed commit would orphan a run, and a publish
		// that failed after a successful commit would strand the job in
		// 'queued' forever. The outbox row commits atomically with the run; the
		// relay delivers it.
		payload, err := json.Marshal(req)
		if err != nil {
			log.Printf("Failed to marshal execution request for run %s: %v", runID, err)
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO execution_outbox (execution_run_id, payload) VALUES ($1, $2)`,
			runID, payload,
		); err != nil {
			log.Printf("Failed to enqueue execution request for run %s: %v", runID, err)
			return err
		}
		log.Printf("Enqueued ExecutionRequest for Job %d (run %s). Playbook: %s", job.ID, runID, manifest.Playbook)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	JobsDispatched.Add(float64(len(jobs)))
	return nil
}

// relayOutbox publishes committed-but-unsent launches to the durable request
// stream and marks them sent. FOR UPDATE SKIP LOCKED makes it safe to run from
// multiple schedulers; the request stream's dedup window makes a re-publish
// after a crash (sent on the bus but not yet marked) harmless.
func (s *Scheduler) relayOutbox() error {
	ctx := context.Background()

	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type outboxRow struct {
		ID      int64           `db:"id"`
		Payload json.RawMessage `db:"payload"`
	}
	var rows []outboxRow
	if err := tx.SelectContext(ctx, &rows, `
		SELECT id, payload FROM execution_outbox
		WHERE status = 'pending'
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT 50`); err != nil {
		return fmt.Errorf("failed to select outbox rows: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	for _, row := range rows {
		var req events.ExecutionRequest
		if err := json.Unmarshal(row.Payload, &req); err != nil {
			log.Printf("outbox: dropping unparseable row %d: %v", row.ID, err)
			logExec(ctx, tx, `UPDATE execution_outbox SET status = 'failed', attempts = attempts + 1 WHERE id = $1`, row.ID)
			continue
		}
		if err := s.Publisher.PublishExecutionRequest(&req); err != nil {
			// Leave the row pending so it is retried on the next tick.
			log.Printf("outbox: publish failed for row %d (will retry): %v", row.ID, err)
			logExec(ctx, tx, `UPDATE execution_outbox SET attempts = attempts + 1 WHERE id = $1`, row.ID)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE execution_outbox SET status = 'sent', sent_at = now(), attempts = attempts + 1 WHERE id = $1`,
			row.ID,
		); err != nil {
			return fmt.Errorf("failed to mark outbox row %d sent: %w", row.ID, err)
		}
	}

	return tx.Commit()
}

func (s *Scheduler) processSchedules() error {
	ctx := context.Background()

	// Transaction
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Fetch due schedules with SKIP LOCKED
	query := `
		SELECT *
		FROM schedules
		WHERE enabled = true AND next_run <= NOW()
		FOR UPDATE SKIP LOCKED
		LIMIT 10`

	var schedules []models.Schedule
	if err := tx.SelectContext(ctx, &schedules, query); err != nil {
		return fmt.Errorf("failed to select pending schedules: %w", err)
	}

	if len(schedules) == 0 {
		return nil
	}

	for _, sched := range schedules {
		log.Printf("Processing schedule %d (%s) due at %s", sched.ID, sched.Name, sched.NextRun)

		// 2. Launch the schedule's target — a workflow run or a job template.
		if err := launchTarget(ctx, tx, sched.Name, sched.WorkflowTemplateID, sched.UnifiedJobTemplateID); err != nil {
			log.Printf("Failed to launch target for schedule %d: %v", sched.ID, err)
			continue
		}
		log.Printf("Launched target for schedule %d", sched.ID)

		// 3. (Skipped) We do NOT create execution_run here.
		// The existing processPendingJobs loop picks up 'pending' jobs with no current_run_id and handles it.

		// 5. Calculate Next Run
		rule, err := rrule.StrToRRule(sched.RRule)
		if err != nil {
			log.Printf("Invalid RRule for schedule %d: %v", sched.ID, err)
			// Disable it to stop error loop
			logExec(ctx, tx, "UPDATE schedules SET enabled = false WHERE id = $1", sched.ID)
			continue
		}

		// rrule-go: rule.After(dt, inclusive)
		next := rule.After(time.Now(), false)

		log.Printf("Schedule %d next run: %s", sched.ID, next)

		_, err = tx.ExecContext(ctx, `
			UPDATE schedules 
			SET next_run = $1, modified_at = NOW() 
			WHERE id = $2`,
			next, sched.ID)

		if err != nil {
			log.Printf("Failed to update schedule %d next_run: %v", sched.ID, err)
			continue
		}
	}

	return tx.Commit()
}

// processTimedOutJobs marks jobs that are stuck in running/queued state as failed.
// This catches cases where the host-runner crashes silently without sending events.
func (s *Scheduler) processTimedOutJobs() error {
	ctx := context.Background()

	// Heartbeat-aware reconciliation. A long-running job is NOT failed merely for
	// running a long time; it is declared lost only when its liveness signal
	// disappears. The host-runner stamps execution_runs.last_heartbeat_at every
	// ~30s during execution, so:
	lostHeartbeatGrace := 2 * time.Minute // ~4 missed heartbeats
	startGrace := 5 * time.Minute         // running but never heartbeated
	queuedTimeout := 10 * time.Minute     // never picked up by an executor

	// 1a. Reconcilable runs: a REMOTE run (has a snapshotted runner host) whose
	// heartbeat went stale is NOT declared failed — the host may have finished the
	// job and hold the authoritative WAL that never got pushed. Hand it to the
	// pull-based reconciler by moving it to 'reconciling'; the job stays as-is
	// (not errored) until the reconciler resolves the true outcome. finished_at
	// stays NULL. See services/reconciler.
	staleCond := `(
		(er.last_heartbeat_at IS NOT NULL AND er.last_heartbeat_at < now() - $1::interval)
		OR (er.last_heartbeat_at IS NULL AND er.started_at IS NOT NULL AND er.started_at < now() - $2::interval)
	)`
	rec, err := s.DB.ExecContext(ctx, `
		UPDATE execution_runs er
		SET state = 'reconciling', reconcile_after = now()
		WHERE er.state = 'running' AND er.runner_host_id IS NOT NULL AND `+staleCond,
		fmt.Sprintf("%d seconds", int(lostHeartbeatGrace.Seconds())),
		fmt.Sprintf("%d seconds", int(startGrace.Seconds())),
	)
	if err != nil {
		log.Printf("Error moving stale runs to reconciling: %v", err)
	} else if rows, _ := rec.RowsAffected(); rows > 0 {
		RunsReconciling.Add(float64(rows))
		log.Printf("Moved %d stale remote runs to reconciling", rows)
	}

	// Queue depth: jobs accepted but not yet running. Sampled once per tick.
	var depth float64
	if err := s.DB.GetContext(ctx, &depth,
		`SELECT count(*) FROM unified_jobs WHERE status IN ('pending','queued')`); err == nil {
		QueueDepth.Set(depth)
	}

	// 1b. Lost runs: a LOCAL run (no runner host — ran on the executor itself)
	// whose heartbeat went stale can't be pulled back over SSH, so it is genuinely
	// lost. Mark the run 'lost' and its job 'error' in one statement.
	result, err := s.DB.ExecContext(ctx, `
		WITH lost AS (
			UPDATE execution_runs er
			SET state = 'lost', finished_at = now()
			WHERE er.state = 'running' AND er.runner_host_id IS NULL AND `+staleCond+`
			RETURNING er.unified_job_id
		)
		UPDATE unified_jobs uj
		SET status = 'error', finished_at = now()
		FROM lost
		WHERE uj.id = lost.unified_job_id
		  AND uj.status NOT IN ('successful', 'failed', 'canceled', 'error')`,
		fmt.Sprintf("%d seconds", int(lostHeartbeatGrace.Seconds())),
		fmt.Sprintf("%d seconds", int(startGrace.Seconds())),
	)
	if err != nil {
		log.Printf("Error reconciling lost runs: %v", err)
	} else if rows, _ := result.RowsAffected(); rows > 0 {
		RunsLost.Add(float64(rows))
		log.Printf("Marked %d local runs as lost (stale/absent heartbeat)", rows)
	}

	// 2. Queued too long: never picked up by an executor. With the durable
	// outbox this should be rare, but it remains a safety net.
	result, err = s.DB.ExecContext(ctx, `
		WITH stuck AS (
			UPDATE unified_jobs uj
			SET status = 'failed', finished_at = now()
			WHERE uj.status = 'queued'
			  AND uj.current_run_id IS NOT NULL
			  AND uj.created_at < now() - $1::interval
			RETURNING uj.current_run_id
		)
		UPDATE execution_runs er
		SET state = 'failed', finished_at = now()
		FROM stuck
		WHERE er.id = stuck.current_run_id
		  AND er.state NOT IN ('successful', 'failed', 'canceled', 'lost')`,
		fmt.Sprintf("%d seconds", int(queuedTimeout.Seconds())),
	)
	if err != nil {
		log.Printf("Error reconciling stuck queued jobs: %v", err)
	} else if rows, _ := result.RowsAffected(); rows > 0 {
		log.Printf("Marked %d queued jobs as failed (never started)", rows)
	}

	return nil
}

func findFile(fs billy.Filesystem, path string) (billy.File, error) {
	// Try direct
	f, err := fs.Open(path)
	if err == nil {
		return f, nil
	}

	// Try with leading slash
	if len(path) > 0 && path[0] != '/' {
		f, err = fs.Open("/" + path)
		if err == nil {
			return f, nil
		}
	}

	// Walk to find match
	var foundPath string
	_ = util.Walk(fs, "/", func(p string, info os.FileInfo, err error) error {
		if foundPath != "" {
			return nil
		}

		// Debug log
		// log.Printf(" - Visiting: %s", p)

		// Match strict but ignoring leading slash for comparison
		cleanP := p
		if len(cleanP) > 0 && cleanP[0] == '/' {
			cleanP = cleanP[1:]
		}
		cleanTarget := path
		if len(cleanTarget) > 0 && cleanTarget[0] == '/' {
			cleanTarget = cleanTarget[1:]
		}

		if cleanP == cleanTarget {
			foundPath = p
		}
		return nil
	})

	if foundPath != "" {
		return fs.Open(foundPath)
	}

	return nil, fmt.Errorf("file not found: %s", path)
}

// generateInventoryINI converts structured hosts and groups to Ansible INI format
func generateInventoryINI(tx *sqlx.Tx, ctx context.Context, hosts []models.Host, groups []models.Group) string {
	var sb strings.Builder

	// Build map of host ID to groups
	hostGroups := make(map[int64][]string)
	ungroupedHosts := make(map[int64]bool)

	for _, h := range hosts {
		ungroupedHosts[h.ID] = true
	}

	// Process each group
	for _, g := range groups {
		// Get hosts in this group. Membership is stored in host_groups (written by
		// the API/UI, inventory import and dynamic sync alike — consolidated in
		// migration 000030).
		var groupHostIDs []int64
		tx.SelectContext(ctx, &groupHostIDs, "SELECT host_id FROM host_groups WHERE group_id = $1", g.ID)

		if len(groupHostIDs) > 0 {
			sb.WriteString(fmt.Sprintf("[%s]\n", g.Name))

			for _, hostID := range groupHostIDs {
				// Find the host
				for _, h := range hosts {
					if h.ID == hostID {
						sb.WriteString(formatHostLine(h))
						delete(ungroupedHosts, h.ID)
						hostGroups[h.ID] = append(hostGroups[h.ID], g.Name)
						break
					}
				}
			}
			sb.WriteString("\n")
		}
	}

	// Add ungrouped hosts under [all] if any
	if len(ungroupedHosts) > 0 {
		sb.WriteString("[ungrouped]\n")
		for _, h := range hosts {
			if ungroupedHosts[h.ID] {
				sb.WriteString(formatHostLine(h))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatHostLine formats a host with its variables for INI
func formatHostLine(h models.Host) string {
	var sb strings.Builder
	sb.WriteString(h.Name)

	// Parse variables JSON
	// Parse variables JSON
	var vars map[string]interface{}
	if h.Variables != nil {
		_ = json.Unmarshal(h.Variables, &vars)
	}
	if vars == nil {
		vars = make(map[string]interface{})
	}

	// Inject ControlMaster=no to prevent Docker crashes
	if val, ok := vars["ansible_ssh_common_args"]; ok {
		vars["ansible_ssh_common_args"] = fmt.Sprintf("%v -o ControlMaster=no", val)
	} else {
		vars["ansible_ssh_common_args"] = "-o StrictHostKeyChecking=no -o ControlMaster=no"
	}

	for k, v := range vars {
		// Quote string values if they contain spaces
		strVal := fmt.Sprintf("%v", v)
		if strings.Contains(strVal, " ") {
			sb.WriteString(fmt.Sprintf(" %s=\"%s\"", k, strVal))
		} else {
			sb.WriteString(fmt.Sprintf(" %s=%s", k, strVal))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

// packProjectToTar creates a base64-encoded tar.gz of the entire in-memory filesystem
func packProjectToTar(fs billy.Filesystem) (string, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	err := util.Walk(fs, "/", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip root
		if path == "/" {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Use relative path (strip leading /)
		if len(path) > 0 && path[0] == '/' {
			header.Name = path[1:]
		} else {
			header.Name = path
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content if not a directory
		if !info.IsDir() {
			file, err := fs.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gzw.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
