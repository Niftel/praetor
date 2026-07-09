package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	JobRequestSubject = "job.requests"
	JobEventSubject   = "job.events"
)

// Job lifecycle event types. The first three drive the run-state projection in
// the consumer (db_writer.updateRunState); the rest are engine-narration events
// that surface Praetor's agentless-bootstrap and checkpoint/resume behaviour on
// the run timeline. Narration events are deliberately inert for state and
// notifications (the consumer's switch has a no-op default) — they exist purely
// to make the execution engine visible where users watch a run.
const (
	EventJobStarted   = "JOB_STARTED"
	EventJobCompleted = "JOB_COMPLETED"
	EventJobFailed    = "JOB_FAILED"
	EventJobCanceled  = "JOB_CANCELED"

	// EventRunnerOnline is emitted the moment the host-runner starts on a target.
	// Its existence is proof the agentless SSH bootstrap succeeded: the binary was
	// pushed over SSH and is now live with no pre-installed agent.
	EventRunnerOnline = "RUNNER_ONLINE"
	// EventCheckpointSaved marks a task boundary durably checkpointed to disk, so
	// an interruption resumes here instead of re-running from the top.
	EventCheckpointSaved = "CHECKPOINT_SAVED"
	// EventResumedFromCheckpoint is emitted when a runner picks up an interrupted
	// play (e.g. after a host reboot), skipping already-completed tasks.
	EventResumedFromCheckpoint = "RESUMED_FROM_CHECKPOINT"
)

// CurrentManifestVersion stamps every ExecutionRequest the scheduler emits.
//
// The manifest is a THREE-party wire contract: the scheduler writes it, the
// executor mutates it in flight, and the host-runner reads it on target hosts
// while riding its own release train — so a host-runner in the field can be older
// than the scheduler that produced the manifest. The compatibility discipline is
// therefore ADDITIVE-ONLY:
//
//   - Only ADD fields; never rename, retype, or repurpose an existing one.
//     (Removing/renaming a field silently changes meaning for an old reader.)
//   - New fields must be optional — an old host-runner ignores unknown JSON keys,
//     and a new host-runner must tolerate a zero/absent value from an old scheduler.
//   - Bump CurrentManifestVersion when you add fields, so the version is a visible
//     record of the contract level; readers may log/branch on it but must not hard-
//     fail on a newer value.
//
// The wire shape is frozen by the golden test in schemas_golden_test.go, so any
// change to it is a deliberate, reviewable diff.
const CurrentManifestVersion = 1

// ExecutionRequest is the message published effectively to "job-execution-requests"
// topic. It contains everything an executor needs to start running a job.
type ExecutionRequest struct {
	// ManifestVersion is the contract level this request was produced at
	// (CurrentManifestVersion). See the additive-only rule above.
	ManifestVersion int         `json:"manifest_version"`
	ExecutionRunID  uuid.UUID   `json:"execution_run_id"`
	UnifiedJobID    int64       `json:"unified_job_id"`
	JobManifest     JobManifest `json:"job_manifest"`
	CreatedAt       time.Time   `json:"created_at"`
}

