package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// EventIntakeSource is the minimal source row the public intake path verifies against.
type EventIntakeSource struct {
	ID    int64  `db:"id"`
	OrgID int64  `db:"organization_id"`
	Token string `db:"token"`
}

// EventRuleMatch is a rule as evaluated during intake.
type EventRuleMatch struct {
	ID         int64          `db:"id"`
	Name       string         `db:"name"`
	Condition  string         `db:"condition"`
	UJTID      sql.NullInt64  `db:"unified_job_template_id"`
	WfID       sql.NullInt64  `db:"workflow_template_id"`
	LimitField sql.NullString `db:"limit_field"`
}

// EventSource is an authenticated event source (token secret write/create-only).
type EventSource struct {
	ID             int64     `json:"id" db:"id"`
	OrganizationID int64     `json:"organization_id" db:"organization_id"`
	Name           string    `json:"name" db:"name"`
	Token          string    `json:"token,omitempty" db:"token"`
	Enabled        *bool     `json:"enabled" db:"enabled"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

// EventRule is a source -> condition -> target automation rule.
type EventRule struct {
	ID                   int64     `json:"id" db:"id"`
	OrganizationID       int64     `json:"organization_id" db:"organization_id"`
	Name                 string    `json:"name" db:"name"`
	Enabled              *bool     `json:"enabled" db:"enabled"`
	SourceID             *int64    `json:"source_id,omitempty" db:"source_id"`
	Condition            string    `json:"condition" db:"condition"`
	UnifiedJobTemplateID *int64    `json:"unified_job_template_id,omitempty" db:"unified_job_template_id"`
	WorkflowTemplateID   *int64    `json:"workflow_template_id,omitempty" db:"workflow_template_id"`
	LimitField           *string   `json:"limit_field,omitempty" db:"limit_field"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
}

// EventStore is the data-access layer for the event-driven automation domain.
type EventStore struct {
	db *sqlx.DB
}

func NewEventStore(db *sqlx.DB) *EventStore { return &EventStore{db: db} }

// IntakeSource loads an enabled source by name for the public intake path.
func (s *EventStore) IntakeSource(ctx context.Context, name string) (EventIntakeSource, error) {
	var src EventIntakeSource
	err := s.db.GetContext(ctx, &src,
		`SELECT id, organization_id, token FROM event_sources WHERE name=$1 AND enabled`, name)
	return src, err
}

// RulesForIntake returns enabled rules in a source's org bound to it or global.
func (s *EventStore) RulesForIntake(ctx context.Context, orgID, sourceID int64) ([]EventRuleMatch, error) {
	rules := []EventRuleMatch{}
	err := s.db.SelectContext(ctx, &rules,
		`SELECT id, name, condition, unified_job_template_id, workflow_template_id, limit_field
		 FROM event_rules
		 WHERE enabled AND organization_id=$1 AND (source_id=$2 OR source_id IS NULL)
		 ORDER BY id`, orgID, sourceID)
	return rules, err
}

// InsertReceipt records an intake receipt (best-effort by the caller).
func (s *EventStore) InsertReceipt(ctx context.Context, sourceID int64, payload []byte, matched int, launched []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO event_receipts (source_id, payload, matched, launched) VALUES ($1,$2,$3,$4)`,
		sourceID, payload, matched, launched)
	return err
}

// JobTemplateAllowSimultaneous reports whether a template permits overlapping runs.
func (s *EventStore) JobTemplateAllowSimultaneous(ctx context.Context, unifiedTemplateID int64) bool {
	var allow bool
	_ = s.db.GetContext(ctx, &allow, `SELECT allow_simultaneous FROM job_templates WHERE unified_job_template_id=$1`, unifiedTemplateID)
	return allow
}

// ActiveJobCount counts non-terminal jobs for a unified template.
func (s *EventStore) ActiveJobCount(ctx context.Context, unifiedTemplateID int64) int {
	var active int
	_ = s.db.GetContext(ctx, &active,
		`SELECT count(*) FROM unified_jobs WHERE unified_job_template_id=$1 AND status NOT IN ('successful','failed','canceled','error')`,
		unifiedTemplateID)
	return active
}

// InsertEventJob creates a pending unified_job from an event rule; the raw error
// is returned so the caller can detect an active-run conflict.
func (s *EventStore) InsertEventJob(ctx context.Context, name string, unifiedTemplateID int64, jobArgs []byte) (int64, error) {
	var id int64
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
		 VALUES ($1,$2,'pending',now(),$3) RETURNING id`,
		name, unifiedTemplateID, jobArgs).Scan(&id)
	return id, err
}

// LaunchWorkflowSnapshot snapshots a workflow template into a running run.
func (s *EventStore) LaunchWorkflowSnapshot(ctx context.Context, workflowTemplateID int64) (int64, error) {
	tx, err := s.db.Beginx()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var wjID int64
	if err := tx.QueryRowxContext(ctx,
		`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1,'running') RETURNING id`, workflowTemplateID).Scan(&wjID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body, status)
		 SELECT $1, node_key, node_type, job_template_id, name, webhook_url, webhook_body, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
		wjID, workflowTemplateID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
		 SELECT $1, parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$2`,
		wjID, workflowTemplateID); err != nil {
		return 0, err
	}
	return wjID, tx.Commit()
}

// ListSources returns all event sources (token masked).
func (s *EventStore) ListSources(ctx context.Context) ([]EventSource, error) {
	out := []EventSource{}
	err := s.db.SelectContext(ctx, &out,
		`SELECT id, organization_id, name, '' AS token, enabled, created_at FROM event_sources ORDER BY name`)
	return out, err
}

// CreateSource inserts an event source and returns it (token included once).
func (s *EventStore) CreateSource(ctx context.Context, in EventSource) (EventSource, error) {
	var created EventSource
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO event_sources (organization_id, name, token, enabled)
		 VALUES ($1,$2,$3, COALESCE($4,true))
		 RETURNING id, organization_id, name, token, enabled, created_at`,
		in.OrganizationID, in.Name, in.Token, in.Enabled).StructScan(&created)
	return created, err
}

// DeleteSource removes an event source.
func (s *EventStore) DeleteSource(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM event_sources WHERE id=$1`, id)
	return err
}

// ListRules returns all event rules.
func (s *EventStore) ListRules(ctx context.Context) ([]EventRule, error) {
	out := []EventRule{}
	err := s.db.SelectContext(ctx, &out,
		`SELECT id, organization_id, name, enabled, source_id, condition, unified_job_template_id, workflow_template_id, limit_field, created_at
		 FROM event_rules ORDER BY id`)
	return out, err
}

// CreateRule inserts an event rule (condition pre-validated by the caller).
func (s *EventStore) CreateRule(ctx context.Context, in EventRule) (EventRule, error) {
	var created EventRule
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO event_rules (organization_id, name, enabled, source_id, condition, unified_job_template_id, workflow_template_id, limit_field)
		 VALUES ($1,$2, COALESCE($3,true), $4,$5,$6,$7,$8)
		 RETURNING id, organization_id, name, enabled, source_id, condition, unified_job_template_id, workflow_template_id, limit_field, created_at`,
		in.OrganizationID, in.Name, in.Enabled, in.SourceID, in.Condition, in.UnifiedJobTemplateID, in.WorkflowTemplateID, in.LimitField).StructScan(&created)
	return created, err
}

// DeleteRule removes an event rule.
func (s *EventStore) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM event_rules WHERE id=$1`, id)
	return err
}
