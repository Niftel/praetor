package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/praetordev/praetor/pkg/events"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSH host-key verification uses trust-on-first-use: the first time we connect to a
// host we record its key in the known_hosts file and trust it; on later connects a
// changed key is refused (possible MITM). Persist SSH_KNOWN_HOSTS on a volume to
// keep trust across executor restarts.
var (
	hostKeyOnce sync.Once
	hostKeyCB   ssh.HostKeyCallback
	hostKeyMu   sync.Mutex
	knownHosts  ssh.HostKeyCallback
	knownPath   string
)

func getHostKeyCallback() ssh.HostKeyCallback {
	hostKeyOnce.Do(func() {
		knownPath = os.Getenv("SSH_KNOWN_HOSTS")
		if knownPath == "" {
			// Default under $HOME/.ssh, which the runtime user owns (the executor
			// drops to a non-root user via gosu); /var/lib/praetor is root-owned.
			home := os.Getenv("HOME")
			if home == "" {
				home = "/var/lib/praetor"
			}
			knownPath = filepath.Join(home, ".ssh", "known_hosts")
		}
		_ = os.MkdirAll(filepath.Dir(knownPath), 0o700)
		if f, err := os.OpenFile(knownPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_ = f.Close()
		} else {
			log.Printf("BootstrapRunner: cannot open known_hosts %s: %v", knownPath, err)
		}
		knownHosts, _ = knownhosts.New(knownPath)
		hostKeyCB = tofuHostKey
	})
	return hostKeyCB
}

// tofuHostKey verifies against known_hosts, trusts an unknown host on first use,
// and refuses a host whose key changed.
func tofuHostKey(hostname string, remote net.Addr, key ssh.PublicKey) error {
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	if knownHosts != nil {
		err := knownHosts(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) > 0 {
				return fmt.Errorf("ssh host key changed for %s — refusing (possible MITM); remove the stale entry from %s to re-trust", hostname, knownPath)
			}
			// empty Want == host not yet known: fall through to trust-on-first-use.
		} else {
			return err
		}
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	if f, err := os.OpenFile(knownPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		_, _ = fmt.Fprintln(f, line)
		_ = f.Close()
	} else {
		log.Printf("BootstrapRunner: failed to record host key for %s in %s: %v", hostname, knownPath, err)
	}
	knownHosts, _ = knownhosts.New(knownPath) // reload so a later key change is detected
	log.Printf("BootstrapRunner: trusted new host key for %s (trust-on-first-use)", hostname)
	return nil
}

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
	// Connection identity comes from the job's Machine credential (AAP-style): the
	// SSH user is the credential's username (ANSIBLE_REMOTE_USER) unless the host
	// pins its own ansible_user. Praetor ships no shared automation key or default
	// user — the operator owns the login account and its authorized_keys on the
	// host, and references the matching private key through a Machine credential.
	user := firstNonEmpty(vars["ansible_user"], req.JobManifest.CredentialEnv["ANSIBLE_REMOTE_USER"])
	if user == "" {
		return fmt.Errorf("no SSH user for runner host %s: assign a Machine credential (with a username) to the job template, or set ansible_user on the host", req.JobManifest.RunnerHost)
	}
	// When we log in as a non-root user, privileged bootstrap steps run via sudo.
	// This assumes the login user can escalate without a password; privilege
	// escalation for the play itself is configured on the credential (become).
	sudo := ""
	if user != "root" {
		sudo = "sudo "
	}

	// SSH key: strictly the Machine credential's key — no shared/platform fallback.
	keyContent := req.JobManifest.CredentialFiles["ANSIBLE_PRIVATE_KEY_FILE"]
	if keyContent == "" {
		return fmt.Errorf("no SSH key for runner host %s: assign a Machine credential with an SSH private key to the job template", req.JobManifest.RunnerHost)
	}
	if !strings.HasSuffix(keyContent, "\n") {
		keyContent += "\n" // SSH rejects a key file without a trailing newline
	}
	sshKeyPath := fmt.Sprintf("/tmp/cred-key-%s", req.ExecutionRunID)
	if err := os.WriteFile(sshKeyPath, []byte(keyContent), 0o600); err != nil {
		return fmt.Errorf("failed to write credential key: %w", err)
	}
	defer os.Remove(sshKeyPath)

	client, err := dialSSH(addr, port, user, sshKeyPath)
	if err != nil {
		return fmt.Errorf("ssh to runner host %s@%s:%s: %w", user, addr, port, err)
	}
	defer client.Close()
	log.Printf("BootstrapRunner: connected to %s@%s:%s for run %s", user, addr, port, req.ExecutionRunID)

	runID := req.ExecutionRunID.String()
	jobDir := "/var/lib/praetor/jobs/" + runID

	// 1. Directories.
	if out, err := runSSH(client, fmt.Sprintf("%smkdir -p %s /opt/praetor /usr/local/share/praetor/plugins/callback", sudo, sshQuote(jobDir))); err != nil {
		return fmt.Errorf("mkdir on target: %w: %s", err, out)
	}

	// 2. The host-runner binary.
	if err := pushFile(client, r.RunnerPayloadPath, "/usr/local/bin/praetor-host-runner", "0755", sudo); err != nil {
		return fmt.Errorf("push host-runner: %w", err)
	}

	// 3. The checkpoint callback plugin (task-level resume).
	if err := pushFile(client, "/tmp/plugins/callback/praetor_checkpoint.py", "/usr/local/share/praetor/plugins/callback/praetor_checkpoint.py", "0644", sudo); err != nil {
		log.Printf("BootstrapRunner: checkpoint plugin push failed (non-fatal): %v", err)
	}

	// 4. The Execution Pack — the self-contained Ansible runtime — so the host
	// needs no Ansible/Python. Pushes the pack the job selected (or the default).
	pack := firstNonEmpty(req.JobManifest.ExecutionPack, "ansible-runtime")
	if err := r.pushRuntime(client, pack, sudo); err != nil {
		log.Printf("BootstrapRunner: pack push failed (non-fatal; will fall back to system ansible): %v", err)
	}

	// 5. The manifest.
	if err := pushBytes(client, manifestBytes, jobDir+"/manifest.json", "0644", sudo); err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}

	// 6. The SSH key the host-runner uses for its downstream plays.
	if err := pushFile(client, sshKeyPath, jobDir+"/id_rsa", "0600", sudo); err != nil {
		return fmt.Errorf("push job key: %w", err)
	}

	// 7. The resume systemd unit (best-effort; skipped where systemd is absent).
	if out, err := runShellScript(client, sudo, resumeUnitScript); err != nil {
		log.Printf("BootstrapRunner: resume unit install skipped: %v: %s", err, out)
	}

	// 8. Launch the host-runner as root, detached so it outlives this SSH session.
	// Running the whole line under `sudo sh` keeps the log redirection root-owned.
	start := fmt.Sprintf(
		"setsid /usr/local/bin/praetor-host-runner --job-dir=%s --api-url=%s --run-id=%s >> %s/runner.log 2>&1 </dev/null &",
		jobDir, callbackURL, runID, jobDir,
	)
	if out, err := runShellScript(client, sudo, start); err != nil {
		return fmt.Errorf("start host-runner: %w: %s", err, out)
	}

	log.Printf("BootstrapRunner: host-runner launched on %s for run %s", addr, req.ExecutionRunID)
	return nil
}

