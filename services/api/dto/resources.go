package dto

import (
	"encoding/json"
	"time"

	"github.com/praetordev/models"
)

// Inventory is the wire shape of an inventory.
type Inventory struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	Kind           string    `json:"kind"`
	Content        *string   `json:"content,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ModifiedAt     time.Time `json:"modified_at"`
}

func FromInventory(m models.Inventory) Inventory {
	return Inventory{
		ID:             m.ID,
		OrganizationID: m.OrganizationID,
		Name:           m.Name,
		Description:    m.Description,
		Kind:           m.Kind,
		Content:        m.Content,
		CreatedAt:      m.CreatedAt,
		ModifiedAt:     m.ModifiedAt,
	}
}

func FromInventories(ms []models.Inventory) []Inventory {
	out := make([]Inventory, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromInventory(m))
	}
	return out
}

func (d Inventory) ToModel() models.Inventory {
	return models.Inventory{
		ID:             d.ID,
		OrganizationID: d.OrganizationID,
		Name:           d.Name,
		Description:    d.Description,
		Kind:           d.Kind,
		Content:        d.Content,
		CreatedAt:      d.CreatedAt,
		ModifiedAt:     d.ModifiedAt,
	}
}

// Host is the wire shape of a host.
type Host struct {
	ID             int64           `json:"id"`
	InventoryID    int64           `json:"inventory_id"`
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	Variables      json.RawMessage `json:"variables,omitempty"`
	Enabled        bool            `json:"enabled"`
	IsControlNode  bool            `json:"is_control_node"`
	IsRunnerHost   bool            `json:"is_runner_host"`
	RunnerLastSeen *time.Time      `json:"runner_last_seen,omitempty"`
	RunnerHealthy  bool            `json:"runner_healthy"`
	CreatedAt      time.Time       `json:"created_at"`
	ModifiedAt     time.Time       `json:"modified_at"`
}

func FromHost(m models.Host) Host {
	return Host{
		ID:             m.ID,
		InventoryID:    m.InventoryID,
		Name:           m.Name,
		Description:    m.Description,
		Variables:      m.Variables,
		Enabled:        m.Enabled,
		IsControlNode:  m.IsControlNode,
		IsRunnerHost:   m.IsRunnerHost,
		RunnerLastSeen: m.RunnerLastSeen,
		RunnerHealthy:  m.RunnerHealthy,
		CreatedAt:      m.CreatedAt,
		ModifiedAt:     m.ModifiedAt,
	}
}

func FromHosts(ms []models.Host) []Host {
	out := make([]Host, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromHost(m))
	}
	return out
}

func (d Host) ToModel() models.Host {
	return models.Host{
		ID:             d.ID,
		InventoryID:    d.InventoryID,
		Name:           d.Name,
		Description:    d.Description,
		Variables:      d.Variables,
		Enabled:        d.Enabled,
		IsControlNode:  d.IsControlNode,
		IsRunnerHost:   d.IsRunnerHost,
		RunnerLastSeen: d.RunnerLastSeen,
		RunnerHealthy:  d.RunnerHealthy,
		CreatedAt:      d.CreatedAt,
		ModifiedAt:     d.ModifiedAt,
	}
}

// Group is the wire shape of a group.
type Group struct {
	ID          int64           `json:"id"`
	InventoryID int64           `json:"inventory_id"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Variables   json.RawMessage `json:"variables,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	ModifiedAt  time.Time       `json:"modified_at"`
}

func FromGroup(m models.Group) Group {
	return Group{
		ID:          m.ID,
		InventoryID: m.InventoryID,
		Name:        m.Name,
		Description: m.Description,
		Variables:   m.Variables,
		CreatedAt:   m.CreatedAt,
		ModifiedAt:  m.ModifiedAt,
	}
}

func FromGroups(ms []models.Group) []Group {
	out := make([]Group, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromGroup(m))
	}
	return out
}

func (d Group) ToModel() models.Group {
	return models.Group{
		ID:          d.ID,
		InventoryID: d.InventoryID,
		Name:        d.Name,
		Description: d.Description,
		Variables:   d.Variables,
		CreatedAt:   d.CreatedAt,
		ModifiedAt:  d.ModifiedAt,
	}
}

// CredentialType is the wire shape of a credential type.
type CredentialType struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	Inputs      json.RawMessage `json:"inputs"`
	Injectors   json.RawMessage `json:"injectors"`
	Managed     bool            `json:"managed"`
	CreatedAt   time.Time       `json:"created_at"`
	ModifiedAt  time.Time       `json:"modified_at"`
}

func FromCredentialType(m models.CredentialType) CredentialType {
	return CredentialType{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Inputs:      m.Inputs,
		Injectors:   m.Injectors,
		Managed:     m.Managed,
		CreatedAt:   m.CreatedAt,
		ModifiedAt:  m.ModifiedAt,
	}
}

func FromCredentialTypes(ms []models.CredentialType) []CredentialType {
	out := make([]CredentialType, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromCredentialType(m))
	}
	return out
}

// Credential is the wire shape of a credential. Secret input values are masked by
// the handler before mapping; this DTO carries whatever Inputs it is given.
type Credential struct {
	ID               int64           `json:"id"`
	OrganizationID   int64           `json:"organization_id"`
	CredentialTypeID int64           `json:"credential_type_id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	Inputs           json.RawMessage `json:"inputs"`
	CreatedAt        time.Time       `json:"created_at"`
	ModifiedAt       time.Time       `json:"modified_at"`
}

