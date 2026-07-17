package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/praetordev/events"
)

const fixtureVersion = "v1"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureVersion, name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func rawFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureVersion, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func requireKeys(t *testing.T, payload []byte, keys ...string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Errorf("required wire key %q is absent", key)
		}
	}
	return object
}

func TestExecutionRequestV1(t *testing.T) {
	payload := fixture(t, "execution_request")
	requireKeys(t, payload, "manifest_version", "execution_run_id", "unified_job_id", "job_manifest", "created_at")

	var request events.ExecutionRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("released events module cannot decode execution request v1: %v", err)
	}
	if request.ManifestVersion != 1 || request.ExecutionRunID.String() != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("execution request identity did not survive decoding: %+v", request)
	}
	if request.JobManifest.Playbook != "site.yml" || request.JobManifest.CredentialID != 13 {
		t.Fatalf("execution manifest fields did not survive decoding: %+v", request.JobManifest)
	}
}

func TestJobEventV1(t *testing.T) {
	payload := fixture(t, "job_event")
	requireKeys(t, payload, "execution_run_id", "unified_job_id", "seq", "event_type", "timestamp")

	var event events.JobEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("released events module cannot decode job event v1: %v", err)
	}
	if event.Seq != 17 || event.EventType != "TASK_OK" || event.Host == nil || *event.Host != "host-1" {
		t.Fatalf("job event fields did not survive decoding: %+v", event)
	}
}

func TestJobEventBatchV1(t *testing.T) {
	payload := fixture(t, "job_event_batch")
	var batch []json.RawMessage
	if err := json.Unmarshal(payload, &batch); err != nil {
		t.Fatalf("decode event batch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("event batch contains %d events, want 2", len(batch))
	}
	for i, event := range batch {
		requireKeys(t, event, "execution_run_id", "unified_job_id", "seq", "event_type", "timestamp")
		var decoded events.JobEvent
		if err := json.Unmarshal(event, &decoded); err != nil {
			t.Fatalf("decode event %d: %v", i, err)
		}
	}
}

func TestLogChunkV1(t *testing.T) {
	payload := fixture(t, "log_chunk")
	requireKeys(t, payload, "execution_run_id", "unified_job_id", "seq", "storage_key", "byte_length", "timestamp")

	var chunk events.LogChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		t.Fatalf("released events module cannot decode log chunk v1: %v", err)
	}
	if chunk.Seq != 3 || chunk.ByteLength != 4096 || chunk.StorageKey == "" {
		t.Fatalf("log chunk fields did not survive decoding: %+v", chunk)
	}
}

func TestHTTPResponseContractsV1(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"heartbeat_response", []string{"status", "cancel"}},
		{"log_cursor_response", []string{"bytes", "seq"}},
		{"runnable_response", []string{"runnable"}},
		{"credentials_response", []string{"env", "files"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireKeys(t, fixture(t, test.name), test.keys...)
		})
	}
}

func TestInventoryRenderedV1(t *testing.T) {
	payload := rawFixture(t, "inventory_rendered.ini")
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		t.Fatal("rendered inventory must be non-empty and newline terminated")
	}
}

func TestInventoryFactsV1(t *testing.T) {
	payload := fixture(t, "inventory_facts")
	facts := requireKeys(t, payload, "db-1", "web-1")
	for host, raw := range facts {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatalf("facts for %s are not a JSON object: %v", host, err)
		}
	}
}

func TestFactCacheUploadV1(t *testing.T) {
	payload := fixture(t, "fact_cache_upload")
	object := requireKeys(t, payload, "facts")
	var facts map[string]json.RawMessage
	if err := json.Unmarshal(object["facts"], &facts); err != nil {
		t.Fatalf("facts is not a host-keyed JSON object: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("fact upload contains %d hosts, want 2", len(facts))
	}
}

func TestInventorySyncV1(t *testing.T) {
	payload := fixture(t, "inventory_sync")
	object := requireKeys(t, payload, "_meta", "all", "ungrouped", "web")
	var meta struct {
		HostVars map[string]json.RawMessage `json:"hostvars"`
	}
	if err := json.Unmarshal(object["_meta"], &meta); err != nil {
		t.Fatalf("decode _meta: %v", err)
	}
	if len(meta.HostVars) != 2 {
		t.Fatalf("inventory sync contains %d hostvars, want 2", len(meta.HostVars))
	}
}

func TestV1ReadersIgnoreAdditiveFields(t *testing.T) {
	objects := []struct {
		name   string
		target any
	}{
		{"execution_request", &events.ExecutionRequest{}},
		{"job_event", &events.JobEvent{}},
		{"log_chunk", &events.LogChunk{}},
	}
	for _, object := range objects {
		t.Run(object.name, func(t *testing.T) {
			fields := requireKeys(t, fixture(t, object.name))
			fields["future_optional_field"] = json.RawMessage(`{"enabled":true}`)
			payload, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(payload, object.target); err != nil {
				t.Fatalf("v1 reader rejected an additive field: %v", err)
			}
		})
	}
}
