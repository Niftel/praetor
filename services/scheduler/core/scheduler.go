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
	"os"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/util"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/events"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/objectstore"
	"github.com/praetordev/praetor/pkg/plog"
	"github.com/teambition/rrule-go"
)

// logger is the scheduler package's structured, component-tagged logger. The
// composition root installs the handler (see pkg/plog); this tags every record
// with component=scheduler across the package's files.
var logger = plog.New("scheduler")

type Scheduler struct {
	DB        *sqlx.DB
	Ticker    *time.Ticker
	Publisher EventPublisher

	// Retention pruning (opt-in): when RetentionDays > 0, terminal jobs finished
	// longer ago than that are deleted — their log blobs removed from Logs, then
	// the job rows (runs/events/chunks/outbox cascade). Logs may be nil (skips
	// blob cleanup). See pruner.go.
	Logs          objectstore.LogStore
	RetentionDays int
	lastPrune     time.Time

	// APIURL is the control-plane base URL embedded in the job manifest so the
	// pushed host-runner knows where to report back. Resolved in main from env;
	// empty is valid (callers fall back to the in-cluster default).
	APIURL string
}

func NewScheduler(db *sqlx.DB, interval time.Duration, publisher EventPublisher) *Scheduler {
	return &Scheduler{
		DB:        db,
		Ticker:    time.NewTicker(interval),
		Publisher: publisher,
	}
}

// tickTask is one pass of the scheduler tick. Splitting the tick into named tasks
// keeps each pass independently testable and gives per-task error visibility
// instead of one monolithic loop body.
type tickTask struct {
	name string
	run  func(ctx context.Context) error
}

// tickTasks returns the ordered passes performed every tick. Order is
// significant: claim → relay → schedules → timeouts → workflows → triggers →
// prune. Passes that log internally (workflows/triggers/prune) return nil.
func (s *Scheduler) tickTasks() []tickTask {
	return []tickTask{
		{"pending_jobs", s.processPendingJobs},
		{"relay_outbox", s.relayOutbox},
		{"schedules", s.processSchedules},
		{"timeouts", s.processTimedOutJobs},
		{"workflows", func(ctx context.Context) error { s.processWorkflows(ctx); return nil }},
		{"event_triggers", func(ctx context.Context) error { s.processEventTriggers(ctx); return nil }},
		{"prune", func(ctx context.Context) error { s.maybePrune(ctx); return nil }},
	}
}

// Start runs the tick loop until ctx is canceled. Cancellation is the only stop
// signal (no separate Done channel); the ticker is stopped on exit.
func (s *Scheduler) Start(ctx context.Context) {
	defer s.Ticker.Stop()
	tasks := s.tickTasks()
	logger.Info("scheduler started")
	for {
		select {
		case <-ctx.Done():
			logger.Info("scheduler stopped")
			return
		case <-s.Ticker.C:
			s.runTick(ctx, tasks)
		}
	}
}

// runTick executes every tick task in order, isolating each task's error so one
// failing pass neither aborts the tick nor is silently swallowed.
func (s *Scheduler) runTick(ctx context.Context, tasks []tickTask) {
	tickStart := time.Now()
	for _, t := range tasks {
		if err := t.run(ctx); err != nil {
			logger.Error("tick task failed", "task", t.name, "err", err)
			TickTaskErrors.WithLabelValues(t.name).Inc()
		}
	}
	TickDuration.Observe(time.Since(tickStart).Seconds())
}