func FromCredential(m models.Credential) Credential {
	return Credential{
		ID:               m.ID,
		OrganizationID:   m.OrganizationID,
		CredentialTypeID: m.CredentialTypeID,
		Name:             m.Name,
		Description:      m.Description,
		Inputs:           m.Inputs,
		CreatedAt:        m.CreatedAt,
		ModifiedAt:       m.ModifiedAt,
	}
}

func FromCredentials(ms []models.Credential) []Credential {
	out := make([]Credential, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromCredential(m))
	}
	return out
}

func (d Credential) ToModel() models.Credential {
	return models.Credential{
		ID:               d.ID,
		OrganizationID:   d.OrganizationID,
		CredentialTypeID: d.CredentialTypeID,
		Name:             d.Name,
		Description:      d.Description,
		Inputs:           d.Inputs,
		CreatedAt:        d.CreatedAt,
		ModifiedAt:       d.ModifiedAt,
	}
}

// JobTemplate is the wire shape of a job template.
type JobTemplate struct {
	ID                   int64           `json:"id"`
	OrganizationID       int64           `json:"organization_id"`
	Name                 string          `json:"name"`
	Description          *string         `json:"description,omitempty"`
	InventoryID          *int64          `json:"inventory_id,omitempty"`
	ProjectID            *int64          `json:"project_id,omitempty"`
	Playbook             string          `json:"playbook"`
	PlaybookContent      *string         `json:"playbook_content,omitempty"`
	UnifiedJobTemplateID *int64          `json:"unified_job_template_id,omitempty"`
	CredentialID         *int64          `json:"credential_id,omitempty"`
	ExecutionPackID      *int64          `json:"execution_pack_id,omitempty"`
	Forks                int             `json:"forks"`
	JobType              string          `json:"job_type"`
	Verbosity            int             `json:"verbosity"`
	ExtraVars            json.RawMessage `json:"extra_vars,omitempty"`
	JobLimit             string          `json:"limit"`
	AskVariablesOnLaunch bool            `json:"ask_variables_on_launch"`
	AskLimitOnLaunch     bool            `json:"ask_limit_on_launch"`
	SurveyEnabled        bool            `json:"survey_enabled"`
	SurveySpec           json.RawMessage `json:"survey_spec,omitempty"`
	WebhookEnabled       bool            `json:"webhook_enabled"`
	WebhookService       string          `json:"webhook_service"`
	WebhookKey           string          `json:"webhook_key"`
	UseFactCache         bool            `json:"use_fact_cache"`
	AllowSimultaneous    bool            `json:"allow_simultaneous"`
	CreatedAt            time.Time       `json:"created_at"`
	ModifiedAt           time.Time       `json:"modified_at"`
}

