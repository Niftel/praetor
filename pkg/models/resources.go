package models

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID             int64     `json:"id" db:"id"`
	OrganizationID int64     `json:"organization_id" db:"organization_id"`
	Name           string    `json:"name" db:"name"`
	Description    *string   `json:"description,omitempty" db:"description"`
	SCMType        string    `json:"scm_type" db:"scm_type"`
	SCMURL         string    `json:"scm_url" db:"scm_url"`
	SCMBranch      *string   `json:"scm_branch,omitempty" db:"scm_branch"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	ModifiedAt     time.Time `json:"modified_at" db:"modified_at"`
}

type Inventory struct {
	ID             int64     `json:"id" db:"id"`
	OrganizationID int64     `json:"organization_id" db:"organization_id"`
	Name           string    `json:"name" db:"name"`
	Description    *string   `json:"description,omitempty" db:"description"`
	Kind           string    `json:"kind" db:"kind"`
	Content        *string   `json:"content,omitempty" db:"content"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	ModifiedAt     time.Time `json:"modified_at" db:"modified_at"`
}

type Host struct {
	ID             int64           `json:"id" db:"id"`
	InventoryID    int64           `json:"inventory_id" db:"inventory_id"`
	Name           string          `json:"name" db:"name"`
	Description    *string         `json:"description,omitempty" db:"description"`
	Variables      json.RawMessage `json:"variables,omitempty" db:"variables"`
	Enabled        bool            `json:"enabled" db:"enabled"`
	IsControlNode  bool            `json:"is_control_node" db:"is_control_node"`
	IsRunnerHost   bool            `json:"is_runner_host" db:"is_runner_host"`
	RunnerLastSeen *time.Time      `json:"runner_last_seen,omitempty" db:"runner_last_seen"`
	RunnerHealthy  bool            `json:"runner_healthy" db:"runner_healthy"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	ModifiedAt     time.Time       `json:"modified_at" db:"modified_at"`
}

type Group struct {
	ID          int64           `json:"id" db:"id"`
	InventoryID int64           `json:"inventory_id" db:"inventory_id"`
	Name        string          `json:"name" db:"name"`
	Description *string         `json:"description,omitempty" db:"description"`
	Variables   json.RawMessage `json:"variables,omitempty" db:"variables"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	ModifiedAt  time.Time       `json:"modified_at" db:"modified_at"`
}

type CredentialType struct {
	ID          int64           `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description *string         `json:"description,omitempty" db:"description"`
	Inputs      json.RawMessage `json:"inputs" db:"inputs"`
	Injectors   json.RawMessage `json:"injectors" db:"injectors"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	ModifiedAt  time.Time       `json:"modified_at" db:"modified_at"`
}

type Credential struct {
	ID               int64           `json:"id" db:"id"`
	OrganizationID   int64           `json:"organization_id" db:"organization_id"`
	CredentialTypeID int64           `json:"credential_type_id" db:"credential_type_id"`
	Name             string          `json:"name" db:"name"`
	Description      *string         `json:"description,omitempty" db:"description"`
	Inputs           json.RawMessage `json:"inputs" db:"inputs"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	ModifiedAt       time.Time       `json:"modified_at" db:"modified_at"`
}

type ExecutionEnvironment struct {
	ID             int64     `json:"id" db:"id"`
	OrganizationID *int64    `json:"organization_id,omitempty" db:"organization_id"`
	Name           string    `json:"name" db:"name"`
	Image          string    `json:"image" db:"image"`
	Description    *string   `json:"description,omitempty" db:"description"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	ModifiedAt     time.Time `json:"modified_at" db:"modified_at"`
}

type JobTemplate struct {
	ID                     int64           `json:"id" db:"id"`
	OrganizationID         int64           `json:"organization_id" db:"organization_id"`
	Name                   string          `json:"name" db:"name"`
	Description            *string         `json:"description,omitempty" db:"description"`
	InventoryID            *int64          `json:"inventory_id,omitempty" db:"inventory_id"`
	ProjectID              *int64          `json:"project_id,omitempty" db:"project_id"`
	Playbook               string          `json:"playbook" db:"playbook"`
	PlaybookContent        *string         `json:"playbook_content,omitempty" db:"playbook_content"`
	ExecutionEnvironmentID *int64          `json:"execution_environment_id,omitempty" db:"execution_environment_id"`
	UnifiedJobTemplateID   *int64          `json:"unified_job_template_id,omitempty" db:"unified_job_template_id"`
	CredentialID           *int64          `json:"credential_id,omitempty" db:"credential_id"`
	Forks                  int             `json:"forks" db:"forks"`
	JobType                string          `json:"job_type" db:"job_type"`
	Verbosity              int             `json:"verbosity" db:"verbosity"`
	ExtraVars              json.RawMessage `json:"extra_vars,omitempty" db:"extra_vars"`
	JobLimit               string          `json:"limit" db:"job_limit"`
	AskVariablesOnLaunch   bool            `json:"ask_variables_on_launch" db:"ask_variables_on_launch"`
	AskLimitOnLaunch       bool            `json:"ask_limit_on_launch" db:"ask_limit_on_launch"`
	SurveyEnabled          bool            `json:"survey_enabled" db:"survey_enabled"`
	SurveySpec             json.RawMessage `json:"survey_spec,omitempty" db:"survey_spec"`
	WebhookEnabled         bool            `json:"webhook_enabled" db:"webhook_enabled"`
	WebhookService         string          `json:"webhook_service" db:"webhook_service"`
	WebhookKey             string          `json:"webhook_key" db:"webhook_key"`
	CreatedAt              time.Time       `json:"created_at" db:"created_at"`
	ModifiedAt             time.Time       `json:"modified_at" db:"modified_at"`
}

type Schedule struct {
	ID                   int64           `json:"id" db:"id"`
	Name                 string          `json:"name" db:"name"`
	Description          *string         `json:"description,omitempty" db:"description"`
	UnifiedJobTemplateID int64           `json:"unified_job_template_id" db:"unified_job_template_id"`
	RRule                string          `json:"rrule" db:"rrule"`
	NextRun              time.Time       `json:"next_run" db:"next_run"`
	Enabled              bool            `json:"enabled" db:"enabled"`
	ExtraVars            json.RawMessage `json:"extra_vars,omitempty" db:"extra_vars"`
	CreatedAt            time.Time       `json:"created_at" db:"created_at"`
	ModifiedAt           time.Time       `json:"modified_at" db:"modified_at"`
}
