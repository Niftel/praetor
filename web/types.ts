// Types matching backend models exactly (snake_case field names)

// Pagination wrapper for list responses
export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

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

export enum JobStatus {
  SUCCESSFUL = 'successful',
  FAILED = 'failed',
  RUNNING = 'running',
  PENDING = 'pending'
}

export interface ExecutionRun {
  id: string;
  unified_job_id: number;
  attempt_number: number;
  executor_instance_id?: number;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  state: string;
  last_heartbeat_at?: string;
  last_event_seq: number;
  persisted_event_seq: number;
}

export interface JobEvent {
  id: number;
  unified_job_id: number;
  execution_run_id: string;
  seq: number;
  event_type: string;
  host_id?: number;
  task_name?: string;
  play_name?: string;
  event_data: any;
  stdout_snippet?: string;
  created_at: string;
}

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

export interface JobTemplate {
  id: number;
  organization_id: number;
  name: string;
  description?: string;
  inventory_id?: number;
  project_id?: number;
  playbook: string;
  playbook_content?: string;
  execution_environment_id?: number;
  unified_job_template_id?: number;
  credential_id?: number;
  forks: number;
  job_type: string;
  verbosity: number;
  extra_vars?: any;
  limit?: string;
  ask_variables_on_launch?: boolean;
  ask_limit_on_launch?: boolean;
  created_at: string;
  modified_at: string;
}

// Alias for backwards compatibility
export type Template = JobTemplate;

export interface Schedule {
  id: number;
  name: string;
  description?: string;
  unified_job_template_id: number;
  rrule: string;
  next_run: string;
  enabled: boolean;
  extra_vars?: any;
  created_at: string;
  modified_at: string;
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

export interface RoleBinding {
  id: number;
  role_id: number;
  user_id?: number;
  team_id?: number;
  organization_id?: number;
  created_at: string;
}

export interface Organization {
  id: number;
  name: string;
  description?: string;
  created_at: string;
  modified_at?: string;
}

// Infrastructure models
export interface Instance {
  id: number;
  hostname: string;
  ip_address?: string;
  capacity: number;
  version?: string;
  enabled: boolean;
  instance_type?: string; // executor, controller, hybrid
  healthy?: boolean;
  last_heartbeat?: string;
  groups?: InstanceGroup[];
}

export interface InstanceGroup {
  id: number;
  name: string;
  created_at: string;
  modified_at?: string;
}
