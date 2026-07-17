#!/usr/bin/env bash
set -euo pipefail

install -d -m 0700 /home/praetor/.ssh
install -m 0600 /run/praetor/authorized_keys /home/praetor/.ssh/authorized_keys
chown -R praetor:praetor /home/praetor/.ssh
chown praetor:praetor /home/praetor
mkdir -p /run/sshd
exec /usr/sbin/sshd -D -e -f /etc/ssh/sshd_config
