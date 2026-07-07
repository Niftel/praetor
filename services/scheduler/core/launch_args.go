package core

import (
	"encoding/json"
)

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
