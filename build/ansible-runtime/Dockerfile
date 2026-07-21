# Builds an Execution Pack: a relocatable, self-contained Python + Ansible
# environment Praetor pushes onto a host, so the host needs NO Ansible or Python.
#
# Everything is driven by build-args so the pack can be defined declaratively via
# a YAML spec (see cmd/execpack + build/execpack/specs). Don't invoke this
# directly — use `make execpack SPEC=<file>` which fills the args from the spec.
#
# A standalone CPython (python-build-standalone) laid out at a FIXED prefix
# (/opt/praetor/packs/<name>) so console-script shebangs stay valid when extracted
# to the same prefix on the target. glibc build — covers all mainstream Linux
# (Debian/Ubuntu/RHEL/Amazon/SUSE...) on both amd64 and arm64.
FROM debian:12-slim AS build

ARG PY_VERSION=3.11.9
ARG PBS_TAG=20240814
ARG TARGETARCH
# PACK_NAME: output tarball base name; also name-scopes the on-host prefix so
# multiple packs coexist (each host-runner uses /opt/praetor/packs/<name>).
ARG PACK_NAME=execpack
ARG PREFIX=/opt/praetor/packs/${PACK_NAME}
# The pack's Python + Ansible deps come from a requirements.txt supplied in the
# build context (composed and validated by the packbuilder from the pack spec —
# see pkg/packspec). Nothing dependency-related is passed as a shell-interpolated
# build-arg, so a spec can't inject pip flags or shell metacharacters. Collections
# are NOT baked into the pack; they come from the project's requirements.yml at run
# time (see cmd/host-runner/galaxy.go).
# Gitea mirror the pack pulls Python + wheels from (reproducible / air-gapped;
# no GitHub or PyPI at build time). Reachable from the build via the packbuilder's
# --network=host + --add-host gitea-host:<ip>.
ARG GITEA_URL=http://gitea-host:3000
ARG GITEA_OWNER=praetor
# HOST_RUNNER_VERSION: the host-runner daemon release (in Gitea) to bundle, so the
# pack is the single self-contained bootstrapping unit (daemon + Python + Ansible).
# REQUIRED, no default: the version is single-sourced from the pack spec's
# `host_runner` field (the packbuilder passes it here). An unset value fails the
# download step below rather than silently bundling a stale release.
ARG HOST_RUNNER_VERSION

RUN apt-get update && apt-get install -y --no-install-recommends curl xz-utils ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Fetch the standalone CPython for the target arch from the Gitea generic registry
# (mirrored by scripts/mirror-python.sh) and lay it out at the fixed prefix.
RUN set -eux; \
    url="${GITEA_URL}/api/packages/${GITEA_OWNER}/generic/python-standalone/${PY_VERSION}+${PBS_TAG}/python-standalone-${PY_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    echo "Downloading $url"; \
    mkdir -p /opt/praetor "$(dirname "$PREFIX")"; \
    curl -fsSL "$url" -o /tmp/py.tgz; \
    tar -xzf /tmp/py.tgz -C /opt/praetor; \
    mv /opt/praetor/python "$PREFIX"; \
    "$PREFIX/bin/python3" --version

# Bundle the host-runner daemon (Praetor comms + WAL) from its Gitea release into
# the pack, so the pack is one self-contained bootstrapping unit.
RUN set -eux; \
    test -n "${HOST_RUNNER_VERSION}" || { echo "HOST_RUNNER_VERSION is required (set host_runner in the pack spec)" >&2; exit 1; }; \
    base="${GITEA_URL}/${GITEA_OWNER}/host-runner/releases/download/${HOST_RUNNER_VERSION}"; \
    file="praetor-host-runner-linux-${TARGETARCH}"; \
    echo "Downloading $base/$file"; \
    curl -fsSL "$base/$file" -o "$PREFIX/bin/praetor-host-runner"; \
    # Verify against the release's published SHA256SUMS — this binary runs on every
    # managed host, so an unverified download is a supply-chain risk.
    curl -fsSL "$base/SHA256SUMS" -o /tmp/host-runner.sha256sums; \
    expected="$(awk -v f="$file" '$2==f {print $1}' /tmp/host-runner.sha256sums)"; \
    test -n "$expected" || { echo "no SHA256SUMS entry for $file" >&2; exit 1; }; \
    echo "${expected}  ${PREFIX}/bin/praetor-host-runner" | sha256sum -c -; \
    chmod 0755 "$PREFIX/bin/praetor-host-runner"; \
    test -s "$PREFIX/bin/praetor-host-runner"

# The host-runner enables this callback to produce the allowlisted task/host
# diagnostics journal and durable checkpoint. Keep it inside the pack so every
# managed host receives the runner and its matching execution instrumentation.
COPY praetor_checkpoint.py ${PREFIX}/plugins/callback/praetor_checkpoint.py
RUN chmod 0644 "${PREFIX}/plugins/callback/praetor_checkpoint.py" \
 && test -s "${PREFIX}/plugins/callback/praetor_checkpoint.py"

# Install the engine + module deps from the Gitea PyPI mirror only (mirrored by
# scripts/mirror-pip.sh) — no public PyPI at build time. The requirements are read
# from the validated requirements.txt in the build context; `pip install -r` means
# no value is ever expanded by the shell.
COPY requirements.txt /tmp/requirements.txt
RUN set -eux; \
    host="$(printf '%s' "${GITEA_URL}" | sed -E 's#^https?://([^:/]+).*#\1#')"; \
    "$PREFIX/bin/pip" install --no-cache-dir \
      --index-url "${GITEA_URL}/api/packages/${GITEA_OWNER}/pypi/simple/" \
      --trusted-host "$host" \
      -r /tmp/requirements.txt; \
    "$PREFIX/bin/ansible-playbook" --version

# Trim caches to keep the tarball smaller.
RUN find "$PREFIX" -type d -name __pycache__ -prune -exec rm -rf {} + ; \
    rm -rf "$PREFIX/lib/python"*/test || true

RUN mkdir -p /out && tar -czf "/out/${PACK_NAME}-linux-${TARGETARCH}.tar.gz" -C / "${PREFIX#/}" \
 && ls -lh /out

# Export stage: `-o type=local` copies just the tarball out of the build.
FROM scratch AS export
COPY --from=build /out/ /
