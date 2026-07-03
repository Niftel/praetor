# Gitea for Praetor's infra artifact registry (host-runner releases + mirrored
# Python/PyPI packages the Execution Pack build pulls from).
#
# Reproducible replacement for the previously hand-built container: a pinned Gitea
# binary on Debian. All state (config, sqlite DB, repos, packages) lives on the
# mounted `gitea-data` volume at /var/lib/gitea — the image is stateless.
FROM debian:12-slim

ARG GITEA_VERSION=1.22.6
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git curl \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -m -u 1000 -s /bin/bash git \
 && arch="$(dpkg --print-architecture)" \
 && curl -fsSL "https://dl.gitea.com/gitea/${GITEA_VERSION}/gitea-${GITEA_VERSION}-linux-${arch}" -o /usr/local/bin/gitea \
 && chmod 0755 /usr/local/bin/gitea

ENV GITEA_WORK_DIR=/var/lib/gitea
USER git
EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/gitea", "web", "--config", "/var/lib/gitea/custom/conf/app.ini"]
