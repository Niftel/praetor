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
		SELECT id, name, unified_job_template_id, status 
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

		// 5. Resolve Project from Template - REQUIRES a template with a project
		if job.UnifiedJobTemplateID == nil {
			log.Printf("Job %d has no template - skipping (template required)", job.ID)
			_, _ = tx.ExecContext(ctx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
			continue
		}

		// Look up Template
		var template models.JobTemplate
		err = tx.GetContext(ctx, &template, "SELECT * FROM job_templates WHERE id = $1", *job.UnifiedJobTemplateID)
		if err != nil {
			log.Printf("Failed to find template %d for job %d: %v", *job.UnifiedJobTemplateID, job.ID, err)
			_, _ = tx.ExecContext(ctx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
			continue
		}

		// Sync from Git project (if provided)
		var projectURL string
		if template.ProjectID != nil {
			var project models.Project
			err = tx.GetContext(ctx, &project, "SELECT * FROM projects WHERE id = $1", *template.ProjectID)
			if err != nil {
				log.Printf("Failed to find project %d for template %s: %v", *template.ProjectID, template.Name, err)
				_, _ = tx.ExecContext(ctx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
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
				_, _ = tx.ExecContext(ctx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
				continue
			}

			// Fetch all hosts in this inventory
			var hosts []models.Host
			err = tx.SelectContext(ctx, &hosts, "SELECT * FROM hosts WHERE inventory_id = $1 AND enabled = true", *template.InventoryID)
			if err != nil {
				log.Printf("Failed to fetch hosts for inventory %d: %v", *template.InventoryID, err)
				_, _ = tx.ExecContext(ctx, "UPDATE unified_jobs SET status = 'failed' WHERE id = $1", job.ID)
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

		manifest := events.JobManifest{
			Inventory:       inventoryContent,
			ProjectURL:      projectURL,
			Playbook:        template.Playbook,
			PlaybookContent: pbContent,
			ExtraVars:       map[string]interface{}{},
			EnvironmentRefs: []string{},
			RunnerHost:      runnerHostName,
			RunnerHostID:    runnerHostID,
			APIURL:          os.Getenv("API_URL"),
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

	return tx.Commit()
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
			_, _ = tx.ExecContext(ctx, `UPDATE execution_outbox SET status = 'failed', attempts = attempts + 1 WHERE id = $1`, row.ID)
			continue
		}
		if err := s.Publisher.PublishExecutionRequest(&req); err != nil {
			// Leave the row pending so it is retried on the next tick.
			log.Printf("outbox: publish failed for row %d (will retry): %v", row.ID, err)
			_, _ = tx.ExecContext(ctx, `UPDATE execution_outbox SET attempts = attempts + 1 WHERE id = $1`, row.ID)
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

		// 2. Launch Job
		var jobID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at)
			VALUES ($1, $2, 'pending', $3)
			RETURNING id`,
			sched.Name, sched.UnifiedJobTemplateID, time.Now(),
		).Scan(&jobID)
		if err != nil {
			log.Printf("Failed to spawn job for schedule %d: %v", sched.ID, err)
			continue
		}
		log.Printf("Spawned job %d from schedule %d", jobID, sched.ID)

		// 3. (Skipped) We do NOT create execution_run here.
		// The existing processPendingJobs loop picks up 'pending' jobs with no current_run_id and handles it.

		// 5. Calculate Next Run
		rule, err := rrule.StrToRRule(sched.RRule)
		if err != nil {
			log.Printf("Invalid RRule for schedule %d: %v", sched.ID, err)
			// Disable it to stop error loop
			_, _ = tx.ExecContext(ctx, "UPDATE schedules SET enabled = false WHERE id = $1", sched.ID)
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

	// Find jobs that have been running/queued for too long without completion
	// Timeout: 30 minutes for running, 10 minutes for queued (should have started)
	timeout := 30 * time.Minute
	queuedTimeout := 10 * time.Minute

	// Mark long-running jobs as failed
	result, err := s.DB.ExecContext(ctx, `
		UPDATE unified_jobs 
		SET status = 'failed', finished_at = NOW()
		WHERE status = 'running' 
		AND started_at IS NOT NULL 
		AND started_at < NOW() - $1::interval
		RETURNING id`, fmt.Sprintf("%d seconds", int(timeout.Seconds())))
	if err != nil {
		log.Printf("Error checking running timeout: %v", err)
	} else if rows, _ := result.RowsAffected(); rows > 0 {
		log.Printf("Marked %d running jobs as failed due to timeout", rows)
	}

	// Mark long-queued jobs as failed (should have transitioned to running)
	result, err = s.DB.ExecContext(ctx, `
		UPDATE unified_jobs 
		SET status = 'failed', finished_at = NOW()
		WHERE status = 'queued' 
		AND current_run_id IS NOT NULL
		AND created_at < NOW() - $1::interval
		RETURNING id`, fmt.Sprintf("%d seconds", int(queuedTimeout.Seconds())))
	if err != nil {
		log.Printf("Error checking queued timeout: %v", err)
	} else if rows, _ := result.RowsAffected(); rows > 0 {
		log.Printf("Marked %d queued jobs as failed due to timeout", rows)
	}

	// Also update corresponding execution_runs
	_, _ = s.DB.ExecContext(ctx, `
		UPDATE execution_runs SET state = 'failed', finished_at = NOW()
		WHERE id IN (
			SELECT current_run_id FROM unified_jobs 
			WHERE status = 'failed' AND finished_at > NOW() - INTERVAL '1 minute'
		) AND state NOT IN ('successful', 'failed')`)

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
		// Get hosts in this group
		var groupHostIDs []int64
		tx.SelectContext(ctx, &groupHostIDs, "SELECT host_id FROM host_group_mapping WHERE group_id = $1", g.ID)

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
