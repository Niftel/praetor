package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/praetordev/praetor/pkg/events"
)

// BootstrapRunner uses Ansible to deploy the Host Runner to targets
type BootstrapRunner struct {
	RunnerPayloadPath string
}

func NewBootstrapRunner() *BootstrapRunner {
	return &BootstrapRunner{
		RunnerPayloadPath: "/tmp/build/linux/praetor-host-runner",
	}
}

func (r *BootstrapRunner) Run(req *events.ExecutionRequest, eventChan chan<- events.JobEvent) error {
	// Inventory-sync runs don't bootstrap a host-runner: the executor runs
	// ansible-inventory locally and upserts the result via ingestion.
	if req.JobManifest.InventorySync {
		return r.syncInventory(req, eventChan)
	}

	log.Printf("BootstrapRunner: Starting deployment for run %s", req.ExecutionRunID)

	// 1. Serialize Manifest to file (to be uploaded)
	manifestBytes, err := json.Marshal(req) // Upload the whole request including ID
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestPath := fmt.Sprintf("/tmp/manifest-%s.json", req.ExecutionRunID)
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		return fmt.Errorf("failed to write manifest temp file: %w", err)
	}
	defer os.Remove(manifestPath) // Clean up

	// 2. Generate Bootstrap Playbook
	// This playbook runs on the TARGET hosts.
	// We assume inventory is provided in the request (req.JobManifest.Inventory).
	// We write the inventory to a file.
	inventoryPath := fmt.Sprintf("/tmp/inventory-%s.ini", req.ExecutionRunID)
	if err := os.WriteFile(inventoryPath, []byte(req.JobManifest.Inventory), 0644); err != nil {
		return fmt.Errorf("failed to write inventory temp file: %w", err)
	}
	defer os.Remove(inventoryPath)

	targetHosts := "all[0]"
	if req.JobManifest.RunnerHost != "" {
		targetHosts = req.JobManifest.RunnerHost
	} else if req.JobManifest.Inventory == "" {
		targetHosts = "localhost"
	}

	bootstrapPlaybook := fmt.Sprintf(`
- name: Deploy Praetor Host Runner
  hosts: %s
  gather_facts: no
  become: yes
  tasks:
    - name: Ensure job directory exists
      file:
        path: /var/lib/praetor/jobs/%s
        state: directory
        mode: '0755'

    - name: Copy Host Runner Binary
      copy:
        src: %s
        dest: /usr/local/bin/praetor-host-runner
        mode: '0755'

    - name: Ensure Ansible plugin directory exists
      file:
        path: /usr/local/share/praetor/plugins/callback
        state: directory
        mode: '0755'

    - name: Copy checkpoint callback plugin (enables task-level resume)
      copy:
        src: /tmp/plugins/callback/praetor_checkpoint.py
        dest: /usr/local/share/praetor/plugins/callback/praetor_checkpoint.py
        mode: '0644'

    - name: Copy Manifest
      copy:
        src: %s
        dest: /var/lib/praetor/jobs/%s/manifest.json

    - name: Copy SSH Key
      copy:
        src: /tmp/keys/id_rsa
        dest: /var/lib/praetor/jobs/%s/id_rsa
        mode: '0600'

    - name: Start Host Runner (Background)
      shell: |
        export ANSIBLE_HOST_KEY_CHECKING=False
        export ANSIBLE_PRIVATE_KEY_FILE=/var/lib/praetor/jobs/%s/id_rsa
        export ANSIBLE_FORCE_COLOR=1
        nohup /usr/local/bin/praetor-host-runner \
          --job-dir=/var/lib/praetor/jobs/%s \
          --api-url=%s \
          --run-id=%s \
          >> /var/lib/praetor/jobs/%s/runner.log 2>&1 &
      async: 10
      poll: 0
`, targetHosts, req.ExecutionRunID, r.RunnerPayloadPath, manifestPath, req.ExecutionRunID, req.ExecutionRunID, req.ExecutionRunID, req.ExecutionRunID, os.Getenv("INGESTION_URL"), req.ExecutionRunID, req.ExecutionRunID)

	playbookPath := fmt.Sprintf("/tmp/bootstrap-%s.yml", req.ExecutionRunID)
	if err := os.WriteFile(playbookPath, []byte(bootstrapPlaybook), 0644); err != nil {
		return fmt.Errorf("failed to write bootstrap playbook: %w", err)
	}
	defer os.Remove(playbookPath)

	// 3. Execute Ansible (Bootstrap)
	// We use the same SSH keys as before (mounted at /tmp/keys/id_rsa in the container/host)
	// We need to ensure ansible knows where keys are.
	// Typically defaults to ~/.ssh/id_rsa or via inventory vars.
	// Our inventory generation in Reconciler/Scheduler ADDS ssh args!
	// So we trust the inventory.

	cmd := exec.Command("ansible-playbook", "-i", inventoryPath, playbookPath)
	cmd.Env = os.Environ()
	// cmd.Env = append(cmd.Env, "ANSIBLE_HOST_KEY_CHECKING=False") // Handled in inventory usually

	output, err := cmd.CombinedOutput()
	log.Printf("Bootstrap Output:\n%s", string(output))

	if err != nil {
		log.Printf("Bootstrap failed: %v", err)
		return fmt.Errorf("bootstrap playbook failed: %w", err)
	}

	log.Printf("Bootstrap successful for run %s", req.ExecutionRunID)
	return nil
}
