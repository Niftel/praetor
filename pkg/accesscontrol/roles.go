package accesscontrol

// RoleKind is the stable API vocabulary for Praetor's built-in access roles.
// It selects a role definition; authorization itself evaluates capabilities.
type RoleKind string

const (
	AdminRole             RoleKind = "admin_role"
	MemberRole            RoleKind = "member_role"
	ReadRole              RoleKind = "read_role"
	AuditorRole           RoleKind = "auditor_role"
	ExecuteRole           RoleKind = "execute_role"
	ProjectAdminRole      RoleKind = "project_admin_role"
	InventoryAdminRole    RoleKind = "inventory_admin_role"
	CredentialAdminRole   RoleKind = "credential_admin_role"
	WorkflowAdminRole     RoleKind = "workflow_admin_role"
	NotificationAdminRole RoleKind = "notification_admin_role"
	JobTemplateAdminRole  RoleKind = "job_template_admin_role"
	ApprovalRole          RoleKind = "approval_role"
	UseRole               RoleKind = "use_role"
	UpdateRole            RoleKind = "update_role"
	AdhocRole             RoleKind = "adhoc_role"
)

var builtinRoleNames = map[ResourceKind]map[RoleKind]string{
	Organization: {
		AdminRole: "Organization Admin", MemberRole: "Organization Member", ReadRole: "Organization Read",
		AuditorRole: "Organization Auditor", ExecuteRole: "Organization Execute",
		ProjectAdminRole: "Organization Project Admin", InventoryAdminRole: "Organization Inventory Admin",
		CredentialAdminRole: "Organization Credential Admin", JobTemplateAdminRole: "Organization Job Template Admin",
		WorkflowAdminRole: "Organization Workflow Admin", ApprovalRole: "Organization Approval",
	},
	Team: {
		AdminRole: "Team Admin", MemberRole: "Team Member", ReadRole: "Team Read",
	},
	Project: {
		AdminRole: "Project Admin", UseRole: "Project Use", UpdateRole: "Project Update", ReadRole: "Project Read",
	},
	Inventory: {
		AdminRole: "Inventory Admin", UseRole: "Inventory Use", UpdateRole: "Inventory Update",
		AdhocRole: "Inventory Adhoc", ReadRole: "Inventory Read",
	},
	Credential: {
		AdminRole: "Credential Admin", UseRole: "Credential Use", ReadRole: "Credential Read",
	},
	JobTemplate: {
		AdminRole: "Job Template Admin", ExecuteRole: "Job Template Execute", ReadRole: "Job Template Read",
	},
	WorkflowTemplate: {
		AdminRole: "Workflow Template Admin", ExecuteRole: "Workflow Template Execute",
		ApprovalRole: "Workflow Template Approve", ReadRole: "Workflow Template Read",
	},
}

func BuiltinRoleName(kind ResourceKind, role RoleKind) (string, bool) {
	name, ok := builtinRoleNames[kind][role]
	return name, ok
}

type BuiltinRole struct {
	Name         string
	Description  string
	ResourceKind *ResourceKind // nil is a global system role
	Capabilities []string
}

func capabilityList(kind ResourceKind, verbs ...Verb) []string {
	out := make([]string, 0, len(verbs))
	for _, verb := range verbs {
		out = append(out, Capability(kind, verb))
	}
	return out
}

func allCapabilitiesFor(kind ResourceKind) []string {
	return capabilityList(kind, verbsByResource[kind]...)
}

func allResourceCapabilities() []string {
	var out []string
	for _, kind := range resourceOrder {
		out = append(out, allCapabilitiesFor(kind)...)
	}
	return out
}

func viewCapabilities() []string {
	out := make([]string, 0, len(resourceOrder))
	for _, kind := range resourceOrder {
		out = append(out, Capability(kind, View))
	}
	return out
}

func scopedRole(kind ResourceKind, role RoleKind, description string, capabilities ...string) BuiltinRole {
	name, ok := BuiltinRoleName(kind, role)
	if !ok {
		panic("missing built-in role name")
	}
	return BuiltinRole{Name: name, Description: description, ResourceKind: &kind, Capabilities: capabilities}
}

