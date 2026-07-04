---
sidebar_position: 1
title: Execution Packs
---

# Execution Packs

An **Execution Pack** is the self-contained runtime Praetor pushes onto a host and runs the playbook from. It's the platform's defining idea: the Ansible engine travels **with** the job, so a managed host needs nothing pre-installed.

## What's inside

A pack is a relocatable bundle laid out at a **fixed prefix** — `/opt/praetor/packs/<name>` — containing:

- a standalone **CPython** (from python-build-standalone, glibc),
- **Ansible** + any pip dependencies and Galaxy collections you declared,
- the **host-runner** daemon (Praetor's on-host agent for this run).

The fixed prefix matters: console-script shebangs stay valid when the bundle is extracted to the same path on the target. Packs are **name-scoped**, so several can coexist on one host.

## How a job uses one

At launch the executor bootstraps entirely over SSH — no Ansible or Python needed on the target for delivery either:

1. Resolve the runner host's connection from its inventory vars + the job's [Machine credential](./credentials.md).
2. Probe the host arch (`uname -m`), pull the matching `‹pack›-linux-‹arch›.tar.gz` from Gitea's package registry, and stream it to the target piped into `tar -x` — **skipped if the pack is already present**.
3. Install the host-runner from the pack and launch it.
4. The host-runner runs the pack's `ansible-playbook`, pointing Ansible at the bundled interpreter (`ANSIBLE_PYTHON_INTERPRETER`) — nothing touches system paths.

**A target host needs only: `sshd`, a POSIX shell, `tar`, and a system `python3`** (Ansible modules execute with the host Python; the *engine* comes from the pack). Notably **no `git`**: a git-backed project is fetched as a `.tar.gz` archive over HTTP and unpacked by the host-runner in-process (it falls back to `git clone` only where git happens to be present, e.g. a non-Gitea URL). Deleting `/opt/praetor` removes everything.

:::note glibc only
The bundled CPython is glibc, covering all mainstream Linux (Debian/Ubuntu/RHEL/Amazon/SUSE…) on amd64 and arm64. musl hosts (Alpine) can't exec the pack's binaries and are out of scope.
:::

## Defining a pack

Packs are declarative. A YAML spec drives a parameterised build:

```yaml
name: docker-tools
python: "3.11.9"          # standalone CPython version
ansible_core: "2.19.11"   # OR  ansible: "12.3.0"  — set exactly one, a version
pip:                      # module deps only (validated, pin-able)
  - docker
  - jmespath
arches:
  - arm64
  - amd64
host_runner: v0.4.0       # daemon release bundled into the pack
```

The pack declares only the **engine**: a Python version, one of `ansible_core` / `ansible` (each a *version* — `ansible_core` installs `ansible-core==<v>`, `ansible` installs the full `ansible==<v>` bundle), and any pip module deps. **Collections are not part of a pack** — they come from the project's `requirements.yml`, installed at run time (see [Inventories & Templates](./inventories-and-templates.md)).

Every field is a typed value the builder composes into a `requirements.txt` (never a shell string), and each is validated against a strict pattern — so a spec can't smuggle pip flags (`--extra-index-url`) or shell metacharacters into the build, whether it's submitted via the API or pulled from a git repo.

Register a pack (with its spec) in the UI or API and the **packbuilder** service builds it: it runs the parameterised Dockerfile per arch (amd64 cross-builds under qemu), **publishes each tarball to Gitea's generic package registry**, and flips the pack `ready`/`failed` with a build log.

A template's `execution_pack_id` selects which pack a job uses; the default is `ansible-runtime`.

### Where packs live

Built packs are stored in **Gitea's generic package registry** under `execpack-‹pack›/current/‹pack›-linux-‹arch›.tar.gz` — the same artifact store the build already pulls Python and wheels from. The database row holds only metadata (spec, status, build log), never the bytes.

This is deliberate: the packbuilder *publishes* over HTTP and the executor *pulls* over HTTP, so the two services are decoupled from a shared filesystem — the executor doesn't need to sit on the same host as the builder. Publishing needs a Gitea `write:package` token (`PRAETOR_GITEA_TOKEN`); reads are anonymous. Without a token the builder falls back to writing the tarball into a shared `build/runtime/` dir, and the executor falls back to reading it there (used by the `execpack` CLI and pre-built packs).

:::note fixed `current` tag
Each rebuild does a delete-then-put on the immutable `current` version (Gitea's generic registry rejects overwriting a file). Packs are identified by *name*, and both push paths skip a host that already has the pack — so a rebuild lands on a host only at its next job, exactly as before. There's no per-build history yet; that would need a versioned tag threaded through the job manifest.
:::

## Reproducible & air-gapped builds

The build pulls **Python and wheels from your Gitea mirror**, not PyPI or GitHub:

```bash
make mirror-python    # standalone CPython -> Gitea generic registry
make mirror-pip       # ansible + wheel closure -> Gitea PyPI registry
```

The host-runner daemon is likewise pulled from a **Gitea release** (`praetor/host-runner`). So a pack build needs nothing from the public internet. See [Host-runner releases](../operations/host-runner-releases.md) for the daemon side.

## Git-backed packs (push-to-build)

A pack can point at a git repo + spec path with a webhook secret. A push to the repo fires the webhook, the packbuilder pulls the spec and rebuilds — the pack analogue of an inbound job webhook.
