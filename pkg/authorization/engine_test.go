package authorization

import (
	"context"
	"reflect"
	"testing"

	legacy "github.com/praetordev/rbac"
	engine "github.com/praetordev/rbac/v4"
)

type fakeResolver struct {
	object []engine.Grant
	global []engine.Grant
	scoped []engine.Grant
	all    []int64
}

func (f fakeResolver) ObjectGrants(context.Context, int64, legacy.ContentType, int64) ([]engine.Grant, error) {
	return f.object, nil
}
func (f fakeResolver) GlobalGrants(context.Context, int64) ([]engine.Grant, error) {
	return f.global, nil
}
func (f fakeResolver) ScopedGrants(context.Context, int64, legacy.ContentType) ([]engine.Grant, error) {
	return f.scoped, nil
}
func (f fakeResolver) AllIDsOfType(context.Context, legacy.ContentType) ([]int64, error) {
	return f.all, nil
}

func TestObjectDecisionUsesV4Policy(t *testing.T) {
	const userID int64 = 7
	obj := legacy.Obj(legacy.ContentTypeInventory, 42)
	grant := engine.Grant{Capability: "view_inventory", Scope: "inventory:42", Effect: engine.Allow}
	a, err := New(fakeResolver{object: []engine.Grant{grant}})
	if err != nil {
		t.Fatal(err)
	}

	allowed, err := a.Can(context.Background(), legacy.NewSubject(userID, false, false), legacy.ActionView, obj)
	if err != nil || !allowed {
		t.Fatalf("matching grant: allowed=%v err=%v", allowed, err)
	}
	allowed, err = a.Can(context.Background(), legacy.NewSubject(userID, false, false), legacy.ActionManage, obj)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("mismatched capability must be denied")
	}
}

func TestGlobalAndVisibleDecisions(t *testing.T) {
	sub := legacy.NewSubject(7, false, false)
	global := engine.Grant{Capability: "view_inventory", Scope: "", Effect: engine.Allow}
	a, err := New(fakeResolver{global: []engine.Grant{global}, all: []int64{2, 5}})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := a.VisibleIDs(context.Background(), sub, legacy.ActionView, legacy.ContentTypeInventory)
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
	ids, err = a.VisibleIDs(context.Background(), sub, legacy.ActionView, legacy.ContentTypeInventory)
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
	allowed, err := a.Can(context.Background(), legacy.NewSubject(7, false, false), legacy.ActionView, legacy.Obj(legacy.ContentTypeInventory, 42))
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("explicit deny must override a global allow")
	}
}
