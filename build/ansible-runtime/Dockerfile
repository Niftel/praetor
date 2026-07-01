# Produces a relocatable, self-contained Ansible runtime tarball that Praetor
# pushes onto target hosts — so a host needs NO Ansible and NO Python installed.
#
# The runtime is a standalone CPython (python-build-standalone) with Ansible
# pip-installed into it, laid out at a FIXED prefix (/opt/praetor/runtime) so the
# console-script shebangs (e.g. bin/ansible-playbook -> bin/python3) stay valid
# when the tarball is extracted to the same prefix on the target.
#
# Build (per arch) and export the tarball to ./build/runtime/:
#   docker buildx build --build-arg TARGETARCH=arm64 \
#     -f build/ansible-runtime/Dockerfile \
#     --target export -o type=local,dest=build/runtime .
#
# glibc build: works on debian/ubuntu/rhel/etc. (musl/alpine needs a musl variant).
FROM debian:12-slim AS build

ARG PY_VERSION=3.11.9
ARG PBS_TAG=20240814
ARG TARGETARCH
ARG PREFIX=/opt/praetor/runtime

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
    mkdir -p /opt/praetor; \
    curl -fsSL "$url" -o /tmp/py.tgz; \
    tar -xzf /tmp/py.tgz -C /opt/praetor; \
    mv /opt/praetor/python "$PREFIX"; \
    "$PREFIX/bin/python3" --version

# Install the full Ansible into the standalone interpreter. The `ansible` package
# bundles ansible-core plus the community collections into site-packages, where
# Ansible auto-discovers them — a complete, self-contained environment.
RUN set -eux; \
    "$PREFIX/bin/pip" install --no-cache-dir --upgrade pip; \
    "$PREFIX/bin/pip" install --no-cache-dir ansible; \
    "$PREFIX/bin/ansible-playbook" --version

# Trim caches to keep the tarball smaller.
RUN find "$PREFIX" -type d -name __pycache__ -prune -exec rm -rf {} + ; \
    rm -rf "$PREFIX/lib/python"*/test || true

RUN mkdir -p /out && tar -czf "/out/ansible-runtime-linux-${TARGETARCH}.tar.gz" -C / opt/praetor/runtime \
 && ls -lh /out

# Export stage: `-o type=local` copies just the tarball out of the build.
FROM scratch AS export
COPY --from=build /out/ /
