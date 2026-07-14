// Package accesscontrol defines Praetor's authorization vocabulary and
// application-facing contracts. Policy evaluation is delegated to RBAC v4.
package accesscontrol

import "context"

type ResourceKind string

const (
	Organization     ResourceKind = "organization"
	Team             ResourceKind = "team"
	Project          ResourceKind = "project"
	Inventory        ResourceKind = "inventory"
	Credential       ResourceKind = "credential"
	JobTemplate      ResourceKind = "job_template"
	WorkflowTemplate ResourceKind = "workflow_template"
)

type Verb string

const (
	View    Verb = "view"
	Add     Verb = "add"
	Change  Verb = "change"
	Delete  Verb = "delete"
	Manage  Verb = "manage"
	Use     Verb = "use"
	Execute Verb = "execute"
	Update  Verb = "update"
	Adhoc   Verb = "adhoc"
	Approve Verb = "approve"
)

type Resource struct {
	Kind ResourceKind
	ID   int64
}

func Object(kind ResourceKind, id int64) Resource { return Resource{Kind: kind, ID: id} }

type Principal struct {
	UserID         int64
	BreakGlassRoot bool
}

// DecisionPoint is the only authorization contract used by application code.
// Implementations must fail closed and source grants from authenticated state.
type DecisionPoint interface {
	Can(context.Context, Principal, Verb, Resource) (bool, error)
	CanCapability(context.Context, Principal, string, Resource) (bool, error)
	CanGlobal(context.Context, Principal, string) (bool, error)
	VisibleIDs(context.Context, Principal, Verb, ResourceKind) ([]int64, error)
}
