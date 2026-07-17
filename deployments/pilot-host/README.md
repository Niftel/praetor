# Disposable pilot managed host

This target proves real SSH automation without exposing a service to the host or
public network. It is a single hardened Rocky Linux 9 container on a dedicated
Docker bridge shared only with the persistent staging k3d nodes.

Security properties:

- immutable digest-pinned base image;
- no privileged mode, Docker socket, host filesystem, or published host port;
- non-root `praetor` SSH login with password and root login disabled;
- live provisioning checks prove the generated key authenticates as UID 1000,
  the pinned host key matches, and root authentication is rejected;
- a locally generated client key and stable host key below
  `~/.local/share/praetor/pilot-host/`, both outside Git;
- read-only root filesystem with bounded writable tmpfs mounts;
- reset removes only the disposable target, network attachments, image, and the
  pilot data directory. It never deletes a cluster, namespace, PVC, or staging
  data root.

Review and provision:

```sh
make pilot-host-plan
make pilot-host-provision
make pilot-host-status
```

The default target address is `172.29.50.10:22`. Provisioning rejects an
overlapping Docker subnet. Override the subnet and address together only when
required:

```sh
PRAETOR_PILOT_SUBNET=172.29.60.0/24 \
PRAETOR_PILOT_ADDRESS=172.29.60.10 \
  make pilot-host-provision
```

Explicitly destroy disposable pilot state:

```sh
make pilot-host-reset
```
