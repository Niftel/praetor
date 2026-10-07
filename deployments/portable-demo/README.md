# Praetor portable demo

This bundle runs the compatible Praetor component set on a laptop using pinned
container images. It does not require source repositories, Go, Node.js, Helm, or
Kubernetes.

## Requirements

- Docker Desktop 4.x (or Docker Engine with Compose v2)
- 8 GB RAM available to Docker; 12 GB recommended
- Approximately 8 GB free disk space
- macOS/Linux launch: `curl` and `openssl` (included with current macOS and most
  Linux distributions)
- Access to `ghcr.io/niftel` (run `docker login ghcr.io` first if the packages
  are private), unless this is an offline bundle containing `images.tar`

## Start

macOS/Linux:

```sh
./start.sh
```

Windows PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\start.ps1
```

Open <http://localhost:3000>. The generated administrator credentials are
printed at startup and stored only in the local `.env` file.

## Operate

```sh
./logs.sh       # follow service logs
./stop.sh       # stop containers and preserve demo data
docker compose ps
docker compose up -d
```

On PowerShell, the equivalent stop command is `docker compose down`.

To permanently reset the demo, run `docker compose down --volumes` and delete
`.env`, then start again. This destroys every job, credential, and configuration
created in the demo.

## Security boundary

This is a local evaluation package, not a production deployment. Published
ports bind to `127.0.0.1`; secrets are randomly generated on first launch; and
insecure built-in defaults are disabled. Do not email or commit the generated
`.env` file. Use the Helm deployment and an external secret manager for a real
environment.

The bundle includes the core control and execution services. Source-build and
development-only infrastructure (Gitea, BuildKit, documentation, metrics, and
sample target hosts) is deliberately excluded.

An offline bundle is specific to its target CPU (`linux/amd64` for most Intel
and AMD work laptops, or `linux/arm64` for Windows-on-ARM and Apple Silicon). It
contains the image archive and needs no registry login after it is created.

Maintainers create an offline handoff with:

```sh
make portable-demo-offline PLATFORM=linux/amd64
```
