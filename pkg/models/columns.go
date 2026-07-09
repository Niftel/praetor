package models

// Explicit SELECT column lists for the model structs that are read outside the
// API's store layer — specifically on the dispatch path (the scheduler's schedule
// tick and pkg/inventoryrender's host/group render, which ingestion runs at every
// job dispatch). They live here, in the lowest-level package, so both the store
// and those dispatch-path readers share one source of truth instead of an
// unguarded `SELECT *` that breaks ("missing destination name X") the moment the
// table gains a column.
//
// services/api/store re-exports these (HostCols = models.HostCols, ...) so its
// column-drift reflection test (store/columns_test.go) transitively guards them
// against their structs — keep that test's rows in place. See #91 / #39.
const (
	HostCols     = `id, inventory_id, name, description, variables, enabled, is_control_node, is_runner_host, runner_last_seen, runner_healthy, created_at, modified_at`
	GroupCols    = `id, inventory_id, name, description, variables, created_at, modified_at`
	ScheduleCols = `id, name, description, unified_job_template_id, workflow_template_id, rrule, next_run, enabled, extra_vars, created_at, modified_at`
)
