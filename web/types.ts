// Types matching backend models exactly (snake_case field names)

// Job/Execution models
export interface UnifiedJob {
  id: number;
  unified_job_template_id?: number;
  name: string;
  status: string;
  current_run_id?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  cancel_requested: boolean;
  job_args?: any;
}

// Alias for backwards compatibility
export type Job = UnifiedJob;

// Resource models
export interface Project {
  id: number;
  organization_id: number;
  name: string;
  description?: string;
  scm_type: string;
  scm_url: string;
  scm_branch?: string;
  created_at: string;
  modified_at: string;
}

export interface Inventory {
  id: number;
  organization_id: number;
  name: string;
  description?: string;
  kind: string;
  content?: string;
  created_at: string;
  modified_at: string;
}

export interface Host {
  id: number;
  inventory_id: number;
  name: string;
  description?: string;
  variables?: any;
  enabled: boolean;
  is_control_node: boolean;
  is_runner_host?: boolean;
  runner_last_seen?: string;
  runner_healthy?: boolean;
  created_at: string;
  modified_at: string;
}

export interface Group {
  id: number;
  inventory_id: number;
  name: string;
  description?: string;
  variables?: any;
  created_at: string;
  modified_at: string;
}

export interface ServicePrincipal {
  id: number;
  organization_id: number;
  name: string;
  description: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  disabled_at?: string | null;
}

export interface ServiceCredential {
  id: number;
  service_principal_id: number;
  name: string;
  expires_at: string;
  last_used_at?: string | null;
  created_at: string;
  revoked_at?: string | null;
}

export interface DelegatedLaunchGrant {
  id: number;
  organization_id: number;
  service_principal_id: number;
  workflow_template_id: number;
  inventory_id: number;
  allowed_host_ids: number[];
  allowed_group_ids: number[];
  max_hosts?: number | null;
  allowed_extra_var_keys: string[];
  approval_team_id?: number | null;
  not_before: string;
  expires_at: string;
  created_at: string;
  updated_at: string;
  revoked_at?: string | null;
}

export interface CredentialType {
  id: number;
  name: string;
  description?: string;
  inputs: any;
  injectors?: any;
  created_at: string;
  modified_at: string;
}

export interface Credential {
  id: number;
  organization_id: number;
  credential_type_id: number;
  name: string;
  description?: string;
  inputs: any;
  created_at: string;
  modified_at: string;
}

export interface InventorySourceField {
  id: string;
  label: string;
  type: 'string' | 'string_list' | 'boolean' | 'yaml';
  required: boolean;
  description?: string;
  default?: unknown;
}

export interface InventorySourceType {
  id: string;
  version: string;
  name: string;
  description: string;
  plugin?: string;
  advanced: boolean;
  fields: InventorySourceField[];
  compatible_credential_types: string[];
  example: string;
}

export interface InventorySourceCatalog {
  version: string;
  results: InventorySourceType[];
}

export type InventoryReconciliationPolicy = 'disable_missing' | 'retain_missing';

export interface InventorySyncHistory {
  id: number;
  correlation_id: string;
  inventory_id?: number;
  inventory_source_id?: number;
  unified_job_id?: number;
  execution_run_id?: string;
  credential_id?: number;
  reconciliation_policy: InventoryReconciliationPolicy;
  phase: 'queued' | 'acquisition' | 'parsing' | 'validation' | 'reconciliation' | 'completed';
  status: 'pending' | 'running' | 'successful' | 'failed' | 'canceled';
  hosts_added: number;
  hosts_updated: number;
  hosts_disabled: number;
  hosts_unchanged: number;
  groups_added: number;
  groups_updated: number;
  groups_unchanged: number;
  diagnostic_code?: string;
  diagnostic_message?: string;
  diagnostic_details: Record<string, unknown>;
  started_at?: string;
  finished_at?: string;
  created_at: string;
}

export interface InventorySyncHistoryResponse {
  results: InventorySyncHistory[];
  total: number;
}

export interface JobTemplate {
  id: number;
  organization_id: number;
  name: string;
  description?: string;
  inventory_id?: number;
  project_id?: number;
  playbook: string;
  playbook_content?: string;
  unified_job_template_id?: number;
  credential_id?: number;
  execution_pack_id?: number;
  forks: number;
  job_type: string;
  verbosity: number;
  extra_vars?: any;
  limit?: string;
  ask_variables_on_launch?: boolean;
  ask_limit_on_launch?: boolean;
  survey_enabled?: boolean;
  survey_spec?: { name?: string; description?: string; spec?: SurveyQuestion[] };
  webhook_enabled?: boolean;
  webhook_service?: string;
  webhook_key?: string;
  use_fact_cache?: boolean;
  allow_simultaneous?: boolean;
  created_at: string;
  modified_at: string;
}

