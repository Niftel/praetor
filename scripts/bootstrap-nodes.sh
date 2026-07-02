#!/usr/bin/env bash
# bootstrap-nodes.sh — prepare cluster hosts for Praetor/Ansible automation.
#
# Creates the automation user, installs the automation PUBLIC key, and grants
# passwordless sudo on each node. This is the host-side setup the operator owns;
# Praetor then connects with a Machine credential holding the matching PRIVATE
# key (username + become_method=sudo).
#
# Two connection modes for the bootstrap step:
#   ssh    (default) — log in to each node with an existing privileged account.
#   docker (--docker) — run against local containers via `docker exec`; no SSH
#                        ports or admin login needed for the setup itself. The
#                        public key is still installed so Praetor can SSH later.
#
# Idempotent: safe to re-run to add nodes or re-apply.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: ./scripts/bootstrap-nodes.sh -u <user> [options] host1 [host2 ...]

  -u, --user USER     automation user to create on each node (required)
  -k, --key PUBKEY    automation PUBLIC key to install (default: ~/.ssh/id_ed25519.pub)
  -a, --admin USER    ssh mode: existing privileged login to bootstrap with (default: root)
  -d, --docker        docker mode: reach nodes via `docker exec` (host = container name/ID)
  -h, --help          show this help

examples:
  ./scripts/bootstrap-nodes.sh -u ansible -a root node1 node2 node3
  ./scripts/bootstrap-nodes.sh -u ansible --docker $(docker ps --format '{{.Names}}')
USAGE
  exit "${1:-0}"
}

AUTOMATION_USER=""
PUBKEY="$HOME/.ssh/id_ed25519.pub"   # automation PUBLIC key to install
ADMIN_USER="root"                    # ssh mode: existing privileged login
MODE="ssh"                           # ssh | docker
NODES=()

while [ $# -gt 0 ]; do
  case "$1" in
    -u|--user)  AUTOMATION_USER="${2:?--user needs a value}"; shift 2 ;;
    -k|--key)   PUBKEY="${2:?--key needs a value}"; shift 2 ;;
    -a|--admin) ADMIN_USER="${2:?--admin needs a value}"; shift 2 ;;
    -d|--docker) MODE="docker"; shift ;;
    -h|--help)  usage 0 ;;
    --)         shift; NODES+=("$@"); break ;;
    -*)         echo "unknown option: $1" >&2; usage 1 ;;
    *)          NODES+=("$1"); shift ;;
  esac
done

[ -n "$AUTOMATION_USER" ] || { echo "error: --user is required" >&2; usage 1; }
[ ${#NODES[@]} -gt 0 ]    || { echo "error: at least one host is required" >&2; usage 1; }
[ -f "$PUBKEY" ]          || { echo "public key not found: $PUBKEY" >&2; exit 1; }
[ "$MODE" != docker ] || command -v docker >/dev/null || { echo "error: docker not found" >&2; exit 1; }
KEY_CONTENT="$(cat "$PUBKEY")"

# The host-side payload. Portable across bash and busybox sh (Alpine), so it
# runs the same whether delivered over SSH or via `docker exec`. Reads
# AUTOMATION_USER and KEY from the environment.
PAYLOAD="$(cat <<'REMOTE'
set -eu
SUDO=""; [ "$(id -u)" -eq 0 ] || SUDO="sudo"
# 1. create the user if missing (RHEL useradd / busybox adduser / Debian adduser)
if ! id "$AUTOMATION_USER" >/dev/null 2>&1; then
  $SUDO useradd -m -s /bin/sh "$AUTOMATION_USER" 2>/dev/null \
  || $SUDO adduser -D -s /bin/sh "$AUTOMATION_USER" 2>/dev/null \
  || $SUDO adduser --disabled-password --gecos "" "$AUTOMATION_USER"
fi
HOME_DIR="$(eval echo "~$AUTOMATION_USER")"
# 2. install the public key (idempotent)
$SUDO install -d -m 700 "$HOME_DIR/.ssh"
$SUDO touch "$HOME_DIR/.ssh/authorized_keys"
if ! $SUDO grep -qxF "$KEY" "$HOME_DIR/.ssh/authorized_keys"; then
  echo "$KEY" | $SUDO tee -a "$HOME_DIR/.ssh/authorized_keys" >/dev/null
fi
$SUDO chmod 600 "$HOME_DIR/.ssh/authorized_keys"
$SUDO chown -R "$AUTOMATION_USER:$AUTOMATION_USER" "$HOME_DIR/.ssh"
# 3. passwordless sudo for become (create the drop-in dir if the image lacks it;
#    the rule is inert until the `sudo` package is installed on that host)
$SUDO install -d -m 755 /etc/sudoers.d
printf '%s ALL=(ALL) NOPASSWD: ALL\n' "$AUTOMATION_USER" | $SUDO tee "/etc/sudoers.d/$AUTOMATION_USER" >/dev/null
$SUDO chmod 440 "/etc/sudoers.d/$AUTOMATION_USER"
echo "  done"
REMOTE
)"

failed=()
for node in "${NODES[@]}"; do
  echo "==> provisioning $node ($MODE)"
  if [ "$MODE" = docker ]; then
    docker exec -i -u 0 \
      -e AUTOMATION_USER="$AUTOMATION_USER" -e KEY="$KEY_CONTENT" \
      "$node" sh -s <<<"$PAYLOAD" || { echo "  FAILED: $node" >&2; failed+=("$node"); }
  else
    ssh -o StrictHostKeyChecking=accept-new "${ADMIN_USER}@${node}" \
      "AUTOMATION_USER='${AUTOMATION_USER}' KEY='${KEY_CONTENT}' bash -s" <<<"$PAYLOAD" \
      || { echo "  FAILED: $node" >&2; failed+=("$node"); }
  fi
done

echo "==> verifying user + sudo"
for node in "${NODES[@]}"; do
  if [ "$MODE" = docker ]; then
    docker exec -u "$AUTOMATION_USER" "$node" sh -c \
      'printf "%s: user %s ok; " "$(hostname)" "$(id -un)"; if sudo -n true 2>/dev/null; then echo "sudo ok"; else echo "sudo unavailable (install the sudo package)"; fi' \
      || echo "  verify failed: $node" >&2
  else
    ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
        -i "${PUBKEY%.pub}" "${AUTOMATION_USER}@${node}" \
        'printf "%s: ok as %s; " "$(hostname)" "$(whoami)"; sudo -n true && echo "sudo ok"' \
      || echo "  verify failed: $node" >&2
  fi
done

if [ ${#failed[@]} -gt 0 ]; then
  echo "==> ${#failed[@]} node(s) failed provisioning: ${failed[*]}" >&2
  exit 1
fi