func FromJobTemplate(m models.JobTemplate) JobTemplate {
	return JobTemplate{
		ID:                   m.ID,
		OrganizationID:       m.OrganizationID,
		Name:                 m.Name,
		Description:          m.Description,
		InventoryID:          m.InventoryID,
		ProjectID:            m.ProjectID,
		Playbook:             m.Playbook,
		PlaybookContent:      m.PlaybookContent,
		UnifiedJobTemplateID: m.UnifiedJobTemplateID,
		CredentialID:         m.CredentialID,
		ExecutionPackID:      m.ExecutionPackID,
		Forks:                m.Forks,
		JobType:              m.JobType,
		Verbosity:            m.Verbosity,
		ExtraVars:            m.ExtraVars,
		JobLimit:             m.JobLimit,
		AskVariablesOnLaunch: m.AskVariablesOnLaunch,
		AskLimitOnLaunch:     m.AskLimitOnLaunch,
		SurveyEnabled:        m.SurveyEnabled,
		SurveySpec:           m.SurveySpec,
		WebhookEnabled:       m.WebhookEnabled,
		WebhookService:       m.WebhookService,
		WebhookKey:           m.WebhookKey,
		UseFactCache:         m.UseFactCache,
		AllowSimultaneous:    m.AllowSimultaneous,
		CreatedAt:            m.CreatedAt,
		ModifiedAt:           m.ModifiedAt,
	}
}

func FromJobTemplates(ms []models.JobTemplate) []JobTemplate {
	out := make([]JobTemplate, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromJobTemplate(m))
	}
	return out
}

func (d JobTemplate) ToModel() models.JobTemplate {
	return models.JobTemplate{
		ID:                   d.ID,
		OrganizationID:       d.OrganizationID,
		Name:                 d.Name,
		Description:          d.Description,
		InventoryID:          d.InventoryID,
		ProjectID:            d.ProjectID,
		Playbook:             d.Playbook,
		PlaybookContent:      d.PlaybookContent,
		UnifiedJobTemplateID: d.UnifiedJobTemplateID,
		CredentialID:         d.CredentialID,
		ExecutionPackID:      d.ExecutionPackID,
		Forks:                d.Forks,
		JobType:              d.JobType,
		Verbosity:            d.Verbosity,
		ExtraVars:            d.ExtraVars,
		JobLimit:             d.JobLimit,
		AskVariablesOnLaunch: d.AskVariablesOnLaunch,
		AskLimitOnLaunch:     d.AskLimitOnLaunch,
		SurveyEnabled:        d.SurveyEnabled,
		SurveySpec:           d.SurveySpec,
		WebhookEnabled:       d.WebhookEnabled,
		WebhookService:       d.WebhookService,
		WebhookKey:           d.WebhookKey,
		UseFactCache:         d.UseFactCache,
		AllowSimultaneous:    d.AllowSimultaneous,
		CreatedAt:            d.CreatedAt,
		ModifiedAt:           d.ModifiedAt,
	}
}

// Schedule is the wire shape of a schedule.
type Schedule struct {
	ID                   int64           `json:"id"`
	Name                 string          `json:"name"`
	Description          *string         `json:"description,omitempty"`
	UnifiedJobTemplateID *int64          `json:"unified_job_template_id,omitempty"`
	WorkflowTemplateID   *int64          `json:"workflow_template_id,omitempty"`
	InventorySourceID    *int64          `json:"inventory_source_id,omitempty"`
	RRule                string          `json:"rrule"`
	NextRun              time.Time       `json:"next_run"`
	Enabled              bool            `json:"enabled"`
	ExtraVars            json.RawMessage `json:"extra_vars,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	ModifiedAt           time.Time       `json:"modified_at"`
}

func FromSchedule(m models.Schedule) Schedule {
	return Schedule{
		ID:                   m.ID,
		Name:                 m.Name,
		Description:          m.Description,
		UnifiedJobTemplateID: m.UnifiedJobTemplateID,
		WorkflowTemplateID:   m.WorkflowTemplateID,
		InventorySourceID:    m.InventorySourceID,
		RRule:                m.RRule,
		NextRun:              m.NextRun,
		Enabled:              m.Enabled,
		ExtraVars:            m.ExtraVars,
		CreatedAt:            m.CreatedAt,
		ModifiedAt:           m.ModifiedAt,
	}
}

func FromSchedules(ms []models.Schedule) []Schedule {
	out := make([]Schedule, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromSchedule(m))
	}
	return out
}

func (d Schedule) ToModel() models.Schedule {
	return models.Schedule{
		ID:                   d.ID,
		Name:                 d.Name,
		Description:          d.Description,
		UnifiedJobTemplateID: d.UnifiedJobTemplateID,
		WorkflowTemplateID:   d.WorkflowTemplateID,
		InventorySourceID:    d.InventorySourceID,
		RRule:                d.RRule,
		NextRun:              d.NextRun,
		Enabled:              d.Enabled,
		ExtraVars:            d.ExtraVars,
		CreatedAt:            d.CreatedAt,
		ModifiedAt:           d.ModifiedAt,
	}
}