export interface SurveyQuestion {
  variable: string;
  question_name: string;
  type: 'text' | 'textarea' | 'password' | 'integer' | 'multiplechoice';
  required: boolean;
  default?: string;
  choices?: string; // newline-separated, for multiplechoice
}

// Alias for backwards compatibility
export type Template = JobTemplate;

export interface Schedule {
  id: number;
  name: string;
  description?: string;
  unified_job_template_id?: number | null;
  workflow_template_id?: number | null;
  inventory_source_id?: number | null;
  rrule: string;
  next_run: string;
  enabled: boolean;
  extra_vars?: any;
  created_at: string;
  modified_at: string;
}

export interface EventTrigger {
  id: number;
  organization_id: number;
  name: string;
  enabled: boolean;
  event_type: 'job_succeeded' | 'job_failed' | 'job_finished';
  source_ujt_id?: number | null;
  workflow_template_id?: number | null;
  unified_job_template_id?: number | null;
  created_at: string;
}

export interface WebhookTrigger {
  kind: 'workflow' | 'job_template' | 'execution_pack';
  id: number;
  name: string;
  service: string;
  url: string;
}

// RBAC models
export interface User {
  id: number;
  username: string;
  email: string;
  first_name?: string;
  last_name?: string;
  is_superuser: boolean;
  is_system_auditor: boolean;
  is_active: boolean;
  created_at: string;
  modified_at?: string;
}

export interface Team {
  id: number;
  organization_id: number;
  name: string;
  description?: string;
  created_at: string;
  modified_at?: string;
}

// AWX-style Role with polymorphic ownership
export interface Role {
  id: number;
  role_field: string;           // e.g., 'admin_role', 'member_role', 'read_role'
  singleton_name?: string;      // For system roles: 'system_administrator', 'system_auditor'
  content_type?: string;        // 'organization', 'team', 'project', etc.
  object_id?: number;           // ID of the owning object
  name?: string;                // Human-readable name
  description?: string;
  created_at?: string;
  modified_at?: string;
  // Legacy field kept for backwards compat
  permissions?: any;
}

export interface Organization {
  id: number;
  name: string;
  description?: string;
  created_at: string;
  modified_at?: string;
}

// Workflows
export type WorkflowNodeType = 'job' | 'approval' | 'webhook_in' | 'webhook_out';
export type WorkflowEdgeType = 'success' | 'failure' | 'always';

export interface WorkflowNode {
  node_key: string;
  node_type: WorkflowNodeType;
  job_template_id?: number | null;
  name: string;
  webhook_url?: string;   // webhook_out: URL to POST
  webhook_body?: string;  // webhook_out: optional JSON body
  approval_timeout_seconds?: number;
  approval_timeout_action?: 'approved' | 'rejected';
}

export interface WorkflowEdge {
  parent_key: string;
  child_key: string;
  edge_type: WorkflowEdgeType;
}

export interface Workflow {
  id: number;
  organization_id: number;
  name: string;
  webhook_enabled?: boolean;
  webhook_service?: string;
  nodes?: WorkflowNode[];
  edges?: WorkflowEdge[];
}

export interface WorkflowJobNode {
  id: number;
  node_key: string;
  node_type: WorkflowNodeType;
  name?: string;
  unified_job_id?: number | null;
  run_id?: string | null;
  status: string;
  callback_url?: string; // webhook_in: populated while awaiting_event
  approval_timeout_seconds?: number;
  approval_timeout_action?: 'approved' | 'rejected';
  awaiting_since?: string;
  decided_at?: string;
  timed_out?: boolean;
}

export interface WorkflowJob {
  id: number;
  workflow_template_id?: number;
  organization_id?: number;
  name?: string;
  status: string;
  created_at?: string;
  finished_at?: string | null;
  nodes: WorkflowJobNode[];
  edges?: WorkflowEdge[];
}

export interface WorkflowRunSummary {
  id: number;
  workflow_template_id: number;
  template_name: string;
  organization_id: number;
  status: string;
  created_at: string;
  finished_at?: string | null;
}

export interface WorkflowApproval {
  id: number;
  workflow_job_id: number;
  workflow_template_id: number;
  organization_id: number;
  workflow_name: string;
  node_name: string;
  node_key: string;
  run_created_at: string;
  awaiting_since: string;
  requested_by?: string;
  deadline?: string;
  timeout_action: 'approved' | 'rejected';
}

// Infrastructure models