// pushRuntime streams the named Execution Pack onto the target and extracts it
// under /opt/praetor/packs/<pack>. It probes the host's CPU arch so it pushes the
// matching pack (<pack>-linux-<arch>.tar.gz); if it's already present it does
// nothing. Packs are name-scoped so several coexist on one host.
func (r *BootstrapRunner) pushRuntime(client *ssh.Client, pack, sudo string) error {
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
	return pushStream(client, f, fmt.Sprintf("%smkdir -p /opt/praetor/packs && %star -xzf - -C /", sudo, sudo))
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
		HostKeyCallback: getHostKeyCallback(),
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

func pushFile(client *ssh.Client, local, remote, mode, sudo string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	return pushStream(client, f, writeCmd(remote, mode, sudo))
}

func pushBytes(client *ssh.Client, data []byte, remote, mode, sudo string) error {
	return pushStream(client, bytes.NewReader(data), writeCmd(remote, mode, sudo))
}

// writeCmd builds the remote command that receives a streamed file on stdin. As
// root it's `cat > file`; as a non-root user it uses `sudo tee` per step so the
// file is written with root privileges (no nested-quote gymnastics).
func writeCmd(remote, mode, sudo string) string {
	if sudo != "" {
		return fmt.Sprintf("%smkdir -p %s && %stee %s > /dev/null && %schmod %s %s",
			sudo, sshQuote(path.Dir(remote)), sudo, sshQuote(remote), sudo, mode, sshQuote(remote))
	}
	return fmt.Sprintf("mkdir -p %s && cat > %s && chmod %s %s",
		sshQuote(path.Dir(remote)), sshQuote(remote), mode, sshQuote(remote))
}

// runShellScript pipes a script to `sh` (or `sudo sh`) on stdin, avoiding quoting
// problems for multi-line scripts (systemd unit) and the detached launch line.
func runShellScript(client *ssh.Client, sudo, script string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	sess.Stdin = strings.NewReader(script)
	shell := "sh"
	if sudo != "" {
		shell = "sudo sh"
	}
	out, err := sess.CombinedOutput(shell)
	return string(out), err
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
