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

RUN apt-get update && apt-get install -y --no-install-recommends curl xz-utils ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Fetch a standalone CPython for the target arch and lay it out at the fixed prefix.
RUN set -eux; \
    case "$TARGETARCH" in \
      arm64) PBS_ARCH=aarch64 ;; \
      amd64) PBS_ARCH=x86_64 ;; \
      *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    url="https://github.com/astral-sh/python-build-standalone/releases/download/${PBS_TAG}/cpython-${PY_VERSION}+${PBS_TAG}-${PBS_ARCH}-unknown-linux-gnu-install_only.tar.gz"; \
    echo "Downloading $url"; \
    mkdir -p /opt/praetor "$(dirname "$PREFIX")"; \
    curl -fsSL "$url" -o /tmp/py.tgz; \
    tar -xzf /tmp/py.tgz -C /opt/praetor; \
    mv /opt/praetor/python "$PREFIX"; \
    "$PREFIX/bin/python3" --version

# Install Ansible + any extra pip packages defined by the spec.
RUN set -eux; \
    "$PREFIX/bin/pip" install --no-cache-dir --upgrade pip; \
    "$PREFIX/bin/pip" install --no-cache-dir ${ANSIBLE_SPEC} ${EXTRA_PIP}; \
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
