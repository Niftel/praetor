package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/praetordev/models"
	"github.com/praetordev/store"
)

type DiagnosticSummary struct {
	UnifiedJobID     int64      `json:"unified_job_id"`
	State            string     `json:"state"`
	CurrentPhase     string     `json:"current_phase"`
	Attempt          int        `json:"attempt"`
	SafeFailureCode  string     `json:"failure_code,omitempty"`
	LastEventSeq     int64      `json:"last_event_seq"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	SourceJobID      *int64     `json:"source_job_id,omitempty"`
	SubsequentJobIDs []int64    `json:"subsequent_job_ids"`
}

type DiagnosticEvent struct {
	Seq         int64     `json:"seq"`
	EventType   string    `json:"event_type"`
	HostID      *int64    `json:"host_id,omitempty"`
	TaskName    *string   `json:"task_name,omitempty"`
	PlayName    *string   `json:"play_name,omitempty"`
	Outcome     *string   `json:"outcome,omitempty"`
	Changed     bool      `json:"changed,omitempty"`
	DurationMS  *int64    `json:"duration_ms,omitempty"`
	FailureCode *string   `json:"failure_code,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type RunDiagnostics struct {
	Summary    DiagnosticSummary `json:"summary"`
	Events     []DiagnosticEvent `json:"events"`
	NextCursor *int64            `json:"next_cursor,omitempty"`
}

func FromDiagnosticEvent(event store.DiagnosticEvent) DiagnosticEvent {
	return DiagnosticEvent{
		Seq: event.Seq, EventType: event.EventType, HostID: event.HostID,
		TaskName: event.TaskName, PlayName: event.PlayName, Outcome: event.Outcome,
		Changed: event.Changed, DurationMS: event.DurationMS,
		FailureCode: event.FailureCode, CreatedAt: event.CreatedAt,
	}
}

func FromRunDiagnostics(summary store.DiagnosticSummary, events []store.DiagnosticEvent, nextCursor *int64) RunDiagnostics {
	result := RunDiagnostics{Summary: DiagnosticSummary{
		UnifiedJobID: summary.UnifiedJobID, State: summary.RunState,
		CurrentPhase: summary.CurrentPhase, Attempt: summary.Attempt,
		SafeFailureCode: summary.SafeFailureCode, LastEventSeq: summary.LastEventSeq,
		StartedAt: summary.StartedAt, FinishedAt: summary.FinishedAt,
		SourceJobID: summary.SourceJobID, SubsequentJobIDs: summary.SubsequentJobIDs,
	}, Events: make([]DiagnosticEvent, 0, len(events)), NextCursor: nextCursor}
	for _, event := range events {
		result.Events = append(result.Events, FromDiagnosticEvent(event))
	}
	return result
}

// UnifiedJob is the wire shape of a job.
type UnifiedJob struct {
	ID                   int64           `json:"id"`
	UnifiedJobTemplateID *int64          `json:"unified_job_template_id,omitempty"`
	Name                 string          `json:"name"`
	Status               string          `json:"status"`
	CurrentRunID         *uuid.UUID      `json:"current_run_id,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	StartedAt            *time.Time      `json:"started_at,omitempty"`
	FinishedAt           *time.Time      `json:"finished_at,omitempty"`
	CancelRequested      bool            `json:"cancel_requested"`
	JobArgs              json.RawMessage `json:"job_args,omitempty"`
}

func FromUnifiedJob(m models.UnifiedJob) UnifiedJob {
	return UnifiedJob{
		ID:                   m.ID,
		UnifiedJobTemplateID: m.UnifiedJobTemplateID,
		Name:                 m.Name,
		Status:               m.Status,
		CurrentRunID:         m.CurrentRunID,
		CreatedAt:            m.CreatedAt,
		StartedAt:            m.StartedAt,
		FinishedAt:           m.FinishedAt,
		CancelRequested:      m.CancelRequested,
		JobArgs:              m.JobArgs,
	}
}

func FromUnifiedJobs(ms []models.UnifiedJob) []UnifiedJob {
	out := make([]UnifiedJob, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromUnifiedJob(m))
	}
	return out
}

// ExecutionRun is the wire shape of an execution run.
type ExecutionRun struct {
	ID                uuid.UUID  `json:"id"`
	UnifiedJobID      int64      `json:"unified_job_id"`
	CreatedAt         time.Time  `json:"created_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	State             string     `json:"state"`
	LastHeartbeatAt   *time.Time `json:"last_heartbeat_at,omitempty"`
	LastEventSeq      int64      `json:"last_event_seq"`
	PersistedEventSeq int64      `json:"persisted_event_seq"`
}

func FromExecutionRun(m models.ExecutionRun) ExecutionRun {
	return ExecutionRun{
		ID:                m.ID,
		UnifiedJobID:      m.UnifiedJobID,
		CreatedAt:         m.CreatedAt,
		StartedAt:         m.StartedAt,
		FinishedAt:        m.FinishedAt,
		State:             m.State,
		LastHeartbeatAt:   m.LastHeartbeatAt,
		LastEventSeq:      m.LastEventSeq,
		PersistedEventSeq: m.PersistedEventSeq,
	}
}

func FromExecutionRuns(ms []models.ExecutionRun) []ExecutionRun {
	out := make([]ExecutionRun, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromExecutionRun(m))
	}
	return out
}

// JobEvent is the wire shape of a job event.
type JobEvent struct {
	ID             int64           `json:"id"`
	UnifiedJobID   int64           `json:"unified_job_id"`
	ExecutionRunID uuid.UUID       `json:"execution_run_id"`
	Seq            int64           `json:"seq"`
	EventType      string          `json:"event_type"`
	HostID         *int64          `json:"host_id,omitempty"`
	TaskName       *string         `json:"task_name,omitempty"`
	PlayName       *string         `json:"play_name,omitempty"`
	EventData      json.RawMessage `json:"event_data"`
	StdoutSnippet  *string         `json:"stdout_snippet,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

func FromJobEvent(m models.JobEvent) JobEvent {
	return JobEvent{
		ID:             m.ID,
		UnifiedJobID:   m.UnifiedJobID,
		ExecutionRunID: m.ExecutionRunID,
		Seq:            m.Seq,
		EventType:      m.EventType,
		HostID:         m.HostID,
		TaskName:       m.TaskName,
		PlayName:       m.PlayName,
		EventData:      m.EventData,
		StdoutSnippet:  m.StdoutSnippet,
		CreatedAt:      m.CreatedAt,
	}
}

func FromJobEvents(ms []models.JobEvent) []JobEvent {
	out := make([]JobEvent, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromJobEvent(m))
	}
	return out
}

func (d JobEvent) ToModel() models.JobEvent {
	return models.JobEvent{
		ID:             d.ID,
		UnifiedJobID:   d.UnifiedJobID,
		ExecutionRunID: d.ExecutionRunID,
		Seq:            d.Seq,
		EventType:      d.EventType,
		HostID:         d.HostID,
		TaskName:       d.TaskName,
		PlayName:       d.PlayName,
		EventData:      d.EventData,
		StdoutSnippet:  d.StdoutSnippet,
		CreatedAt:      d.CreatedAt,
	}
}
