package launch

import (
	"encoding/json"
	"reflect"
	"testing"
)

func strptr(s string) *string { return &s }

func TestJobArgsEmptyIsEmptyObject(t *testing.T) {
	if got := string(Options{}.JobArgs()); got != "{}" {
		t.Fatalf("empty Options.JobArgs() = %q, want {}", got)
	}
}

func TestJobArgsRoundTrip(t *testing.T) {
	in := Options{
		ExtraVars:         map[string]interface{}{"env": "prod"},
		Limit:             strptr("web"),
		InventorySourceID: 7,
	}
	got := ParseArgs(in.JobArgs())
	if !reflect.DeepEqual(got.ExtraVars, in.ExtraVars) {
		t.Errorf("ExtraVars round-trip: got %v want %v", got.ExtraVars, in.ExtraVars)
	}
	if got.Limit == nil || *got.Limit != "web" {
		t.Errorf("Limit round-trip: got %v want web", got.Limit)
	}
	if got.InventorySourceID != 7 {
		t.Errorf("InventorySourceID round-trip: got %d want 7", got.InventorySourceID)
	}
}

// TestParseArgsBackwardCompatible guards that Options reads the exact job_args
// JSON shape older rows were written with (the previous scheduler launch_args
// struct), so the pkg/launch cutover doesn't strand in-flight jobs.
func TestParseArgsBackwardCompatible(t *testing.T) {
	old := json.RawMessage(`{"extra_vars":{"eda_event":{"host":"h1"}},"limit":"h1","inventory_source_id":9}`)
	o := ParseArgs(old)
	if o.EffectiveLimit("default") != "h1" {
		t.Errorf("EffectiveLimit = %q, want h1", o.EffectiveLimit("default"))
	}
	if o.InventorySourceID != 9 {
		t.Errorf("InventorySourceID = %d, want 9", o.InventorySourceID)
	}
	if _, ok := o.ExtraVars["eda_event"]; !ok {
		t.Errorf("expected eda_event in ExtraVars, got %v", o.ExtraVars)
	}
}

func TestMergeExtraVarsLaunchWins(t *testing.T) {
	tpl := json.RawMessage(`{"a":"template","b":"template"}`)
	o := Options{ExtraVars: map[string]interface{}{"b": "launch", "c": "launch"}}
	got := o.MergeExtraVars(tpl)
	want := map[string]interface{}{"a": "template", "b": "launch", "c": "launch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeExtraVars = %v, want %v", got, want)
	}
}

func TestEffectiveLimit(t *testing.T) {
	if got := (Options{}).EffectiveLimit("tpl"); got != "tpl" {
		t.Errorf("no override: got %q want tpl", got)
	}
	if got := (Options{Limit: strptr("override")}).EffectiveLimit("tpl"); got != "override" {
		t.Errorf("override: got %q want override", got)
	}
	// An explicit empty --limit is a real override, distinct from "unset".
	if got := (Options{Limit: strptr("")}).EffectiveLimit("tpl"); got != "" {
		t.Errorf("explicit empty override: got %q want empty", got)
	}
}
