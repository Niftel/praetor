package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type UnifiedJob struct {
	ID                   int64           `json:"id" db:"id"`
	UnifiedJobTemplateID *int64          `json:"unified_job_template_id,omitempty" db:"unified_job_template_id"`
	Name                 string          `json:"name" db:"name"`
	Status               string          `json:"status" db:"status"`
	CurrentRunID         *uuid.UUID      `json:"current_run_id,omitempty" db:"current_run_id"`
	CreatedAt            time.Time       `json:"created_at" db:"created_at"`
	StartedAt            *time.Time      `json:"started_at,omitempty" db:"started_at"`
	FinishedAt           *time.Time      `json:"finished_at,omitempty" db:"finished_at"`
	CancelRequested      bool            `json:"cancel_requested" db:"cancel_requested"`
	JobArgs              json.RawMessage `json:"job_args,omitempty" db:"job_args"`
}

type ExecutionRun struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	UnifiedJobID       int64      `json:"unified_job_id" db:"unified_job_id"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	StartedAt          *time.Time `json:"started_at,omitempty" db:"started_at"`
	FinishedAt         *time.Time `json:"finished_at,omitempty" db:"finished_at"`
	State              string     `json:"state" db:"state"`
	LastHeartbeatAt    *time.Time `json:"last_heartbeat_at,omitempty" db:"last_heartbeat_at"`
	LastEventSeq       int64      `json:"last_event_seq" db:"last_event_seq"`
	PersistedEventSeq  int64      `json:"persisted_event_seq" db:"persisted_event_seq"`
}

type JobEvent struct {
	ID             int64           `json:"id" db:"id"`
	UnifiedJobID   int64           `json:"unified_job_id" db:"unified_job_id"`
	ExecutionRunID uuid.UUID       `json:"execution_run_id" db:"execution_run_id"`
	Seq            int64           `json:"seq" db:"seq"`
	EventType      string          `json:"event_type" db:"event_type"`
	HostID         *int64          `json:"host_id,omitempty" db:"host_id"`
	TaskName       *string         `json:"task_name,omitempty" db:"task_name"`
	PlayName       *string         `json:"play_name,omitempty" db:"play_name"`
	EventData      json.RawMessage `json:"event_data" db:"event_data"`
	StdoutSnippet  *string         `json:"stdout_snippet,omitempty" db:"stdout_snippet"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

type JobHostSummary struct {
	ID           int64      `json:"id" db:"id"`
	UnifiedJobID int64      `json:"unified_job_id" db:"unified_job_id"`
	HostID       int64      `json:"host_id" db:"host_id"`
	Changed      int        `json:"changed" db:"changed"`
	Failed       int        `json:"failed" db:"failed"`
	Ok           int        `json:"ok" db:"ok"`
	Skipped      int        `json:"skipped" db:"skipped"`
	Unreachable  int        `json:"unreachable" db:"unreachable"`
	LastEventAt  *time.Time `json:"last_event_at,omitempty" db:"last_event_at"`
}

type JobOutputChunk struct {
	ID             int64     `json:"id" db:"id"`
	ExecutionRunID uuid.UUID `json:"execution_run_id" db:"execution_run_id"`
	Seq            int64     `json:"seq" db:"seq"`
	StorageKey     string    `json:"storage_key" db:"storage_key"`
	ByteLength     int       `json:"byte_length" db:"byte_length"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}
