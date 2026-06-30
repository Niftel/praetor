package core

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"
)

// loadHostFacts returns the stored ansible_facts for every host in an inventory
// that has them, keyed by host name — what the host-runner preloads into the
// Ansible fact cache. Returns nil when there are none.
func loadHostFacts(ctx context.Context, tx *sqlx.Tx, inventoryID int64) map[string]json.RawMessage {
	rows, err := tx.QueryxContext(ctx, `
		SELECT h.name, hf.facts
		FROM host_facts hf JOIN hosts h ON h.id = hf.host_id
		WHERE h.inventory_id = $1`, inventoryID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := map[string]json.RawMessage{}
	for rows.Next() {
		var name string
		var facts []byte
		if rows.Scan(&name, &facts) == nil {
			out[name] = json.RawMessage(facts)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// launchArgs is the prompt-on-launch override payload stored in
// unified_jobs.job_args when a job is launched. Fields are pointers/maps so an
// absent override is distinguishable from an empty one.
type launchArgs struct {
	ExtraVars map[string]interface{} `json:"extra_vars,omitempty"`
	Limit     *string                `json:"limit,omitempty"`
}

func parseLaunchArgs(raw json.RawMessage) launchArgs {
	var a launchArgs
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &a)
	}
	return a
}

// inventorySourceID returns the inventory_source_id a sync job references, or 0
// if this isn't a sync job.
func inventorySourceID(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var a struct {
		InventorySourceID int64 `json:"inventory_source_id"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.InventorySourceID
}

// mergeExtraVars overlays launch-supplied extra_vars on top of the template's
// default extra_vars (launch wins on key conflicts).
func mergeExtraVars(templateVars json.RawMessage, jobArgs json.RawMessage) map[string]interface{} {
	out := map[string]interface{}{}
	if len(templateVars) > 0 {
		_ = json.Unmarshal(templateVars, &out)
	}
	for k, v := range parseLaunchArgs(jobArgs).ExtraVars {
		out[k] = v
	}
	return out
}

// effectiveLimit returns the launch-supplied --limit if one was provided,
// otherwise the template's default.
func effectiveLimit(templateLimit string, jobArgs json.RawMessage) string {
	if l := parseLaunchArgs(jobArgs).Limit; l != nil {
		return *l
	}
	return templateLimit
}
