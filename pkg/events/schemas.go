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
	RunnerHost      string                 `json:"runner_host,omitempty"`
	RunnerHostID    int64                  `json:"runner_host_id,omitempty"` // Host ID for heartbeat calls
	APIURL          string                 `json:"api_url,omitempty"`        // API URL for heartbeat calls

	// Credentials
	SSHUser       string `json:"ssh_user,omitempty"`
	SSHPrivateKey string `json:"ssh_private_key,omitempty"`

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
