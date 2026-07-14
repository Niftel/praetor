// Package authorization adapts Praetor's persisted capability grants to the
// domain-blind github.com/praetordev/rbac/v4 policy engine.
package authorization

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/praetordev/praetor/pkg/accesscontrol"
	engine "github.com/praetordev/rbac/v4"
)

// Resolver is the trusted attribute boundary. Implementations obtain grants
// from server-controlled state using the authenticated subject id; request data
// must never be accepted as a grant source.
type Resolver interface {
	ObjectGrants(context.Context, int64, accesscontrol.ResourceKind, int64) ([]engine.Grant, error)
	GlobalGrants(context.Context, int64) ([]engine.Grant, error)
	ScopedGrants(context.Context, int64, accesscontrol.ResourceKind) ([]engine.Grant, error)
	AllIDsOfType(context.Context, accesscontrol.ResourceKind) ([]int64, error)
}

type Authorizer struct {
	grants Resolver
	policy *engine.Loader
}

var _ accesscontrol.DecisionPoint = (*Authorizer)(nil)

const policy = `[
  {"name":"allow-exact-global","effect":"allow","when":{"all":[
    {"eq":[{"attr":"grant.effect"},{"lit":"allow"}]},
    {"eq":[{"attr":"grant.cap"},{"attr":"need"}]},
    {"eq":[{"attr":"grant.scope"},{"lit":""}]}
  ]}},
  {"name":"allow-exact-scoped","effect":"allow","when":{"all":[
    {"ne":[{"attr":"scope"},{"lit":""}]},
    {"eq":[{"attr":"grant.effect"},{"lit":"allow"}]},
    {"eq":[{"attr":"grant.cap"},{"attr":"need"}]},
    {"eq":[{"attr":"grant.scope"},{"attr":"scope"}]}
  ]}},
  {"name":"deny-exact","effect":"deny","when":{"all":[
    {"eq":[{"attr":"grant.effect"},{"lit":"deny"}]},
    {"eq":[{"attr":"grant.cap"},{"attr":"need"}]},
    {"any":[
      {"eq":[{"attr":"grant.scope"},{"lit":""}]},
      {"eq":[{"attr":"grant.scope"},{"attr":"scope"}]}
    ]}
  ]}}
]`

func New(resolver Resolver) (*Authorizer, error) {
	loader := engine.NewLoader(engine.NewMemorySource([]byte(policy)), engine.DenyOverrides)
	if err := loader.Refresh(context.Background()); err != nil {
		return nil, fmt.Errorf("load Praetor RBAC policy: %w", err)
	}
	return &Authorizer{grants: resolver, policy: loader}, nil
}

func scope(contentType accesscontrol.ResourceKind, objectID int64) string {
	return fmt.Sprintf("%s:%d", contentType, objectID)
}

func (a *Authorizer) decide(grants []engine.Grant, need, target string) bool {
	return a.policy.Decide(engine.Query{Grants: grants, Need: need, Scope: target}).Allow
}

func (a *Authorizer) Can(ctx context.Context, sub accesscontrol.Principal, action accesscontrol.Verb, obj accesscontrol.Resource) (bool, error) {
	if !accesscontrol.IsCapability(obj.Kind, action) {
		return false, fmt.Errorf("capability %q is not defined for content type %q", action, obj.Kind)
	}
	return a.CanCapability(ctx, sub, accesscontrol.Capability(obj.Kind, action), obj)
}

func (a *Authorizer) CanCapability(ctx context.Context, sub accesscontrol.Principal, codename string, obj accesscontrol.Resource) (bool, error) {
	grants, err := a.grants.ObjectGrants(ctx, sub.UserID, obj.Kind, obj.ID)
	if err != nil {
		return false, fmt.Errorf("resolve object grants: %w", err)
	}
	return a.decide(grants, codename, scope(obj.Kind, obj.ID)), nil
}

func (a *Authorizer) CanGlobal(ctx context.Context, sub accesscontrol.Principal, codename string) (bool, error) {
	grants, err := a.grants.GlobalGrants(ctx, sub.UserID)
	if err != nil {
		return false, fmt.Errorf("resolve global grants: %w", err)
	}
	return a.decide(grants, codename, ""), nil
}

func (a *Authorizer) VisibleIDs(ctx context.Context, sub accesscontrol.Principal, action accesscontrol.Verb, contentType accesscontrol.ResourceKind) ([]int64, error) {
	if !accesscontrol.IsCapability(contentType, action) {
		return nil, fmt.Errorf("capability %q is not defined for content type %q", action, contentType)
	}
	codename := accesscontrol.Capability(contentType, action)
	global, err := a.grants.GlobalGrants(ctx, sub.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve global grants: %w", err)
	}
	if a.decide(global, codename, "") {
		return a.grants.AllIDsOfType(ctx, contentType)
	}

	grants, err := a.grants.ScopedGrants(ctx, sub.UserID, contentType)
	if err != nil {
		return nil, fmt.Errorf("resolve scoped grants: %w", err)
	}
	seen := make(map[string]struct{}, len(grants))
	ids := make([]int64, 0, len(grants))
	for _, grant := range grants {
		if _, ok := seen[grant.Scope]; ok || !a.decide(grants, codename, grant.Scope) {
			continue
		}
		ct, rawID, ok := strings.Cut(grant.Scope, ":")
		id, err := strconv.ParseInt(rawID, 10, 64)
		if !ok || err != nil || ct != string(contentType) {
			continue
		}
		seen[grant.Scope] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func (a *Authorizer) AllIDsOfType(ctx context.Context, contentType accesscontrol.ResourceKind) ([]int64, error) {
	return a.grants.AllIDsOfType(ctx, contentType)
}