// JobManifest contains all resolved configuration for the job execution.
type JobManifest struct {
	// For now, minimal fields as per vision doc example
	// InventoryID references the inventory to run against. The scheduler ships only
	// this id (by reference) in the outbox/NATS message; the executor fetches the
	// rendered INI from ingestion at dispatch and fills Inventory below, so a large
	// inventory doesn't bloat the message toward NATS's size limit (#13). 0 = none
	// (localhost).
	InventoryID     int64                  `json:"inventory_id,omitempty"`
	Inventory       string                 `json:"inventory"`        // Rendered INI; empty in the outbox/NATS, filled by the executor
	ProjectURL      string                 `json:"project_url"`      // Git URL for project
	ProjectRef      string                 `json:"project_ref"`      // Git branch/tag/commit (optional)
	Playbook        string                 `json:"playbook"`         // Playbook file path within project
	PlaybookContent string                 `json:"playbook_content"` // Inline playbook content (optional)
	ExtraVars       map[string]interface{} `json:"extra_vars"`
	Limit           string                 `json:"limit,omitempty"` // Ansible --limit host pattern
	// Verbosity is the ansible-playbook -v level (0–4) and Forks caps parallelism
	// (ansible-playbook --forks N). Both come from the job template; the host-runner
	// applies them when it builds the play command. They were previously stored on
	// the template but dropped in the manifest, so the UI settings did nothing (#78).
	Verbosity    int                        `json:"verbosity,omitempty"`
	Forks        int                        `json:"forks,omitempty"`
	UseFactCache bool                       `json:"use_fact_cache,omitempty"`
	CachedFacts  map[string]json.RawMessage `json:"cached_facts,omitempty"` // hostname -> ansible_facts to preload

	// Inventory sync (Phase 3a): when InventorySync is set, the executor runs
	// `ansible-inventory --list` against InventorySource and upserts the result
	// into inventory SyncInventoryID, instead of running a playbook.
	InventorySync       bool   `json:"inventory_sync,omitempty"`
	InventorySource     string `json:"inventory_source,omitempty"`
	InventorySourceKind string `json:"inventory_source_kind,omitempty"`
	SyncInventoryID     int64  `json:"sync_inventory_id,omitempty"`

	RunnerHost   string `json:"runner_host,omitempty"`
	RunnerHostID int64  `json:"runner_host_id,omitempty"` // Host ID for heartbeat calls
	APIURL       string `json:"api_url,omitempty"`        // API URL for heartbeat calls

	// ExecutionPack names the self-contained Python+Ansible runtime to push and
	// run in (the executor pushes /tmp/build/runtime/<pack>-linux-<arch>.tar.gz,
	// the host-runner uses /opt/praetor/packs/<pack>). Empty = the default pack.
	ExecutionPack string `json:"execution_pack,omitempty"`

	// Credentials
	SSHUser       string `json:"ssh_user,omitempty"`
	SSHPrivateKey string `json:"ssh_private_key,omitempty"`

	// CredentialID is the Machine credential the scheduler selected for this run.
	// The scheduler writes ONLY this reference into the manifest (and snapshots it
	// onto execution_runs) — never the decrypted secret. The executor resolves the
	// injectors at dispatch from ingestion's authenticated run-scoped endpoint and
	// fills CredentialEnv/CredentialFiles in its in-memory copy before pushing to
	// the host-runner, so no plaintext key is persisted in the outbox or NATS.
	CredentialID int64 `json:"credential_id,omitempty"`

	// CredentialEnv / CredentialFiles are the resolved AWX-style injectors.
	// CredentialEnv maps an env var name to its (decrypted) value; CredentialFiles
	// maps an env var name to file content the executor writes to a temp file and
	// points the var at (e.g. ANSIBLE_PRIVATE_KEY_FILE, AWS_ACCESS_KEY_ID). These
	// are populated by the executor after resolving CredentialID — they are NOT set
	// by the scheduler and are absent from the persisted/wire manifest.
	CredentialEnv   map[string]string `json:"credential_env,omitempty"`
	CredentialFiles map[string]string `json:"credential_files,omitempty"`

	// GalaxyServers are the Ansible Galaxy / Automation Hub servers to install
	// project requirements from. Empty means the public galaxy.ansible.com.
	GalaxyServers []GalaxyServer `json:"galaxy_servers,omitempty"`

	// IngestToken is the per-run bearer token the host-runner presents on its
	// ingestion calls (events/logs/heartbeat/facts). It is minted at dispatch by
	// the executor from the shared internal secret + run id (see pkg/runtoken) and
	// verified by ingestion in constant time. It lives only in the 0600 manifest on
	// the target — never in argv or the 0644 runner-meta — and is bound to the run
	// id, so it cannot be replayed against another run.
	IngestToken string `json:"ingest_token,omitempty"`
}

// GalaxyServer is a configured Ansible Galaxy / Automation Hub endpoint used to
// resolve a project's role/collection requirements.
type GalaxyServer struct {
	Name    string `json:"name"`               // server id, e.g. "automation_hub"
	URL     string `json:"url"`                // API URL
	Token   string `json:"token,omitempty"`    // API token (secret)
	AuthURL string `json:"auth_url,omitempty"` // SSO token-exchange URL (Automation Hub)
}

// JobEvent represents a single event emitted by the executor during execution.
// It corresponds to the 'job_event' table and 'job-events' topic.
type JobEvent struct {
	ExecutionRunID uuid.UUID `json:"execution_run_id"`
	UnifiedJobID   int64     `json:"unified_job_id"`
	Seq            int64     `json:"seq"`
	EventType      string    `json:"event_type"` // e.g. "JOB_STARTED", "TASK_OK"
	Timestamp      time.Time `json:"timestamp"`

	// Optional fields depending on event type
	Host          *string         `json:"host,omitempty"`
	TaskName      *string         `json:"task_name,omitempty"`
	PlayName      *string         `json:"play_name,omitempty"`
	StdoutSnippet *string         `json:"stdout_snippet,omitempty"`
	EventData     json.RawMessage `json:"event_data,omitempty"`
}

// LogChunk represents a chunk of log output uploaded to object storage.
// It corresponds to the 'job_output_chunk' table.
type LogChunk struct {
	ExecutionRunID uuid.UUID `json:"execution_run_id"`
	UnifiedJobID   int64     `json:"unified_job_id"` // Optional but helpful for routing
	Seq            int64     `json:"seq"`
	StorageKey     string    `json:"storage_key"`
	ByteLength     int       `json:"byte_length"`
	Timestamp      time.Time `json:"timestamp"`
}
