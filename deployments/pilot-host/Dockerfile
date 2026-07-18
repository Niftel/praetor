FROM rockylinux:9@sha256:d7be1c094cc5845ee815d4632fe377514ee6ebcf8efaed6892889657e5ddaaa6

RUN dnf -y install openssh-server openssh-clients sudo procps-ng tar gzip iproute && \
    dnf clean all && rm -rf /var/cache/dnf && \
    useradd --create-home --shell /bin/bash praetor && \
    printf 'praetor ALL=(ALL) NOPASSWD: ALL\n' >/etc/sudoers.d/praetor && \
    chmod 0440 /etc/sudoers.d/praetor

COPY sshd_config /etc/ssh/sshd_config
COPY entrypoint.sh /usr/local/bin/praetor-pilot-entrypoint
RUN chmod 0755 /usr/local/bin/praetor-pilot-entrypoint

EXPOSE 22
ENTRYPOINT ["/usr/local/bin/praetor-pilot-entrypoint"]
