// Package launch is the single place a job or workflow run is created.
//
// Before this package, "start a job" was reimplemented at six independent
// INSERT INTO unified_jobs sites across the api and scheduler services, each
// hand-marshalling a different subset of an untyped job_args JSON blob — which
// is how scheduled runs and workflow nodes silently dropped their overrides.
// Every launch surface (manual, inbound webhook, EDA event, inventory-source
// sync, schedule / event trigger, workflow node) now funnels through Job; every
// workflow-launch surface funnels through Workflow. The typed Options value
// replaces the ad-hoc job_args blob on the producing side, and ParseArgs reads
// it back on the consuming (scheduler) side, so the two ends can't drift.
//
// See docs/coupling-decomposition-plan.md (B2).
package launch

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Options carries prompt-on-launch overrides into a job. It is the typed
// replacement for the unified_jobs.job_args JSON blob. Pointers/maps keep
// "unset" distinct from "empty" so the scheduler can tell a real override from a
// default (e.g. an explicit empty --limit vs. no --limit at all).
type Options struct {
	// ExtraVars are launch-supplied variables overlaid on the template defaults.
	ExtraVars map[string]interface{}
	// Limit overrides the template's --limit host pattern when non-nil.
	Limit *string
	// InventorySourceID is set only for inventory-source sync jobs (which have no
	// job template); the scheduler runs `ansible-inventory --list` for it.
	InventorySourceID int64
}

// jobArgs is the on-disk shape persisted in unified_jobs.job_args. It is private:
// producers construct Options and call JobArgs; the scheduler calls ParseArgs.
// The JSON tags are the frozen wire shape (previously defined in the scheduler's
// launch_args.go) and must stay backward-compatible with rows already stored.
type jobArgs struct {
	ExtraVars         map[string]interface{} `json:"extra_vars,omitempty"`
	Limit             *string                `json:"limit,omitempty"`
	InventorySourceID int64                  `json:"inventory_source_id,omitempty"`
}

// JobArgs marshals the overrides into the job_args JSON stored on unified_jobs.
// Empty Options marshal to "{}".
func (o Options) JobArgs() json.RawMessage {
	b, err := json.Marshal(jobArgs{
		ExtraVars:         o.ExtraVars,
		Limit:             o.Limit,
		InventorySourceID: o.InventorySourceID,
	})
	if err != nil || len(b) == 0 {
		return json.RawMessage("{}")
	}
	return b
}

// ParseArgs decodes a unified_jobs.job_args blob back into Options. Used by the
// scheduler when it turns a pending job into an execution manifest.
func ParseArgs(raw json.RawMessage) Options {
	var a jobArgs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &a)
	}
	return Options{ExtraVars: a.ExtraVars, Limit: a.Limit, InventorySourceID: a.InventorySourceID}
}

// MergeExtraVars overlays the launch-supplied extra_vars on top of the
// template's default extra_vars (launch wins on key conflicts).
func (o Options) MergeExtraVars(templateVars json.RawMessage) map[string]interface{} {
	out := map[string]interface{}{}
	if len(templateVars) > 0 {
		_ = json.Unmarshal(templateVars, &out)
	}
	for k, v := range o.ExtraVars {
		out[k] = v
	}
	return out
}

// EffectiveLimit returns the launch-supplied --limit if one was provided,
// otherwise the template's default.
func (o Options) EffectiveLimit(templateLimit string) string {
	if o.Limit != nil {
		return *o.Limit
	}
	return templateLimit
}

// Execer is the minimal database seam Job and Workflow need. It is satisfied by
// *sqlx.DB, *sqlx.Tx, *sql.DB and *sql.Tx, so a caller can run a launch inside
// its own transaction (for atomicity) or standalone.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Job inserts a pending unified_job and returns its id. This is the single
// INSERT INTO unified_jobs site for the whole platform. ujtID is nil for
// inventory-source syncs (which have no job template); created_at is set by the
// database clock. The scheduler picks the row up (status='pending', no
// current_run_id), reads job_args via ParseArgs, and dispatches it.
func Job(ctx context.Context, ex Execer, name string, ujtID *int64, opts Options) (int64, error) {
	var id int64
	err := ex.QueryRowContext(ctx,
		`INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
		 VALUES ($1, $2, 'pending', now(), $3) RETURNING id`,
		name, ujtID, []byte(opts.JobArgs())).Scan(&id)
	return id, err
}

// Workflow snapshots a workflow template's node/edge graph into a new running
// workflow_jobs run and returns its id. The snapshot is taken at launch so later
// template edits don't mutate an in-flight run. This is the single
// workflow-launch site: manual launch, schedules, event triggers, inbound
// webhooks and EDA rules all call it. Callers that need the three inserts to be
// atomic pass a transaction as ex.
func Workflow(ctx context.Context, ex Execer, wfID int64) (int64, error) {
	var wjID int64
	if err := ex.QueryRowContext(ctx,
		`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1, 'running') RETURNING id`,
		wfID).Scan(&wjID); err != nil {
		return 0, err
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body, status)
		 SELECT $1, node_key, node_type, job_template_id, name, webhook_url, webhook_body, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
		wjID, wfID); err != nil {
		return 0, err
	}
	if _, err := ex.ExecContext(ctx,
		`INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
		 SELECT $1, parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$2`,
		wjID, wfID); err != nil {
		return 0, err
	}
	return wjID, nil
}
