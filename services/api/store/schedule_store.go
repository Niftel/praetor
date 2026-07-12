package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
)

// ScheduleStore is the data-access layer for the schedules domain. A schedule has
// no org column of its own; it inherits its target's (job/workflow template) org,
// so the org-resolution helpers live here too.
type ScheduleStore struct {
	db *sqlx.DB
}

func NewScheduleStore(db *sqlx.DB) *ScheduleStore { return &ScheduleStore{db: db} }

// ListAll returns all schedules (superuser/auditor view).
func (s *ScheduleStore) ListAll(ctx context.Context) ([]models.Schedule, error) {
	schedules := []models.Schedule{}
	err := s.db.SelectContext(ctx, &schedules, "SELECT "+ScheduleCols+" FROM schedules ORDER BY id ASC")
	return schedules, wrap("ScheduleStore.ListAll", err)
}

// ListByTargetOrgIDs returns schedules whose target template lives in one of orgIDs.
func (s *ScheduleStore) ListByTargetOrgIDs(ctx context.Context, orgIDs []int64) ([]models.Schedule, error) {
	schedules := []models.Schedule{}
	if len(orgIDs) == 0 {
		return schedules, nil
	}
	q, args, err := sqlx.In(`
		SELECT `+Prefixed("s", ScheduleCols)+` FROM schedules s
		LEFT JOIN workflow_templates wt ON wt.id = s.workflow_template_id
		LEFT JOIN job_templates jt ON jt.unified_job_template_id = s.unified_job_template_id
		WHERE COALESCE(wt.organization_id, jt.organization_id) IN (?)
		ORDER BY s.id ASC`, orgIDs)
	if err != nil {
		return nil, wrap("ScheduleStore.ListByTargetOrgIDs", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &schedules, q, args...)
	return schedules, wrap("ScheduleStore.ListByTargetOrgIDs", err)
}

// Get returns a single schedule by id.
func (s *ScheduleStore) Get(ctx context.Context, id int64) (models.Schedule, error) {
	var sched models.Schedule
	err := s.db.GetContext(ctx, &sched, "SELECT "+ScheduleCols+" FROM schedules WHERE id = $1", id)
	return sched, wrap("ScheduleStore.Get", err)
}

// Create inserts a schedule (fields already computed by the caller) and returns
// the new id.
func (s *ScheduleStore) Create(ctx context.Context, sched models.Schedule) (int64, error) {
	query := `
		INSERT INTO schedules (name, description, unified_job_template_id, workflow_template_id, rrule, next_run, enabled, extra_vars, created_at, modified_at)
		VALUES (:name, :description, :unified_job_template_id, :workflow_template_id, :rrule, :next_run, :enabled, :extra_vars, :created_at, :modified_at)
		RETURNING id`
	rows, err := s.db.NamedQuery(query, sched)
	if err != nil {
		return 0, wrap("ScheduleStore.Create", err)
	}
	defer rows.Close()
	var id int64
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return 0, wrap("ScheduleStore.Create", err)
		}
	}
	return id, nil
}

// Update applies an edit to a schedule.
func (s *ScheduleStore) Update(ctx context.Context, sched models.Schedule) error {
	query := `
		UPDATE schedules
		SET name=:name, description=:description, rrule=:rrule, next_run=:next_run,
		    enabled=:enabled, extra_vars=:extra_vars, modified_at=:modified_at,
		    unified_job_template_id=:unified_job_template_id,
		    workflow_template_id=:workflow_template_id
		WHERE id = :id`
	_, err := s.db.NamedExecContext(ctx, query, sched)
	return wrap("ScheduleStore.Update", err)
}

// Delete removes a schedule by id.
func (s *ScheduleStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM schedules WHERE id = $1", id)
	return wrap("ScheduleStore.Delete", err)
}

// TargetOrg resolves the organization of a schedule's target (workflow XOR job
// template). ok is false when neither target resolves.
func (s *ScheduleStore) TargetOrg(ctx context.Context, wfID, ujtID *int64) (int64, bool) {
	var org int64
	switch {
	case wfID != nil:
		err := s.db.GetContext(ctx, &org, `SELECT organization_id FROM workflow_templates WHERE id=$1`, *wfID)
		return org, err == nil
	case ujtID != nil:
		err := s.db.GetContext(ctx, &org, `SELECT organization_id FROM job_templates WHERE unified_job_template_id=$1`, *ujtID)
		return org, err == nil
	}
	return 0, false
}

// ScheduleOrg resolves the org a schedule inherits from its target, by id.
func (s *ScheduleStore) ScheduleOrg(ctx context.Context, id int64) (int64, bool) {
	var org sql.NullInt64
	err := s.db.GetContext(ctx, &org, `
		SELECT COALESCE(wt.organization_id, jt.organization_id)
		FROM schedules s
		LEFT JOIN workflow_templates wt ON wt.id = s.workflow_template_id
		LEFT JOIN job_templates jt ON jt.unified_job_template_id = s.unified_job_template_id
		WHERE s.id=$1`, id)
	if err != nil || !org.Valid {
		return 0, false
	}
	return org.Int64, true
}
