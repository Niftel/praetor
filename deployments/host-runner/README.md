# Host-runner job resume

When a job is running and the machine executing it restarts, the job should be
picked back up rather than abandoned. The `praetor-host-runner` keeps all
durable state for a job in its directory under `/var/lib/praetor/jobs/<run_id>/`
(manifest, WAL, stdout, sync cursors), so a restart only needs two things:

1. **Persistent storage** for `/var/lib/praetor` so the job directories survive
   the restart.
2. **A boot hook** that runs the resume scan once the machine is back up.

The resume scan re-runs any job whose `status.json` is absent or non-terminal:

```
praetor-host-runner --resume-root=/var/lib/praetor/jobs
```

Resume re-runs the playbook (Ansible cannot checkpoint mid-play; idempotent
plays converge). WAL events and log chunks are deduplicated downstream by
`(execution_run_id, seq)`, and the host's log cursor continues where it left
off, so a resume never double-counts or loses output.

## Production hosts (systemd)

```bash
# 1. The binary is installed at bootstrap time (or copy it yourself):
#    /usr/local/bin/praetor-host-runner
# 2. Ensure the job directory is on persistent storage (a real disk, not tmpfs).
# 3. Install and enable the boot unit:
sudo cp deployments/host-runner/praetor-resume.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable praetor-resume.service
```

On the next boot, `praetor-resume.service` resumes every unfinished job.

## Demo (docker compose, Alpine target hosts)

Alpine has no systemd, so wire it manually. Give each target host a persistent
volume for `/var/lib/praetor` and run the scan on container start. Example
override for a target host service:

```yaml
services:
  web1:
    volumes:
      - web1-praetor:/var/lib/praetor
    command: >
      /bin/sh -c "apk add --no-cache openssh python3 sudo ansible git && ...setup... &&
        ( [ -x /usr/local/bin/praetor-host-runner ] &&
          /usr/local/bin/praetor-host-runner --resume-root=/var/lib/praetor/jobs
          >> /var/lib/praetor/resume.log 2>&1 || true ) &&
        /usr/sbin/sshd -D"

volumes:
  web1-praetor:
```

Then `docker restart web1` mid-job re-runs the in-flight job on startup.

## Caveats

- If the job directory itself is lost (no persistent storage), the job cannot be
  resumed; the control plane will mark the run `lost` once heartbeats stop (the
  heartbeat-aware reconciler).
- The scan resumes jobs sequentially. For hosts that run many concurrent jobs,
  this serializes recovery.
