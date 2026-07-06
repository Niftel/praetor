package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

const eventTriggerCols = `id, organization_id, name, enabled, event_type, source_ujt_id, workflow_template_id, unified_job_template_id, created_at`

// EventTrigger launches a target when a job reaches a terminal state.
type EventTrigger struct {
	ID                   int64     `json:"id" db:"id"`
	OrganizationID       int64     `json:"organization_id" db:"organization_id"`
	Name                 string    `json:"name" db:"name"`
	Enabled              bool      `json:"enabled" db:"enabled"`
	EventType            string    `json:"event_type" db:"event_type"`
	SourceUJTID          *int64    `json:"source_ujt_id,omitempty" db:"source_ujt_id"`
	WorkflowTemplateID   *int64    `json:"workflow_template_id,omitempty" db:"workflow_template_id"`
	UnifiedJobTemplateID *int64    `json:"unified_job_template_id,omitempty" db:"unified_job_template_id"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
}

// WebhookSourceRow is a webhook-enabled source (workflow/job-template/pack).
type WebhookSourceRow struct {
	ID      int64  `db:"id"`
	Name    string `db:"name"`
	Service string `db:"service"`
}

// TriggerStore is the data-access layer for event triggers and the read-only
// inbound-webhook trigger view.
type TriggerStore struct {
	db *sqlx.DB
}

func NewTriggerStore(db *sqlx.DB) *TriggerStore { return &TriggerStore{db: db} }

// TriggerOrg returns the org owning an event trigger.
func (s *TriggerStore) TriggerOrg(ctx context.Context, id int64) (int64, bool) {
	var org int64
	err := s.db.GetContext(ctx, &org, `SELECT organization_id FROM event_triggers WHERE id=$1`, id)
	return org, err == nil
}

// ListEventAll returns all event triggers (superuser/auditor view).
func (s *TriggerStore) ListEventAll(ctx context.Context) ([]EventTrigger, error) {
	rows := []EventTrigger{}
	err := s.db.SelectContext(ctx, &rows, `SELECT `+eventTriggerCols+` FROM event_triggers ORDER BY id`)
	return rows, wrap("TriggerStore.ListEventAll", err)
}

// ListEventByOrgs returns event triggers scoped to a set of orgs.
func (s *TriggerStore) ListEventByOrgs(ctx context.Context, orgIDs []int64) ([]EventTrigger, error) {
	rows := []EventTrigger{}
	if len(orgIDs) == 0 {
		return rows, nil
	}
	q, args, err := sqlx.In(`SELECT `+eventTriggerCols+` FROM event_triggers WHERE organization_id IN (?) ORDER BY id`, orgIDs)
	if err != nil {
		return nil, wrap("TriggerStore.ListEventByOrgs", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &rows, q, args...)
	return rows, wrap("TriggerStore.ListEventByOrgs", err)
}

// CreateEvent inserts an event trigger (enabled=true) and returns it.
func (s *TriggerStore) CreateEvent(ctx context.Context, in EventTrigger) (EventTrigger, error) {
	var created EventTrigger
	err := s.db.QueryRowxContext(ctx,
		`INSERT INTO event_triggers (organization_id, name, enabled, event_type, source_ujt_id, workflow_template_id, unified_job_template_id)
		 VALUES ($1,$2,true,$3,$4,$5,$6)
		 RETURNING `+eventTriggerCols,
		in.OrganizationID, in.Name, in.EventType, in.SourceUJTID, in.WorkflowTemplateID, in.UnifiedJobTemplateID,
	).StructScan(&created)
	return created, wrap("TriggerStore.CreateEvent", err)
}

// UpdateEvent edits an event trigger (enabled taken verbatim) and returns it.
func (s *TriggerStore) UpdateEvent(ctx context.Context, id int64, in EventTrigger) (EventTrigger, error) {
	var updated EventTrigger
	err := s.db.QueryRowxContext(ctx,
		`UPDATE event_triggers SET name=$2, enabled=$3, event_type=$4, source_ujt_id=$5, workflow_template_id=$6, unified_job_template_id=$7
		 WHERE id=$1
		 RETURNING `+eventTriggerCols,
		id, in.Name, in.Enabled, in.EventType, in.SourceUJTID, in.WorkflowTemplateID, in.UnifiedJobTemplateID,
	).StructScan(&updated)
	return updated, wrap("TriggerStore.UpdateEvent", err)
}

// DeleteEvent removes an event trigger.
func (s *TriggerStore) DeleteEvent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM event_triggers WHERE id=$1`, id)
	return wrap("TriggerStore.DeleteEvent", err)
}

// scopedWebhookRows runs a webhook-source base query, org-filtered when !all.
func (s *TriggerStore) scopedWebhookRows(ctx context.Context, base string, all bool, orgIDs []int64) ([]WebhookSourceRow, error) {
	rows := []WebhookSourceRow{}
	if all {
		err := s.db.SelectContext(ctx, &rows, base+" ORDER BY name")
		return rows, wrap("TriggerStore.scopedWebhookRows", err)
	}
	q, args, err := sqlx.In(base+" AND organization_id IN (?) ORDER BY name", orgIDs)
	if err != nil {
		return nil, wrap("TriggerStore.scopedWebhookRows", err)
	}
	q = s.db.Rebind(q)
	err = s.db.SelectContext(ctx, &rows, q, args...)
	return rows, wrap("TriggerStore.scopedWebhookRows", err)
}

// WebhookWorkflows lists webhook-enabled workflow templates (org-scoped when !all).
func (s *TriggerStore) WebhookWorkflows(ctx context.Context, all bool, orgIDs []int64) ([]WebhookSourceRow, error) {
	return s.scopedWebhookRows(ctx, `SELECT id, name, COALESCE(webhook_service,'generic') AS service FROM workflow_templates WHERE webhook_enabled`, all, orgIDs)
}

// WebhookJobTemplates lists webhook-enabled job templates (org-scoped when !all).
func (s *TriggerStore) WebhookJobTemplates(ctx context.Context, all bool, orgIDs []int64) ([]WebhookSourceRow, error) {
	return s.scopedWebhookRows(ctx, `SELECT id, name, COALESCE(webhook_service,'generic') AS service FROM job_templates WHERE webhook_enabled`, all, orgIDs)
}

// WebhookPacks lists execution packs with an inbound webhook key (superuser-only).
func (s *TriggerStore) WebhookPacks(ctx context.Context) ([]WebhookSourceRow, error) {
	rows := []WebhookSourceRow{}
	err := s.db.SelectContext(ctx, &rows,
		`SELECT id, name, 'generic' AS service FROM execution_packs WHERE webhook_key IS NOT NULL AND webhook_key <> '' ORDER BY name`)
	return rows, wrap("TriggerStore.WebhookPacks", err)
}
