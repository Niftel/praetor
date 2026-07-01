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

// ExecutionRequest is the message published effectively to "job-execution-requests"
// topic. It contains everything an executor needs to start running a job.
type ExecutionRequest struct {
	ExecutionRunID uuid.UUID   `json:"execution_run_id"`
	UnifiedJobID   int64       `json:"unified_job_id"`
	JobManifest    JobManifest `json:"job_manifest"`
	CreatedAt      time.Time   `json:"created_at"`
}

// JobManifest contains all resolved configuration for the job execution.
type JobManifest struct {
	// For now, minimal fields as per vision doc example
	Inventory       string                 `json:"inventory"`        // Raw inventory INI content
	ProjectURL      string                 `json:"project_url"`      // Git URL for project
	ProjectRef      string                 `json:"project_ref"`      // Git branch/tag/commit (optional)
	Playbook        string                 `json:"playbook"`         // Playbook file path within project
	PlaybookContent string                 `json:"playbook_content"` // Inline playbook content (optional)
	ExtraVars       map[string]interface{} `json:"extra_vars"`
	Limit           string                 `json:"limit,omitempty"` // Ansible --limit host pattern
	UseFactCache    bool                   `json:"use_fact_cache,omitempty"`
	CachedFacts     map[string]json.RawMessage `json:"cached_facts,omitempty"` // hostname -> ansible_facts to preload

	// Inventory sync (Phase 3a): when InventorySync is set, the executor runs
	// `ansible-inventory --list` against InventorySource and upserts the result
	// into inventory SyncInventoryID, instead of running a playbook.
	InventorySync     bool   `json:"inventory_sync,omitempty"`
	InventorySource   string `json:"inventory_source,omitempty"`
	InventorySourceKind string `json:"inventory_source_kind,omitempty"`
	SyncInventoryID   int64  `json:"sync_inventory_id,omitempty"`

	RunnerHost      string                 `json:"runner_host,omitempty"`
	RunnerHostID    int64                  `json:"runner_host_id,omitempty"` // Host ID for heartbeat calls
	APIURL          string                 `json:"api_url,omitempty"`        // API URL for heartbeat calls

	// ExecutionPack names the self-contained Python+Ansible runtime to push and
	// run in (the executor pushes /tmp/build/runtime/<pack>-linux-<arch>.tar.gz,
	// the host-runner uses /opt/praetor/packs/<pack>). Empty = the default pack.
	ExecutionPack string `json:"execution_pack,omitempty"`

	// Credentials
	SSHUser       string `json:"ssh_user,omitempty"`
	SSHPrivateKey string `json:"ssh_private_key,omitempty"`

	// CredentialEnv / CredentialFiles are resolved from a credential's
	// AWX-style injectors by the scheduler. CredentialEnv maps an environment
	// variable name to its (already-decrypted) value. CredentialFiles maps an
	// environment variable name to file content; the executor writes the content
	// to a temp file and points the env var at that path. Used to authenticate
	// cloud dynamic-inventory plugins (e.g. AWS_ACCESS_KEY_ID for aws_ec2).
	CredentialEnv   map[string]string `json:"credential_env,omitempty"`
	CredentialFiles map[string]string `json:"credential_files,omitempty"`

	// GalaxyServers are the Ansible Galaxy / Automation Hub servers to install
	// project requirements from. Empty means the public galaxy.ansible.com.
	GalaxyServers []GalaxyServer `json:"galaxy_servers,omitempty"`
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
