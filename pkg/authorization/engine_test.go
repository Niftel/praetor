package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/praetordev/praetor/pkg/accesscontrol"
	engine "github.com/praetordev/rbac/v4"
)

type fakeResolver struct {
	object []engine.Grant
	global []engine.Grant
	scoped []engine.Grant
	all    []int64
}

func TestFilePolicyKeepsLastKnownGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, defaultPolicy, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewFile(context.Background(), fakeResolver{}, path)
	if err != nil {
		t.Fatal(err)
	}
	before := a.PolicyStatus()
	if !before.Loaded || before.Version == "" || before.Source != path {
		t.Fatalf("unexpected initial status: %+v", before)
	}

	if err := os.WriteFile(path, []byte(`[{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshPolicy(context.Background()); err == nil {
		t.Fatal("malformed refresh must fail")
	}
	after := a.PolicyStatus()
	if after.Version != before.Version || !after.Loaded {
		t.Fatalf("bad refresh replaced last-known-good: before=%+v after=%+v", before, after)
	}

	updated := append(append([]byte(nil), defaultPolicy...), '\n')
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.PolicyStatus().Version == before.Version {
		t.Fatal("valid changed policy did not install a new snapshot")
	}
}

func TestFilePolicyFailsClosedAtStartup(t *testing.T) {
	if _, err := NewFile(context.Background(), fakeResolver{}, filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing configured policy must fail startup")
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", (1<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFile(context.Background(), fakeResolver{}, path); err == nil {
		t.Fatal("oversized configured policy must fail startup")
	}
}

func TestPeriodicRefreshReportsFailureAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, defaultPolicy, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewFile(context.Background(), fakeResolver{}, path)
	if err != nil {
		t.Fatal(err)
	}
	initialVersion := a.PolicyStatus().Version
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := make(chan error, 1)
	go a.RefreshEvery(ctx, 10*time.Millisecond, func(err error) {
		select {
		case errors <- err:
		default:
		}
	})

	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-errors:
	case <-time.After(time.Second):
		t.Fatal("periodic refresh did not report malformed policy")
	}
	failed := a.PolicyStatus()
	if failed.Version != initialVersion || failed.LastRefreshError == "" {
		t.Fatalf("failure did not retain and report last-known-good: %+v", failed)
	}

	updated := append(append([]byte(nil), defaultPolicy...), '\n')
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := a.PolicyStatus()
		if status.Version != initialVersion && status.LastRefreshError == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("periodic refresh did not recover with the valid policy")
}

func TestVerifiedFileRejectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, defaultPolicy, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(defaultPolicy)
	a, err := NewVerifiedFile(context.Background(), fakeResolver{}, path, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	before := a.PolicyStatus()
	if before.Integrity != "sha256" {
		t.Fatalf("integrity status = %q", before.Integrity)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), defaultPolicy...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshPolicy(context.Background()); err == nil {
		t.Fatal("changed bytes must fail the pinned digest")
	}
	after := a.PolicyStatus()
	if after.Version != before.Version || after.LastRefreshError == "" {
		t.Fatalf("tampered update disturbed last-known-good: %+v", after)
	}
}

func (f fakeResolver) ObjectGrants(context.Context, int64, accesscontrol.ResourceKind, int64) ([]engine.Grant, error) {
	return f.object, nil
}
func (f fakeResolver) GlobalGrants(context.Context, int64) ([]engine.Grant, error) {
	return f.global, nil
}
func (f fakeResolver) ScopedGrants(context.Context, int64, accesscontrol.ResourceKind) ([]engine.Grant, error) {
	return f.scoped, nil
}
func (f fakeResolver) AllIDsOfType(context.Context, accesscontrol.ResourceKind) ([]int64, error) {
	return f.all, nil
}

func TestObjectDecisionUsesV4Policy(t *testing.T) {
	const userID int64 = 7
	obj := accesscontrol.Object(accesscontrol.Inventory, 42)
	grant := engine.Grant{Capability: "view_inventory", Scope: "inventory:42", Effect: engine.Allow}
	a, err := New(fakeResolver{object: []engine.Grant{grant}})
	if err != nil {
		t.Fatal(err)
	}
	if a.policy.Version() == "" || a.policy.Current() == nil {
		t.Fatal("RBAC v4 loader did not install an immutable policy snapshot")
	}

	allowed, err := a.Can(context.Background(), accesscontrol.Principal{UserID: userID}, accesscontrol.View, obj)
	if err != nil || !allowed {
		t.Fatalf("matching grant: allowed=%v err=%v", allowed, err)
	}
	allowed, err = a.Can(context.Background(), accesscontrol.Principal{UserID: userID}, accesscontrol.Manage, obj)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("mismatched capability must be denied")
	}
}

func TestGlobalAndVisibleDecisions(t *testing.T) {
	sub := accesscontrol.Principal{UserID: 7}
	global := engine.Grant{Capability: "view_inventory", Scope: "", Effect: engine.Allow}
	a, err := New(fakeResolver{global: []engine.Grant{global}, all: []int64{2, 5}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := a.VisibleIDs(context.Background(), sub, accesscontrol.View, accesscontrol.Inventory)
	if err != nil || !reflect.DeepEqual(ids, []int64{2, 5}) {
		t.Fatalf("global visible ids: ids=%v err=%v", ids, err)
	}

	a, err = New(fakeResolver{scoped: []engine.Grant{
		{Capability: "view_inventory", Scope: "inventory:3", Effect: engine.Allow},
		{Capability: "manage_inventory", Scope: "inventory:4", Effect: engine.Allow},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err = a.VisibleIDs(context.Background(), sub, accesscontrol.View, accesscontrol.Inventory)
	if err != nil || !reflect.DeepEqual(ids, []int64{3}) {
		t.Fatalf("scoped visible ids: ids=%v err=%v", ids, err)
	}
}

func TestDenyOverrides(t *testing.T) {
	a, err := New(fakeResolver{object: []engine.Grant{
		{Capability: "view_inventory", Scope: "", Effect: engine.Allow},
		{Capability: "view_inventory", Scope: "inventory:42", Effect: engine.Deny},
	}})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := a.Can(context.Background(), accesscontrol.Principal{UserID: 7}, accesscontrol.View, accesscontrol.Object(accesscontrol.Inventory, 42))
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("explicit deny must override a global allow")
	}
}

func TestDecisionObserverReceivesV4Provenance(t *testing.T) {
	grant := engine.Grant{Capability: "view_inventory", Scope: "inventory:42", Effect: engine.Allow}
	a, err := New(fakeResolver{object: []engine.Grant{grant}})
	if err != nil {
		t.Fatal(err)
	}
	var events []DecisionEvent
	a.SetDecisionObserver(func(event DecisionEvent) { events = append(events, event) })

	obj := accesscontrol.Object(accesscontrol.Inventory, 42)
	if allowed, err := a.Can(context.Background(), accesscontrol.Principal{UserID: 7}, accesscontrol.View, obj); err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	if allowed, err := a.Can(context.Background(), accesscontrol.Principal{UserID: 7}, accesscontrol.Manage, obj); err != nil || allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	if len(events) != 2 {
		t.Fatalf("events=%d, want 2", len(events))
	}
	allowed := events[0]
	if allowed.UserID != 7 || allowed.Capability != "view_inventory" || allowed.Scope != "inventory:42" || !allowed.Allow || allowed.Snapshot == "" || allowed.RuleID == nil || allowed.RuleEffect != "ALLOW" {
		t.Fatalf("unexpected allow event: %+v", allowed)
	}
	denied := events[1]
	if denied.Allow || denied.Snapshot != allowed.Snapshot || denied.RuleID != nil || denied.Reason == "" {
		t.Fatalf("unexpected default-deny event: %+v", denied)
	}
}
