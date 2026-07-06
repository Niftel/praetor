#!/bin/sh
set -e

# Copy keys if they exist in /tmp/keys
if [ -d "/tmp/keys" ]; then
    echo "Importing SSH keys from legacy mount or /tmp/keys..."
    
    # Handle private key
    if [ -f "/tmp/keys/id_rsa" ]; then
        cp /tmp/keys/id_rsa /home/praetor/.ssh/id_rsa
        chmod 600 /home/praetor/.ssh/id_rsa
    fi

    # Handle public key
    if [ -f "/tmp/keys/id_rsa.pub" ]; then
        cp /tmp/keys/id_rsa.pub /home/praetor/.ssh/id_rsa.pub
        chmod 644 /home/praetor/.ssh/id_rsa.pub
    fi
fi

# Ensure ownership is correct
chown -R praetor:praetor /home/praetor/.ssh

# SSH host-key policy: trust-on-first-use. accept-new records an unknown host's
# key on first connect but REJECTS a changed key thereafter (MITM protection),
# and known_hosts lives on the persisted praetor-ssh volume so trust survives
# restarts. (Previously StrictHostKeyChecking=no + UserKnownHostsFile=/dev/null
# disabled verification entirely, defeating that volume.)
cat <<EOF > /home/praetor/.ssh/config
Host *
    StrictHostKeyChecking accept-new
    LogLevel ERROR
EOF
chmod 600 /home/praetor/.ssh/config
chown praetor:praetor /home/praetor/.ssh/config

# Localhost jobs run the host-runner on the executor itself: it writes job dirs
# under /var/lib/praetor and extracts the Execution Pack under /opt/praetor. Make
# both writable by the runtime user (the process runs as praetor via gosu).
mkdir -p /var/lib/praetor /opt/praetor
chown praetor:praetor /var/lib/praetor /opt/praetor

# Drop privileges and exec
exec gosu praetor "$@"
