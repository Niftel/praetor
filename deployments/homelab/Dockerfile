# A RHEL 9-compatible managed node for the Praetor home lab: Rocky Linux 9 running
# systemd as PID 1 (so the systemd/service Ansible modules work like a real box),
# with sshd the only exposed surface. Praetor logs in as root over SSH using the
# pushed-pack key and bootstraps the self-contained Execution Pack — so this image
# deliberately does NOT install Ansible or Python; the pack brings its own.
FROM rockylinux:9

RUN dnf -y install openssh-server sudo procps-ng tar gzip iproute git && \
    dnf clean all && rm -rf /var/cache/dnf && \
    systemctl enable sshd && \
    # Key-only root login, matching the sandbox target.
    sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config

# Stable, pre-generated SSH host keys (gitignored, made by up.sh) so recreating or
# rebuilding a node keeps the same host identity — otherwise the executor pins the
# old key in known_hosts and refuses the new one as a possible MITM.
COPY hostkeys/ssh_host_ed25519_key hostkeys/ssh_host_rsa_key hostkeys/ssh_host_ecdsa_key /etc/ssh/
COPY hostkeys/ssh_host_ed25519_key.pub hostkeys/ssh_host_rsa_key.pub hostkeys/ssh_host_ecdsa_key.pub /etc/ssh/
RUN chmod 600 /etc/ssh/ssh_host_*_key && chmod 644 /etc/ssh/ssh_host_*_key.pub

RUN mkdir -p /root/.ssh && chmod 700 /root/.ssh
COPY authorized_keys /root/.ssh/authorized_keys
RUN chmod 600 /root/.ssh/authorized_keys

EXPOSE 22
# systemd as init; run the container with --privileged so it can manage cgroups.
CMD ["/sbin/init"]
