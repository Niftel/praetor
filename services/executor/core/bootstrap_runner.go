package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/praetordev/praetor/pkg/events"
)

// BootstrapRunner uses Ansible to deploy the Host Runner to targets
type BootstrapRunner struct {
	RunnerPayloadPath  string
	RuntimePayloadPath string
}

func NewBootstrapRunner() *BootstrapRunner {
	runtimePayload := os.Getenv("RUNTIME_PAYLOAD_PATH")
	if runtimePayload == "" {
		// The self-contained Ansible runtime bundle the executor pushes onto
		// glibc target hosts. Arch-matched to the host-runner binary.
		runtimePayload = "/tmp/build/runtime/ansible-runtime-linux-arm64.tar.gz"
	}
	return &BootstrapRunner{
		RunnerPayloadPath:  "/tmp/build/linux/praetor-host-runner",
		RuntimePayloadPath: runtimePayload,
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

	// SSH key used to reach the target: default to the platform's shared key, but
	// if the job carries a machine credential (its injector renders the private
	// key into CredentialFiles["ANSIBLE_PRIVATE_KEY_FILE"]), write that out and
	// use it instead — both for this bootstrap connection and, copied to the
	// target, for the host-runner's downstream plays. This is what lets a job
	// authenticate to real hosts that don't trust the shared platform key.
	sshKeyPath := "/tmp/keys/id_rsa"
	if content := req.JobManifest.CredentialFiles["ANSIBLE_PRIVATE_KEY_FILE"]; content != "" {
		// SSH rejects a private key file that does not end in a newline with the
		// cryptic "error in libcrypto" — an easy mistake when a key is pasted into
		// the UI without a trailing newline. Normalise it so pasted keys just work.
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		credKeyPath := fmt.Sprintf("/tmp/cred-key-%s", req.ExecutionRunID)
		if err := os.WriteFile(credKeyPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("failed to write credential key: %w", err)
		}
		defer os.Remove(credKeyPath)
		sshKeyPath = credKeyPath
		log.Printf("BootstrapRunner: using machine-credential SSH key for run %s", req.ExecutionRunID)
	}

	// The host-runner calls back to the control plane from the TARGET host, so the
	// callback URL must be reachable from there. Internal service DNS
	// (ingestion:8081) only resolves for hosts on Praetor's own network; managed
	// hosts elsewhere need the control plane's externally-reachable address.
	// HOST_RUNNER_CALLBACK_URL provides it, falling back to INGESTION_URL for
	// on-network hosts.
	callbackURL := os.Getenv("HOST_RUNNER_CALLBACK_URL")
	if callbackURL == "" {
		callbackURL = os.Getenv("INGESTION_URL")
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

    - name: Detect existing runtime and libc
      # "present" if the self-contained runtime is already extracted, else the
      # host's libc. The glibc runtime bundle can't run on musl (Alpine).
      shell: |
        if [ -x /opt/praetor/runtime/bin/ansible-playbook ]; then echo present; exit 0; fi
        if ls /lib/ld-musl-*.so.1 >/dev/null 2>&1; then echo musl; else echo glibc; fi
      register: praetor_rt
      changed_when: false

    - name: Ensure /opt/praetor exists
      file:
        path: /opt/praetor
        state: directory
        mode: '0755'
      when: praetor_rt.stdout == "glibc"

    - name: Push the self-contained Ansible runtime
      # The execution environment (Python + Ansible) is pushed onto the host so it
      # needs nothing pre-installed. The host-runner extracts and runs it. Skipped
      # when already present or on musl (no glibc bundle for it yet).
      copy:
        src: %s
        dest: /opt/praetor/ansible-runtime.tar.gz
      when: praetor_rt.stdout == "glibc"

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

    - name: Install resume systemd unit (best-effort; skipped where systemd is absent)
      shell: |
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
          cat > /etc/systemd/system/praetor-resume.service <<'UNIT'
        [Unit]
        Description=Praetor host-runner — resume interrupted jobs after a restart
        After=network-online.target
        Wants=network-online.target

        [Service]
        Type=simple
        ExecStart=/usr/local/bin/praetor-host-runner --resume-root=/var/lib/praetor/jobs
        TimeoutStartSec=0

        [Install]
        WantedBy=multi-user.target
        UNIT
          systemctl daemon-reload
          systemctl enable praetor-resume.service
        fi
      args:
        executable: /bin/sh
      failed_when: false

    - name: Copy Manifest
      copy:
        src: %s
        dest: /var/lib/praetor/jobs/%s/manifest.json

    - name: Copy SSH Key
      copy:
        src: %s
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
`, targetHosts, req.ExecutionRunID, r.RunnerPayloadPath, r.RuntimePayloadPath, manifestPath, req.ExecutionRunID, sshKeyPath, req.ExecutionRunID, req.ExecutionRunID, req.ExecutionRunID, callbackURL, req.ExecutionRunID, req.ExecutionRunID)

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
	// Authenticate this bootstrap connection with the chosen SSH key (shared or
	// machine-credential) and any machine-credential env the injector produced
	// (ANSIBLE_REMOTE_USER / ANSIBLE_PASSWORD). ANSIBLE_REMOTE_USER is only a
	// default — a per-host ansible_user in the inventory still takes precedence.
	cmd.Env = append(cmd.Env, "ANSIBLE_PRIVATE_KEY_FILE="+sshKeyPath)
	cmd.Env = append(cmd.Env, "ANSIBLE_HOST_KEY_CHECKING=False")
	for k, v := range req.JobManifest.CredentialEnv {
		if v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	output, err := cmd.CombinedOutput()
	log.Printf("Bootstrap Output:\n%s", string(output))

	if err != nil {
		log.Printf("Bootstrap failed: %v", err)
		return fmt.Errorf("bootstrap playbook failed: %w", err)
	}

	log.Printf("Bootstrap successful for run %s", req.ExecutionRunID)
	return nil
}