func BuiltinRoles() []BuiltinRole {
	systemCapabilities := make([]string, 0, len(Catalog()))
	for _, definition := range Catalog() {
		systemCapabilities = append(systemCapabilities, definition.Codename)
	}
	orgViews := viewCapabilities()
	return []BuiltinRole{
		{Name: SystemAdministrator, Description: "Full platform access.", Capabilities: systemCapabilities},
		{Name: SystemAuditor, Description: "Read-only platform access.", Capabilities: append(orgViews, ViewActivityStream)},

		scopedRole(Organization, AdminRole, "Manage the organization and its resources.", allResourceCapabilities()...),
		scopedRole(Organization, MemberRole, "Belong to the organization.", Capability(Organization, View)),
		scopedRole(Organization, ReadRole, "View organization settings.", Capability(Organization, View)),
		scopedRole(Organization, AuditorRole, "View the organization and its resources.", orgViews...),
		scopedRole(Organization, ExecuteRole, "Run executable resources in the organization.",
			Capability(JobTemplate, Execute), Capability(JobTemplate, View), Capability(WorkflowTemplate, Execute), Capability(WorkflowTemplate, View)),
		scopedRole(Organization, ProjectAdminRole, "Manage organization projects.", allCapabilitiesFor(Project)...),
		scopedRole(Organization, InventoryAdminRole, "Manage organization inventories.", allCapabilitiesFor(Inventory)...),
		scopedRole(Organization, CredentialAdminRole, "Manage organization credentials.", allCapabilitiesFor(Credential)...),
		scopedRole(Organization, JobTemplateAdminRole, "Manage organization job templates.", allCapabilitiesFor(JobTemplate)...),
		scopedRole(Organization, WorkflowAdminRole, "Manage organization workflows.", capabilityList(WorkflowTemplate, Add, View, Change, Delete, Manage, Execute)...),
		scopedRole(Organization, ApprovalRole, "Approve organization workflow gates.", Capability(WorkflowTemplate, Approve), Capability(WorkflowTemplate, View)),

		scopedRole(Project, AdminRole, "Manage the project.", capabilityList(Project, View, Change, Delete, Manage, Use, Update)...),
		scopedRole(Project, UseRole, "Use the project.", Capability(Project, Use), Capability(Project, View)),
		scopedRole(Project, UpdateRole, "Update the project.", Capability(Project, Update), Capability(Project, View)),
		scopedRole(Project, ReadRole, "View the project.", Capability(Project, View)),

		scopedRole(Inventory, AdminRole, "Manage the inventory.", capabilityList(Inventory, View, Change, Delete, Manage, Use, Update, Adhoc)...),
		scopedRole(Inventory, UseRole, "Use the inventory.", Capability(Inventory, Use), Capability(Inventory, View)),
		scopedRole(Inventory, UpdateRole, "Update inventory sources.", Capability(Inventory, Update), Capability(Inventory, View)),
		scopedRole(Inventory, AdhocRole, "Run ad-hoc commands.", Capability(Inventory, Adhoc), Capability(Inventory, View)),
		scopedRole(Inventory, ReadRole, "View the inventory.", Capability(Inventory, View)),

		scopedRole(Credential, AdminRole, "Manage the credential.", capabilityList(Credential, View, Change, Delete, Manage, Use)...),
		scopedRole(Credential, UseRole, "Use the credential.", Capability(Credential, Use), Capability(Credential, View)),
		scopedRole(Credential, ReadRole, "View the credential.", Capability(Credential, View)),

		scopedRole(JobTemplate, AdminRole, "Manage the job template.", capabilityList(JobTemplate, View, Change, Delete, Manage, Execute)...),
		scopedRole(JobTemplate, ExecuteRole, "Execute the job template.", Capability(JobTemplate, Execute), Capability(JobTemplate, View)),
		scopedRole(JobTemplate, ReadRole, "View the job template.", Capability(JobTemplate, View)),

		scopedRole(WorkflowTemplate, AdminRole, "Manage the workflow template.", capabilityList(WorkflowTemplate, View, Change, Delete, Manage, Execute)...),
		scopedRole(WorkflowTemplate, ExecuteRole, "Execute the workflow template.", Capability(WorkflowTemplate, Execute), Capability(WorkflowTemplate, View)),
		scopedRole(WorkflowTemplate, ApprovalRole, "Approve workflow gates.", Capability(WorkflowTemplate, Approve), Capability(WorkflowTemplate, View)),
		scopedRole(WorkflowTemplate, ReadRole, "View the workflow template.", Capability(WorkflowTemplate, View)),

		scopedRole(Team, AdminRole, "Manage the team.", capabilityList(Team, View, Change, Delete, Manage)...),
		scopedRole(Team, MemberRole, "Belong to the team.", Capability(Team, View)),
		scopedRole(Team, ReadRole, "View the team.", Capability(Team, View)),
	}
}
