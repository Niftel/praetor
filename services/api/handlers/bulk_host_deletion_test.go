package handlers

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestBulkHostDeleteConfirmationTokenIsOpaqueAndHashed(t *testing.T) {
	token, digest, err := newBulkHostConfirmationToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token is not 256-bit base64url: len=%d err=%v", len(raw), err)
	}
	if len(digest) != 64 || token == digest {
		t.Fatalf("unexpected token digest: token=%q digest=%q", token, digest)
	}
	other, otherDigest, err := newBulkHostConfirmationToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == other || digest == otherDigest {
		t.Fatal("independent confirmation tokens collided")
	}
}

func TestBulkHostDeleteStateHashAndBlockersCoverDestructiveState(t *testing.T) {
	state := bulkHostState{
		ID: 12, InventoryID: 8, Name: "edge-01", ModifiedAt: time.Unix(100, 5).UTC(),
	}
	original := bulkHostStateHash(state)
	if blockers := blockersForBulkHost(state); len(blockers) != 0 {
		t.Fatalf("unrelated state produced blockers: %+v", blockers)
	}
	state.IsRunnerHost = true
	state.DelegatedCount = 2
	state.GroupCount = 3
	state.JobEventCount = 4
	blockers := blockersForBulkHost(state)
	if len(blockers) != 2 ||
		blockers[0].Code != "inventory_runner" ||
		blockers[1].Code != "delegated_launch_grant" ||
		blockers[1].Count != 2 {
		t.Fatalf("live relationships not reported: %+v", blockers)
	}
	if original == bulkHostStateHash(state) {
		t.Fatal("snapshot hash ignored blocker changes")
	}
	effects := effectsForBulkHost(state)
	if len(effects) != 2 ||
		effects[0].Code != "group_membership" || effects[0].Effect != "delete" ||
		effects[1].Code != "job_event_host_reference" || effects[1].Effect != "detach" {
		t.Fatalf("affected relationships not reported: %+v", effects)
	}
	state.ModifiedAt = state.ModifiedAt.Add(time.Second)
	if original == bulkHostStateHash(state) {
		t.Fatal("snapshot hash ignored host modification")
	}
}

func TestPublicBulkHostDeletePreviewOmitsConfirmationSnapshot(t *testing.T) {
	internal := []bulkHostDeletePreviewResult{{
		Index: 0, Status: "ready", HTTPStatus: 200, HostID: 4,
		Blockers: []bulkHostDeleteBlocker{}, SnapshotHash: "sensitive-internal-binding",
	}}
	public := publicBulkHostDeletePreview(internal)
	if len(public) != 1 || public[0].HostID != 4 || internal[0].SnapshotHash == "" {
		t.Fatalf("preview projection lost public state: %+v", public)
	}
}
