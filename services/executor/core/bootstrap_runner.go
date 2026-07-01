package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/praetordev/praetor/pkg/events"
	"golang.org/x/crypto/ssh"
)

// BootstrapRunner deploys the host-runner (and the self-contained execution
// environment) to a target host directly over SSH — no Ansible and no Python are
// required on the target. It copies the host-runner binary, the Ansible runtime
// bundle, the checkpoint plugin, the manifest and the key over SSH sessions, then
// launches the host-runner. The target only needs sshd, a POSIX shell and tar.
type BootstrapRunner struct {
	RunnerPayloadPath string
	RuntimeDir        string // dir holding <pack>-linux-<arch>.tar.gz Execution Packs
}

func NewBootstrapRunner() *BootstrapRunner {
	runtimeDir := os.Getenv("RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/tmp/build/runtime"
	}
	return &BootstrapRunner{
		RunnerPayloadPath: "/tmp/build/linux/praetor-host-runner",
		RuntimeDir:        runtimeDir,
	}
}

func (r *BootstrapRunner) Run(req *events.ExecutionRequest, eventChan chan<- events.JobEvent) error {
	// Inventory-sync runs don't bootstrap a host-runner: the executor runs
	// ansible-inventory locally and upserts the result via ingestion.
	if req.JobManifest.InventorySync {
		return r.syncInventory(req, eventChan)
	}

	log.Printf("BootstrapRunner: Starting deployment for run %s", req.ExecutionRunID)

	manifestBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	callbackURL := os.Getenv("HOST_RUNNER_CALLBACK_URL")
	if callbackURL == "" {
		callbackURL = os.Getenv("INGESTION_URL")
	}

	// A job with no inventory runs on the executor itself (localhost).
	if req.JobManifest.RunnerHost == "" {
		return r.localBootstrap(req, manifestBytes, callbackURL)
	}

	// Resolve how to reach the runner host from its inventory connection vars.
	vars := parseHostVars(req.JobManifest.Inventory, req.JobManifest.RunnerHost)
	addr := firstNonEmpty(vars["ansible_host"], req.JobManifest.RunnerHost)
	port := firstNonEmpty(vars["ansible_port"], "22")
	user := firstNonEmpty(vars["ansible_user"], req.JobManifest.CredentialEnv["ANSIBLE_REMOTE_USER"], "root")

	// SSH key: the machine-credential key if the job carries one, else the shared
	// platform/automation key.
	sshKeyPath := "/tmp/keys/id_rsa"
	if content := req.JobManifest.CredentialFiles["ANSIBLE_PRIVATE_KEY_FILE"]; content != "" {
		if !strings.HasSuffix(content, "\n") {
			content += "\n" // SSH rejects a key file without a trailing newline
		}
		credKeyPath := fmt.Sprintf("/tmp/cred-key-%s", req.ExecutionRunID)
		if err := os.WriteFile(credKeyPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("failed to write credential key: %w", err)
		}
		defer os.Remove(credKeyPath)
		sshKeyPath = credKeyPath
		log.Printf("BootstrapRunner: using machine-credential SSH key for run %s", req.ExecutionRunID)
	}

	client, err := dialSSH(addr, port, user, sshKeyPath)
	if err != nil {
		return fmt.Errorf("ssh to runner host %s@%s:%s: %w", user, addr, port, err)
	}
	defer client.Close()
	log.Printf("BootstrapRunner: connected to %s@%s:%s for run %s", user, addr, port, req.ExecutionRunID)

	runID := req.ExecutionRunID.String()
	jobDir := "/var/lib/praetor/jobs/" + runID

	// 1. Directories.
	if out, err := runSSH(client, fmt.Sprintf("mkdir -p %s /opt/praetor /usr/local/share/praetor/plugins/callback", sshQuote(jobDir))); err != nil {
		return fmt.Errorf("mkdir on target: %w: %s", err, out)
	}

	// 2. The host-runner binary.
	if err := pushFile(client, r.RunnerPayloadPath, "/usr/local/bin/praetor-host-runner", "0755"); err != nil {
		return fmt.Errorf("push host-runner: %w", err)
	}

	// 3. The checkpoint callback plugin (task-level resume).
	if err := pushFile(client, "/tmp/plugins/callback/praetor_checkpoint.py", "/usr/local/share/praetor/plugins/callback/praetor_checkpoint.py", "0644"); err != nil {
		log.Printf("BootstrapRunner: checkpoint plugin push failed (non-fatal): %v", err)
	}

	// 4. The Execution Pack — the self-contained Ansible runtime — so the host
	// needs no Ansible/Python. Pushes the pack the job selected (or the default).
	pack := firstNonEmpty(req.JobManifest.ExecutionPack, "ansible-runtime")
	if err := r.pushRuntime(client, pack); err != nil {
		log.Printf("BootstrapRunner: pack push failed (non-fatal; will fall back to system ansible): %v", err)
	}

	// 5. The manifest.
	if err := pushBytes(client, manifestBytes, jobDir+"/manifest.json", "0644"); err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}

	// 6. The SSH key the host-runner uses for its downstream plays.
	if err := pushFile(client, sshKeyPath, jobDir+"/id_rsa", "0600"); err != nil {
		return fmt.Errorf("push job key: %w", err)
	}

	// 7. The resume systemd unit (best-effort; skipped where systemd is absent).
	if out, err := runSSH(client, resumeUnitScript); err != nil {
		log.Printf("BootstrapRunner: resume unit install skipped: %v: %s", err, out)
	}

	// 8. Launch the host-runner, detached so it outlives this SSH session.
	start := fmt.Sprintf(
		"setsid /usr/local/bin/praetor-host-runner --job-dir=%s --api-url=%s --run-id=%s >> %s/runner.log 2>&1 </dev/null &",
		jobDir, callbackURL, runID, jobDir,
	)
	if out, err := runSSH(client, start); err != nil {
		return fmt.Errorf("start host-runner: %w: %s", err, out)
	}

	log.Printf("BootstrapRunner: host-runner launched on %s for run %s", addr, req.ExecutionRunID)
	return nil
}

