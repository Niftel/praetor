// Package authorization adapts Praetor's persisted capability grants to the
// domain-blind github.com/praetordev/rbac/v4 policy engine.
package authorization

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	grants      Resolver
	policy      *engine.Loader
	source      string
	integrity   string
	mu          sync.RWMutex
	lastAttempt time.Time
	lastSuccess time.Time
	lastError   string
	observer    func(DecisionEvent)
}

var _ accesscontrol.DecisionPoint = (*Authorizer)(nil)

//go:embed policy.json
var defaultPolicy []byte

func New(resolver Resolver) (*Authorizer, error) {
	return newWithSource(context.Background(), resolver, engine.NewMemorySource(defaultPolicy), "embedded", "embedded", nil)
}

func NewFile(ctx context.Context, resolver Resolver, path string) (*Authorizer, error) {
	return NewWithSource(ctx, resolver, engine.NewFileSource(path), path)
}

func NewWithSource(ctx context.Context, resolver Resolver, source engine.Source, description string) (*Authorizer, error) {
	return newWithSource(ctx, resolver, source, description, "passthrough", nil)
}

func newWithSource(ctx context.Context, resolver Resolver, source engine.Source, description, integrity string, verifier engine.Verifier) (*Authorizer, error) {
	options := []engine.LoaderOption{}
	if verifier != nil {
		options = append(options, engine.WithVerifier(verifier))
	}
	loader := engine.NewLoader(source, engine.DenyOverrides, options...)
	if err := loader.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("load Praetor RBAC policy: %w", err)
	}
	now := time.Now().UTC()
	return &Authorizer{grants: resolver, policy: loader, source: description, integrity: integrity, lastAttempt: now, lastSuccess: now}, nil
}

type PolicyStatus struct {
	Version            string    `json:"version"`
	Source             string    `json:"source"`
	Integrity          string    `json:"integrity"`
	Loaded             bool      `json:"loaded"`
	LastRefreshAttempt time.Time `json:"last_refresh_attempt"`
	LastRefreshSuccess time.Time `json:"last_refresh_success"`
	LastRefreshError   string    `json:"last_refresh_error,omitempty"`
}

// DecisionEvent is the stable, security-audit view of an RBAC v4 decision.
// RuleID is nil when the engine default-denied because no rule matched.
type DecisionEvent struct {
	UserID     int64  `json:"user_id"`
	Capability string `json:"capability"`
	Scope      string `json:"scope"`
	Allow      bool   `json:"allow"`
	Snapshot   string `json:"snapshot"`
	Reason     string `json:"reason"`
	RuleID     *int   `json:"rule_id,omitempty"`
	RuleName   string `json:"rule_name,omitempty"`
	RuleEffect string `json:"rule_effect,omitempty"`
}

// SetDecisionObserver installs an optional observer called synchronously after
// every policy evaluation. Observers must return quickly and must not panic.
func (a *Authorizer) SetDecisionObserver(observer func(DecisionEvent)) {
	a.mu.Lock()
	a.observer = observer
	a.mu.Unlock()
}

func (a *Authorizer) PolicyStatus() PolicyStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return PolicyStatus{Version: a.policy.Version(), Source: a.source, Integrity: a.integrity, Loaded: a.policy.Current() != nil,
		LastRefreshAttempt: a.lastAttempt, LastRefreshSuccess: a.lastSuccess, LastRefreshError: a.lastError}
}

func (a *Authorizer) RefreshPolicy(ctx context.Context) error {
	now := time.Now().UTC()
	err := a.policy.Refresh(ctx)
	a.mu.Lock()
	a.lastAttempt = now
	if err != nil {
		a.lastError = err.Error()
	} else {
		a.lastSuccess = now
		a.lastError = ""
	}
	a.mu.Unlock()
	return err
}

func scope(contentType accesscontrol.ResourceKind, objectID int64) string {
	return fmt.Sprintf("%s:%d", contentType, objectID)
}

func (a *Authorizer) decide(userID int64, grants []engine.Grant, need, target string) bool {
	decision := a.policy.Decide(engine.Query{Grants: grants, Need: need, Scope: target})
	a.mu.RLock()
	observer := a.observer
	a.mu.RUnlock()
	if observer != nil {
		event := DecisionEvent{UserID: userID, Capability: need, Scope: target, Allow: decision.Allow, Snapshot: decision.Snapshot, Reason: decision.Reason}
		if rule, ok := decision.Decider(); ok {
			event.RuleID = &rule.ID
			event.RuleName = rule.Name
			event.RuleEffect = rule.Effect.String()
		}
		observer(event)
	}
	return decision.Allow
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
	return a.decide(sub.UserID, grants, codename, scope(obj.Kind, obj.ID)), nil
}

func (a *Authorizer) CanGlobal(ctx context.Context, sub accesscontrol.Principal, codename string) (bool, error) {
	grants, err := a.grants.GlobalGrants(ctx, sub.UserID)
	if err != nil {
		return false, fmt.Errorf("resolve global grants: %w", err)
	}
	return a.decide(sub.UserID, grants, codename, ""), nil
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
	if a.decide(sub.UserID, global, codename, "") {
		return a.grants.AllIDsOfType(ctx, contentType)
	}

	grants, err := a.grants.ScopedGrants(ctx, sub.UserID, contentType)
	if err != nil {
		return nil, fmt.Errorf("resolve scoped grants: %w", err)
	}
	seen := make(map[string]struct{}, len(grants))
	ids := make([]int64, 0, len(grants))
	for _, grant := range grants {
		if _, ok := seen[grant.Scope]; ok || !a.decide(sub.UserID, grants, codename, grant.Scope) {
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
