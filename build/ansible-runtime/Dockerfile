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
# Spec-driven contents (no inline comments — Docker treats them as part of the value):
# ANSIBLE_SPEC: pip requirement, e.g. ansible or ansible-core==2.16.*
ARG ANSIBLE_SPEC=ansible
# EXTRA_PIP: space-separated extra pip packages (module deps)
ARG EXTRA_PIP=
# GALAXY_COLLECTIONS: space-separated ansible-galaxy collections
ARG GALAXY_COLLECTIONS=
# Gitea mirror the pack pulls Python + wheels from (reproducible / air-gapped;
# no GitHub or PyPI at build time). Reachable from the build via the packbuilder's
# --network=host + --add-host gitea-host:<ip>.
ARG GITEA_URL=http://gitea-host:3000
ARG GITEA_OWNER=praetor
# HOST_RUNNER_VERSION: the host-runner daemon release (in Gitea) to bundle, so the
# pack is the single self-contained bootstrapping unit (daemon + Python + Ansible).
ARG HOST_RUNNER_VERSION=v0.1.0

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
    url="${GITEA_URL}/${GITEA_OWNER}/host-runner/releases/download/${HOST_RUNNER_VERSION}/praetor-host-runner-linux-${TARGETARCH}"; \
    echo "Downloading $url"; \
    curl -fsSL "$url" -o "$PREFIX/bin/praetor-host-runner"; \
    chmod 0755 "$PREFIX/bin/praetor-host-runner"; \
    test -s "$PREFIX/bin/praetor-host-runner"

# Install Ansible + any extra pip packages from the Gitea PyPI mirror only
# (mirrored by scripts/mirror-pip.sh) — no PyPI at build time.
RUN set -eux; \
    host="$(printf '%s' "${GITEA_URL}" | sed -E 's#^https?://([^:/]+).*#\1#')"; \
    "$PREFIX/bin/pip" install --no-cache-dir \
      --index-url "${GITEA_URL}/api/packages/${GITEA_OWNER}/pypi/simple/" \
      --trusted-host "$host" \
      ${ANSIBLE_SPEC} ${EXTRA_PIP}; \
    "$PREFIX/bin/ansible-playbook" --version

# Install any extra Galaxy collections into site-packages/ansible_collections,
# where Ansible auto-discovers them (no runtime env needed).
RUN set -eux; \
    if [ -n "${GALAXY_COLLECTIONS}" ]; then \
      SITE="$("$PREFIX/bin/python3" -c 'import sysconfig;print(sysconfig.get_paths()["purelib"])')"; \
      "$PREFIX/bin/ansible-galaxy" collection install ${GALAXY_COLLECTIONS} -p "$SITE"; \
    fi

# Trim caches to keep the tarball smaller.
RUN find "$PREFIX" -type d -name __pycache__ -prune -exec rm -rf {} + ; \
    rm -rf "$PREFIX/lib/python"*/test || true

RUN mkdir -p /out && tar -czf "/out/${PACK_NAME}-linux-${TARGETARCH}.tar.gz" -C / "${PREFIX#/}" \
 && ls -lh /out

# Export stage: `-o type=local` copies just the tarball out of the build.
FROM scratch AS export
COPY --from=build /out/ /