// pushRuntime streams the named Execution Pack onto the target and extracts it
// under /opt/praetor/packs/<pack>. It probes the host's CPU arch so it pushes the
// matching pack (<pack>-linux-<arch>.tar.gz); if it's already present it does
// nothing. Packs are name-scoped so several coexist on one host.
func (r *BootstrapRunner) pushRuntime(client *ssh.Client, pack string) error {
	detect, err := runSSH(client, fmt.Sprintf(`if [ -x /opt/praetor/packs/%s/bin/ansible-playbook ]; then echo present; else
  case "$(uname -m)" in aarch64|arm64) echo arm64 ;; x86_64|amd64) echo amd64 ;; *) echo unknown ;; esac
fi`, pack))
	if err != nil {
		return err
	}
	arch := strings.TrimSpace(detect)
	if arch == "present" {
		return nil
	}
	if arch == "unknown" || arch == "" {
		return fmt.Errorf("unsupported host CPU arch for Execution Pack")
	}
	tarball := fmt.Sprintf("%s/%s-linux-%s.tar.gz", r.RuntimeDir, pack, arch)
	f, err := os.Open(tarball)
	if err != nil {
		return fmt.Errorf("open Execution Pack %q for %s: %w", pack, arch, err)
	}
	defer f.Close()
	log.Printf("BootstrapRunner: pushing Execution Pack %q (%s)", pack, arch)
	return pushStream(client, f, "mkdir -p /opt/praetor/packs && tar -xzf - -C /")
}

// localBootstrap runs the host-runner on the executor itself for jobs with no
// inventory (localhost execution).
func (r *BootstrapRunner) localBootstrap(req *events.ExecutionRequest, manifestBytes []byte, callbackURL string) error {
	runID := req.ExecutionRunID.String()
	jobDir := "/var/lib/praetor/jobs/" + runID
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(jobDir+"/manifest.json", manifestBytes, 0644); err != nil {
		return err
	}
	log.Printf("BootstrapRunner: no runner host — executing locally for run %s", req.ExecutionRunID)
	cmd := exec.Command(r.RunnerPayloadPath,
		"--job-dir="+jobDir, "--api-url="+callbackURL, "--run-id="+runID)
	logFile, _ := os.OpenFile(jobDir+"/runner.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		cmd.Stdout, cmd.Stderr = logFile, logFile
	}
	return cmd.Start() // detached; do not wait
}

// --- SSH helpers ---

func dialSSH(addr, port, user, keyPath string) (*ssh.Client, error) {
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	return ssh.Dial("tcp", net.JoinHostPort(addr, port), cfg)
}

// runSSH runs a command on the target and returns its combined output.
func runSSH(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}

// pushStream feeds r to a remote command's stdin (e.g. `cat > file` or
// `tar -xzf - -C /`), the primitive for copying files over SSH without Python.
func pushStream(client *ssh.Client, r io.Reader, remoteCmd string) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.Stdin = r
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run(remoteCmd); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func pushFile(client *ssh.Client, local, remote, mode string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	return pushStream(client, f, writeCmd(remote, mode))
}

func pushBytes(client *ssh.Client, data []byte, remote, mode string) error {
	return pushStream(client, bytes.NewReader(data), writeCmd(remote, mode))
}

func writeCmd(remote, mode string) string {
	return fmt.Sprintf("mkdir -p %s && cat > %s && chmod %s %s",
		sshQuote(path.Dir(remote)), sshQuote(remote), mode, sshQuote(remote))
}

func sshQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseHostVars pulls a host's inventory-line vars (ansible_host/port/user, ...)
// out of the generated INI, so the executor can reach it over SSH directly.
func parseHostVars(inventory, host string) map[string]string {
	vars := map[string]string{}
	if host == "" {
		return vars
	}
	for _, line := range strings.Split(inventory, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != host {
			continue
		}
		for _, f := range fields[1:] {
			if i := strings.Index(f, "="); i > 0 {
				vars[f[:i]] = strings.Trim(f[i+1:], `"'`)
			}
		}
		return vars
	}
	return vars
}

const resumeUnitScript = `if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/praetor-resume.service <<'UNIT'
[Unit]
Description=Praetor host-runner - resume interrupted jobs after a restart
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
fi`
