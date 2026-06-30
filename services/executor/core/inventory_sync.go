package core

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

// syncInventory runs `ansible-inventory --list` against the source and POSTs the
// resulting JSON to ingestion, which upserts hosts/groups into the inventory.
// The executor emits the lifecycle events itself (there is no host-runner here).
func (r *BootstrapRunner) syncInventory(req *events.ExecutionRequest, eventChan chan<- events.JobEvent) error {
	m := req.JobManifest
	log.Printf("Inventory sync for run %s -> inventory %d", req.ExecutionRunID, m.SyncInventoryID)
	eventChan <- events.JobEvent{
		ExecutionRunID: req.ExecutionRunID, UnifiedJobID: req.UnifiedJobID,
		Seq: 1, EventType: "JOB_STARTED", Timestamp: time.Now(),
	}

	dir, err := os.MkdirTemp("", "praetor-sync-")
	if err != nil {
		return r.syncFail(req, eventChan, err)
	}
	defer os.RemoveAll(dir)

	// A plugin/static config is a .yml file; a script is an executable.
	name, mode := "source.yml", os.FileMode(0644)
	if m.InventorySourceKind == "script" {
		name, mode = "source", os.FileMode(0o755)
	}
	srcPath := filepath.Join(dir, name)
	if err := os.WriteFile(srcPath, []byte(m.InventorySource), mode); err != nil {
		return r.syncFail(req, eventChan, err)
	}

	cmd := exec.Command("ansible-inventory", "-i", srcPath, "--list")
	// Apply credential injectors so the inventory plugin can authenticate.
	env := os.Environ()
	for k, v := range m.CredentialEnv {
		env = append(env, k+"="+v)
	}
	for k, content := range m.CredentialFiles {
		// k is an env var name (alnum/underscore), safe to use as a filename.
		fp := filepath.Join(dir, "cred_"+k)
		if err := os.WriteFile(fp, []byte(content), 0o600); err != nil {
			return r.syncFail(req, eventChan, fmt.Errorf("writing credential file %s: %w", k, err))
		}
		env = append(env, k+"="+fp)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return r.syncFail(req, eventChan, fmt.Errorf("ansible-inventory failed: %v: %s", err, out))
	}

	ingestion := os.Getenv("INGESTION_URL")
	if ingestion == "" {
		ingestion = "http://ingestion:8081"
	}
	url := fmt.Sprintf("%s/api/v1/inventories/%d/sync-data", ingestion, m.SyncInventoryID)
	resp, err := http.Post(url, "application/json", bytes.NewReader(out))
	if err != nil {
		return r.syncFail(req, eventChan, fmt.Errorf("posting sync data: %w", err))
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return r.syncFail(req, eventChan, fmt.Errorf("ingestion upsert returned %d", resp.StatusCode))
	}

	msg := fmt.Sprintf("Inventory %d synced", m.SyncInventoryID)
	eventChan <- events.JobEvent{
		ExecutionRunID: req.ExecutionRunID, UnifiedJobID: req.UnifiedJobID,
		Seq: 2, EventType: "JOB_COMPLETED", Timestamp: time.Now(), StdoutSnippet: &msg,
	}
	log.Printf("Inventory sync complete for run %s", req.ExecutionRunID)
	return nil
}

func (r *BootstrapRunner) syncFail(req *events.ExecutionRequest, eventChan chan<- events.JobEvent, cause error) error {
	log.Printf("Inventory sync failed for run %s: %v", req.ExecutionRunID, cause)
	msg := cause.Error()
	eventChan <- events.JobEvent{
		ExecutionRunID: req.ExecutionRunID, UnifiedJobID: req.UnifiedJobID,
		Seq: 2, EventType: "JOB_FAILED", Timestamp: time.Now(), StdoutSnippet: &msg,
	}
	return cause
}
