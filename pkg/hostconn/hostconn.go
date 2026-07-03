// Package hostconn holds the SSH connection primitives shared by the executor
// (which bootstraps the host-runner onto a target) and the reconciler (which
// SSHes back to the same host to harvest its WAL). Centralizing them keeps one
// trust-on-first-use host-key policy and one inventory-var parser across both.
package hostconn

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSH host-key verification uses trust-on-first-use: the first time we connect to
// a host we record its key and trust it; on later connects a changed key is
// refused (possible MITM). Persist SSH_KNOWN_HOSTS on a volume to keep trust
// across restarts and across the executor/reconciler.
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
			// Default under $HOME/.ssh, which the runtime user owns (services drop
			// to a non-root user via gosu); /var/lib/praetor is root-owned.
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
			log.Printf("hostconn: cannot open known_hosts %s: %v", knownPath, err)
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
		log.Printf("hostconn: failed to record host key for %s in %s: %v", hostname, knownPath, err)
	}
	knownHosts, _ = knownhosts.New(knownPath) // reload so a later key change is detected
	log.Printf("hostconn: trusted new host key for %s (trust-on-first-use)", hostname)
	return nil
}

// Dial opens an SSH connection to addr:port as user, authenticating with the
// given PEM private key. Host-key verification is trust-on-first-use.
func Dial(addr, port, user string, keyPEM []byte) (*ssh.Client, error) {
	signer, err := ssh.ParsePrivateKey(keyPEM)
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

// Run runs a command on the target and returns its combined output.
func Run(client *ssh.Client, cmd string) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}

// ParseHostVars pulls a host's inventory-line vars (ansible_host/port/user, ...)
// out of the generated INI, so a caller can reach it over SSH directly.
func ParseHostVars(inventory, host string) map[string]string {
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

// FirstNonEmpty returns the first non-empty string among vals, or "".
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Quote single-quotes a string for safe use in a remote shell command.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