func (s *Scheduler) processPendingJobs(ctx context.Context) error {
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
		WHERE status = 'pending' AND current_run_id IS NULL AND NOT cancel_requested
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
			logger.Error("create run for job failed", "job_id", job.ID, "err", err)
			return err // Rollback
		}

		// 4. Update Job
		_, err = tx.ExecContext(ctx, `
			UPDATE unified_jobs 
			SET status = 'queued', current_run_id = $1 
			WHERE id = $2`, runID, job.ID)

		if err != nil {
			logger.Error("update job failed", "job_id", job.ID, "err", err)
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
				logger.Error("inventory sync source not found", "job_id", job.ID, "source_id", srcID, "err", err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status='failed' WHERE id=$1", job.ID)
				continue
			}
			syncManifest := events.JobManifest{
				InventorySync:       true,
				InventorySource:     src.Source,
				InventorySourceKind: src.Kind,
				SyncInventoryID:     src.InventoryID,
				APIURL:              s.APIURL,
			}
			// Reference only: the executor resolves the source's cloud credential at
			// dispatch from ingestion (no plaintext at rest). Snapshot the id on the
			// run so resolution is run-scoped.
			if src.CredentialID != nil {
				syncManifest.CredentialID = *src.CredentialID
				if _, uerr := tx.ExecContext(ctx,
					`UPDATE execution_runs SET credential_id = $1 WHERE id = $2`, *src.CredentialID, runID); uerr != nil {
					logger.Error("snapshot credential id on run failed", "job_id", job.ID, "run_id", runID, "err", uerr)
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
			logger.Info("enqueued inventory sync", "job_id", job.ID, "run_id", runID, "source_id", srcID)
			continue
		}

		// 5. Resolve Project from Template - REQUIRES a template with a project
		if job.UnifiedJobTemplateID == nil {
			logger.Warn("job has no template - skipping (template required)", "job_id", job.ID)
			logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
			continue
		}

		// Look up Template
		var template models.JobTemplate
		err = tx.GetContext(ctx, &template, "SELECT * FROM job_templates WHERE id = $1", *job.UnifiedJobTemplateID)
		if err != nil {
			logger.Error("find template for job failed", "template_id", *job.UnifiedJobTemplateID, "job_id", job.ID, "err", err)
			logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
			continue
		}

		// Sync from Git project (if provided)
		var projectURL string
		if template.ProjectID != nil {
			var project models.Project
			err = tx.GetContext(ctx, &project, "SELECT * FROM projects WHERE id = $1", *template.ProjectID)
			if err != nil {
				logger.Error("find project for template failed", "project_id", *template.ProjectID, "template", template.Name, "err", err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}
			projectURL = project.SCMURL
			logger.Info("using project for job", "project", project.Name, "scm_url", project.SCMURL, "job_id", job.ID)
		} else {
			logger.Info("template has no project - using default/inline logic", "template", template.Name, "job_id", job.ID)
		}

		// 6. Generate inventory from structured hosts and groups
		var inventoryContent string
		if template.InventoryID != nil {
			var inventory models.Inventory
			err = tx.GetContext(ctx, &inventory, "SELECT * FROM inventories WHERE id = $1", *template.InventoryID)
			if err != nil {
				logger.Error("find inventory for template failed", "inventory_id", *template.InventoryID, "template", template.Name, "err", err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}

			// Fetch all hosts in this inventory
			var hosts []models.Host
			err = tx.SelectContext(ctx, &hosts, "SELECT * FROM hosts WHERE inventory_id = $1 AND enabled = true", *template.InventoryID)
			if err != nil {
				logger.Error("fetch hosts for inventory failed", "inventory_id", *template.InventoryID, "err", err)
				logExec(ctx, tx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}

			// Fetch all groups in this inventory
			var groups []models.Group
			err = tx.SelectContext(ctx, &groups, "SELECT * FROM groups WHERE inventory_id = $1", *template.InventoryID)
			if err != nil {
				logger.Error("fetch groups for inventory failed", "inventory_id", *template.InventoryID, "err", err)
			}

			// Build INI inventory
			inventoryContent = generateInventoryINI(tx, ctx, hosts, groups)
			logger.Debug("generated inventory", "inventory", inventory.Name, "job_id", job.ID, "content", inventoryContent)
			logger.Info("generated inventory", "inventory", inventory.Name, "hosts", len(hosts), "groups", len(groups), "job_id", job.ID)

			if len(hosts) == 0 {
				logger.Warn("inventory has no enabled hosts - proceeding (localhost/group vars may apply)", "inventory", inventory.Name)
			}
		} else {
			logger.Info("template has no inventory - using default localhost", "template", template.Name, "job_id", job.ID)
			// inventoryContent remains empty, Executor will default to localhost
		}

		// Inline playbooks are disabled: playbooks come only from a source-control
		// project (never dispatch template.PlaybookContent, even if a legacy row
		// still has it). The API rejects inline content on create/update.

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
				logger.Info("using runner host", "host", runnerHostName, "host_id", runnerHostID, "job_id", job.ID)
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
					logger.Info("no runner host set - using first host", "host", runnerHostName, "host_id", runnerHostID, "job_id", job.ID)
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
				logger.Error("snapshot runner_host_id failed", "job_id", job.ID, "err", err)
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
			PlaybookContent: "", // inline playbooks disabled — SCM projects only
			ExtraVars:       extraVars,
			Limit:           limit,
			UseFactCache:    template.UseFactCache,
			CachedFacts:     cachedFacts,
			RunnerHost:      runnerHostName,
			RunnerHostID:    runnerHostID,
			APIURL:          s.APIURL,
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
			// Reference only: the manifest carries the credential id and we snapshot
			// it on the run; the executor resolves the injectors at dispatch from
			// ingestion, so no plaintext key is persisted in the outbox or NATS (#11).
			// Snapshotting on the run keeps resolution strictly run-scoped (000045).
			manifest.CredentialID = *template.CredentialID
			if _, uerr := tx.ExecContext(ctx,
				`UPDATE execution_runs SET credential_id = $1 WHERE id = $2`, *template.CredentialID, runID); uerr != nil {
				logger.Error("snapshot credential id on run failed", "job_id", job.ID, "run_id", runID, "err", uerr)
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
			logger.Error("marshal execution request failed", "run_id", runID, "err", err)
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO execution_outbox (execution_run_id, payload) VALUES ($1, $2)`,
			runID, payload,
		); err != nil {
			logger.Error("enqueue execution request failed", "run_id", runID, "err", err)
			return err
		}
		logger.Info("enqueued execution request", "job_id", job.ID, "run_id", runID, "playbook", manifest.Playbook)
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
func (s *Scheduler) relayOutbox(ctx context.Context) error {
	// Recover orphaned claims: rows left 'sending' by a relay that crashed after
	// claiming but before publishing/marking. Only stale claims are reset so a
	// concurrent scheduler's in-flight batch isn't disturbed; the request stream's
	// dedup window makes any resulting re-publish harmless.
	if _, err := s.DB.ExecContext(ctx, `
		UPDATE execution_outbox SET status = 'pending'
		WHERE status = 'sending' AND sent_at < now() - interval '2 minutes'`); err != nil {
		logger.Error("outbox: recover stale claims failed", "err", err)
	}

	// Atomically claim a batch. The single UPDATE ... RETURNING takes and releases
	// its row locks within the statement, so the NATS publishes below run with no
	// open transaction and no locks held — a publish can no longer be stranded
	// inside an uncommitted tx (the previous dual-write hazard).
	type outboxRow struct {
		ID      int64           `db:"id"`
		Payload json.RawMessage `db:"payload"`
	}
	var rows []outboxRow
	if err := s.DB.SelectContext(ctx, &rows, `
		UPDATE execution_outbox SET status = 'sending', sent_at = now()
		WHERE id IN (
			SELECT id FROM execution_outbox
			WHERE status = 'pending'
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT 50
		)
		RETURNING id, payload`); err != nil {
		return fmt.Errorf("failed to claim outbox rows: %w", err)
	}

	for _, row := range rows {
		var req events.ExecutionRequest
		if err := json.Unmarshal(row.Payload, &req); err != nil {
			logger.Error("outbox: dropping unparseable row", "row_id", row.ID, "err", err)
			logExec(ctx, s.DB, `UPDATE execution_outbox SET status = 'failed', attempts = attempts + 1 WHERE id = $1`, row.ID)
			continue
		}
		if err := s.Publisher.PublishExecutionRequest(&req); err != nil {
			// Return the row to the queue for the next tick.
			logger.Error("outbox: publish failed (will retry)", "row_id", row.ID, "err", err)
			logExec(ctx, s.DB, `UPDATE execution_outbox SET status = 'pending', attempts = attempts + 1 WHERE id = $1`, row.ID)
			continue
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE execution_outbox SET status = 'sent', sent_at = now(), attempts = attempts + 1 WHERE id = $1`,
			row.ID); err != nil {
			// Published but couldn't mark sent; leaving it 'sending' means stale-claim
			// recovery requeues it and dedup makes the re-publish harmless.
			logger.Error("outbox: published row but failed to mark sent", "row_id", row.ID, "err", err)
		}
	}
	return nil
}

func (s *Scheduler) processSchedules(ctx context.Context) error {
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
		logger.Info("processing schedule", "schedule_id", sched.ID, "name", sched.Name, "due_at", sched.NextRun)

		// 2. Launch the schedule's target — a workflow run or a job template.
		if err := launchTarget(ctx, tx, sched.Name, sched.WorkflowTemplateID, sched.UnifiedJobTemplateID); err != nil {
			logger.Error("launch target for schedule failed", "schedule_id", sched.ID, "err", err)
			continue
		}
		logger.Info("launched target for schedule", "schedule_id", sched.ID)

		// 3. (Skipped) We do NOT create execution_run here.
		// The existing processPendingJobs loop picks up 'pending' jobs with no current_run_id and handles it.

		// 5. Calculate Next Run
		rule, err := rrule.StrToRRule(sched.RRule)
		if err != nil {
			logger.Error("invalid RRule for schedule; disabling", "schedule_id", sched.ID, "err", err)
			// Disable it to stop error loop
			logExec(ctx, tx, "UPDATE schedules SET enabled = false WHERE id = $1", sched.ID)
			continue
		}

		// rrule-go: rule.After(dt, inclusive)
		next := rule.After(time.Now(), false)

		logger.Info("schedule next run computed", "schedule_id", sched.ID, "next_run", next)

		_, err = tx.ExecContext(ctx, `
			UPDATE schedules 
			SET next_run = $1, modified_at = NOW() 
			WHERE id = $2`,
			next, sched.ID)

		if err != nil {
			logger.Error("update schedule next_run failed", "schedule_id", sched.ID, "err", err)
			continue
		}
	}

	return tx.Commit()
}

// processTimedOutJobs marks jobs that are stuck in running/queued state as failed.
// This catches cases where the host-runner crashes silently without sending events.
func (s *Scheduler) processTimedOutJobs(ctx context.Context) error {
	// Heartbeat-aware reconciliation. A long-running job is NOT failed merely for
	// running a long time; it is declared lost only when its liveness signal
	// disappears. The host-runner stamps execution_runs.last_heartbeat_at every
	// ~30s during execution, so:
	lostHeartbeatGrace := 2 * time.Minute // ~4 missed heartbeats
	startGrace := 5 * time.Minute         // running but never heartbeated
	queuedTimeout := 10 * time.Minute     // never picked up by an executor
	localRecoveryGrace := 10 * time.Minute // window for the executor to resume a local run before true-loss (#45)

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
		logger.Error("move stale runs to reconciling failed", "err", err)
	} else if rows, _ := rec.RowsAffected(); rows > 0 {
		RunsReconciling.Add(float64(rows))
		logger.Info("moved stale remote runs to reconciling", "count", rows)
	}

	// Queue depth: jobs accepted but not yet running. Sampled once per tick.
	var depth float64
	if err := s.DB.GetContext(ctx, &depth,
		`SELECT count(*) FROM unified_jobs WHERE status IN ('pending','queued')`); err == nil {
		QueueDepth.Set(depth)
	}

	// 1b. Stale LOCAL runs (no runner host — ran on the executor itself). These
	// can't be pulled back over SSH, but the executor persists the same WAL to
	// /var/lib/praetor/jobs and resumes interrupted local runs on startup (#45), so
	// a stale local run is NOT immediately lost: park it in 'reconciling' with a
	// recovery deadline (reconcile_after) and leave its job untouched, giving the
	// executor time to resume it (a resumed runner's heartbeat revives it). The SSH
	// reconciler ignores these (it JOINs on runner_host_id), so parking is safe.
	rl, err := s.DB.ExecContext(ctx, `
		UPDATE execution_runs er
		SET state = 'reconciling', reconcile_after = now() + $3::interval
		WHERE er.state = 'running' AND er.runner_host_id IS NULL AND `+staleCond,
		fmt.Sprintf("%d seconds", int(lostHeartbeatGrace.Seconds())),
		fmt.Sprintf("%d seconds", int(startGrace.Seconds())),
		fmt.Sprintf("%d seconds", int(localRecoveryGrace.Seconds())),
	)
	if err != nil {
		logger.Error("park stale local runs for recovery failed", "err", err)
	} else if rows, _ := rl.RowsAffected(); rows > 0 {
		RunsReconciling.Add(float64(rows))
		logger.Info("parked stale local runs for executor recovery", "count", rows)
	}

	// 1c. True loss: a local run still in 'reconciling' past its recovery deadline
	// was never resumed (executor gone for good / WAL unrecoverable). Now declare
	// it lost and its job errored — the delayed form of the old 1b semantics.
	result, err := s.DB.ExecContext(ctx, `
		WITH lost AS (
			UPDATE execution_runs er
			SET state = 'lost', finished_at = now()
			WHERE er.state = 'reconciling' AND er.runner_host_id IS NULL
			  AND er.reconcile_after IS NOT NULL AND er.reconcile_after < now()
			RETURNING er.unified_job_id
		)
		UPDATE unified_jobs uj
		SET status = 'error', finished_at = now()
		FROM lost
		WHERE uj.id = lost.unified_job_id
		  AND uj.status NOT IN ('successful', 'failed', 'canceled', 'error')`)
	if err != nil {
		logger.Error("reconcile lost local runs failed", "err", err)
	} else if rows, _ := result.RowsAffected(); rows > 0 {
		RunsLost.Add(float64(rows))
		logger.Warn("marked local runs as lost (recovery deadline passed)", "count", rows)
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
		  AND NOT run_is_terminal(er.state) AND er.state <> 'lost'`,
		fmt.Sprintf("%d seconds", int(queuedTimeout.Seconds())),
	)
	if err != nil {
		logger.Error("reconcile stuck queued jobs failed", "err", err)
	} else if rows, _ := result.RowsAffected(); rows > 0 {
		logger.Warn("marked queued jobs as failed (never started)", "count", rows)
	}

	// Void any still-pending outbox row whose run is already terminal. Without
	// this, a launch that was reaped above (or canceled) while its outbox row was
	// unsent — e.g. NATS was down so the relay never published — would be published
	// on recovery and bootstrap a "ghost run" the DB already calls failed. The
	// relay only picks status='pending', so flipping it to 'failed' retires it.
	if vr, verr := s.DB.ExecContext(ctx, `
		UPDATE execution_outbox o
		SET status = 'failed', attempts = attempts + 1
		FROM execution_runs er
		WHERE o.execution_run_id = er.id
		  AND o.status = 'pending'
		  AND er.state IN ('failed', 'canceled')`); verr != nil {
		logger.Error("void outbox for terminal runs failed", "err", verr)
	} else if n, _ := vr.RowsAffected(); n > 0 {
		logger.Info("voided pending outbox launches for already-terminal runs", "count", n)
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
