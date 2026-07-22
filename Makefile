.PHONY: build compat-check contract-test deployment-contract-test local-deploy-contract-test workflow-lint verify-changed verify-changed-images secrets-execution-contract-test secrets-execution-e2e readiness-report-test gosec release-preflight release-preflight-remote release-plan workspace-health shared-module-health shared-module-health-remote host-runner release-host-runner mirror-python mirror-pip execpack test chaos-test clean run-api up up-demo down restart local-cluster-create local-cluster-status local-cluster-start local-cluster-stop local-cluster-recover local-cluster-update local-cluster-release staging-environment-plan staging-environment-provision staging-environment-status pilot-host-plan pilot-host-provision pilot-host-status pilot-host-reset staging-pilot-access-plan staging-pilot-access-seed staging-pilot-access-status staging-pilot-journey-plan staging-pilot-journey-seed staging-pilot-journey-status staging-pilot-journey-run staging-pilot-journey-faults staging-pilot-credential-faults staging-pilot-readiness

BINARY_DIR=bin
API_BINARY=$(BINARY_DIR)/praetor-api
GOSEC_VERSION ?= v2.28.0
GOSEC_REPORT ?= $(CURDIR)/.gosec/gosec.sarif
ACTIONLINT_VERSION ?= v1.7.12

# Host-runner cross-compilation target. The binary is bootstrapped onto your
# MANAGED hosts, so this is their CPU arch, not necessarily the build machine's.
# It defaults to the build machine's arch (so the local docker-compose demo,
# whose containers run the host arch, works out of the box); override for
# cross-arch targets, e.g. `make host-runner HOST_RUNNER_ARCH=amd64`.
HOST_RUNNER_OS ?= linux
HOST_RUNNER_ARCH ?= $(shell go env GOHOSTARCH)
HOST_RUNNER_BINARY=build/$(HOST_RUNNER_OS)/praetor-host-runner

build:
	@echo "Building the api service..."
	mkdir -p $(BINARY_DIR)
	go build -o $(API_BINARY) ./cmd/api
	@echo "Build complete. (scheduler, ingestion, consumer, executor, reconciler now"
	@echo " live in their own repos — github.com/praetordev/<service>.)"

# Validate the released component set, contract module versions, and database
# migration range before building or publishing a platform release.
compat-check:
	go run ./cmd/compatcheck

contract-test:
	GOWORK=off go test ./tests/contracts

# Keep deployable health probes synchronized with routes registered by the API.
deployment-contract-test:
	GOWORK=off go test ./tests -run '^TestHelmAPIProbeRoutes$$'

# Keep both local deployment paths immutable and manifest-driven.
local-deploy-contract-test:
	GOWORK=off go test ./tests -run '^TestLocalDeployment'

workflow-lint:
	GOWORK=off go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

# Use the same path classifier as pull-request CI and run only affected gates.
# Add image builds when changing Dockerfiles or validating a release candidate.
verify-changed:
	./scripts/verify-changed.sh

verify-changed-images:
	./scripts/verify-changed.sh --images

# Live integration gate for the deployed Praetor + Secrets Service stack.
secrets-execution-contract-test:
	bash -n ./scripts/test-secrets-execution-e2e.sh
	grep -q 'credential plaintext was stored' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'exactly one credential resolution attempt' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'JOB_COMPLETED' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'PRAETOR_E2E_EVIDENCE_FILE' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'POSTGRES_USER:-postgres' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'terminal executor manifest retained planted secret material' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'completed-run credential replay' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'wrong-workload credential resolution' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'expired binding resolution' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'retired credential binding registration' ./scripts/test-secrets-execution-e2e.sh
	grep -q 'Scanning API, audit, database, and workload artifacts' ./scripts/test-secrets-execution-e2e.sh

secrets-execution-e2e:
	./scripts/test-secrets-execution-e2e.sh

readiness-report-test:
	go test ./internal/readiness ./cmd/readiness-report
	bash -n ./scripts/generate-readiness-report.sh
	bash -n ./scripts/validate-delegated-api-e2e.sh
	bash -n ./scripts/wait-for-postgres.sh

# Run the same pinned Go security scan locally and in CI. The complete report is
# uploaded to code scanning; existing high-severity findings are baselined so
# only new regressions block development.
gosec:
	mkdir -p "$(dir $(GOSEC_REPORT))"
	GOWORK=off go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -no-fail -fmt sarif -out "$(GOSEC_REPORT)" ./...
	./scripts/check-gosec-baseline.sh "$(GOSEC_REPORT)"

# A release preflight intentionally fails while the manifest is marked
# development. The remote form also verifies GHCR images and Go module tags.
release-preflight:
	./scripts/release-preflight.sh

release-preflight-remote:
	./scripts/release-preflight.sh --remote

release-plan:
	@test -n "$(VERSION)" || { echo "usage: make release-plan VERSION=x.y.z"; exit 1; }
	./scripts/promote-platform-release.sh --dry-run "$(VERSION)"

# Run each extracted deployable service as an independent module. Repositories
# are expected beside this one; override their parent with PRAETOR_WORKSPACE_DIR.
workspace-health:
	./scripts/check-workspace-health.sh

shared-module-health:
	./scripts/check-workspace-health.sh --modules

shared-module-health-remote:
	./scripts/check-workspace-health.sh --modules --remote

# Cross-compile the host-runner daemon locally (dev convenience). NOTE: this is
# NOT how a target gets its daemon — the daemon ships inside the Execution Pack,
# pinned by the pack spec's `host_runner` field and checksum-verified at pack build
# (see build/ansible-runtime/Dockerfile). Use `make release-host-runner` to publish
# a version the pack build then pulls. This target is just for local inspection.
host-runner:
	@echo "Building host-runner for $(HOST_RUNNER_OS)/$(HOST_RUNNER_ARCH)..."
	mkdir -p build/$(HOST_RUNNER_OS)
	GOOS=$(HOST_RUNNER_OS) GOARCH=$(HOST_RUNNER_ARCH) CGO_ENABLED=0 go build -o $(HOST_RUNNER_BINARY) ./cmd/host-runner

# Release the host-runner daemon as versioned, per-arch artifacts in Gitea (the
# source of truth for this infra artifact; the execution-pack build pulls it from
# there). Example: make release-host-runner VERSION=0.1.0
release-host-runner:
	@test -n "$(VERSION)" || { echo "usage: make release-host-runner VERSION=x.y.z"; exit 1; }
	VERSION=$(VERSION) ./scripts/release-host-runner.sh

# Mirror the pinned standalone CPython runtime into Gitea's generic package
# registry so pack builds are reproducible/air-gapped (no GitHub at build time).
# Needs GITEA_TOKEN. Example: make mirror-python GITEA_TOKEN=xxxx
mirror-python:
	./scripts/mirror-python.sh

# Mirror the Ansible + pip dependency wheelhouse (linux amd64+arm64) into Gitea's
# PyPI registry so pack builds pip-install from Gitea only. Needs GITEA_TOKEN.
# Example: make mirror-pip GITEA_TOKEN=xxxx REQS="ansible docker"
mirror-pip:
	./scripts/mirror-pip.sh

# Build an Execution Pack (self-contained Python + Ansible pushed to hosts) from a
# declarative YAML spec — the ExecPack equivalent of ansible-builder. Output goes
# to build/runtime/. Example: make execpack SPEC=build/execpack/specs/default.yml
SPEC ?= build/execpack/specs/default.yml
execpack:
	@echo "Building Execution Pack from $(SPEC)..."
	go run ./cmd/execpack -spec $(SPEC) -out build/runtime

test:
	@echo "Running tests..."
	$(MAKE) deployment-contract-test
	$(MAKE) local-deploy-contract-test
	$(MAKE) secrets-execution-contract-test
	$(MAKE) readiness-report-test
	go test -v ./tests/...
	@echo "Running unit tests (incl. #39 no-wildcard-SELECT gate + column-drift checks)..."
	go test ./services/... ./pkg/...
	@echo "Tests passed."

# Exercise execution-plane durability against isolated PostgreSQL and NATS
# containers. This intentionally pauses PostgreSQL and restarts NATS.
chaos-test:
	./scripts/chaos-test.sh

# Manage the local k3d cluster as one dependency-aware unit. These targets avoid
# Docker's restart-policy race where serverlb loops after server-0 was stopped.
local-cluster-create:
	./scripts/local-cluster.sh create

local-cluster-status:
	./scripts/local-cluster.sh status

local-cluster-start:
	./scripts/local-cluster.sh start

local-cluster-stop:
	./scripts/local-cluster.sh stop

local-cluster-recover:
	./scripts/local-cluster.sh recover

local-cluster-update:
	./scripts/update-local-cluster.sh

# Deploy the exact image set declared by platform-compatibility.yaml.
local-cluster-release:
	./scripts/deploy-local-release.sh

# Persistent release-candidate staging prerequisites. This is intentionally
# isolated from the mutable local cluster and disposable validation fixture.
staging-environment-plan:
	./scripts/staging-environment.sh plan

staging-environment-provision:
	./scripts/staging-environment.sh provision

staging-environment-status:
	./scripts/staging-environment.sh status

pilot-host-plan:
	./scripts/pilot-host.sh plan

pilot-host-provision:
	./scripts/pilot-host.sh provision

pilot-host-status:
	./scripts/pilot-host.sh status

pilot-host-reset:
	./scripts/pilot-host.sh reset

staging-pilot-access-plan:
	./scripts/staging-pilot-access.sh plan

staging-pilot-access-seed:
	./scripts/staging-pilot-access.sh seed

staging-pilot-access-status:
	./scripts/staging-pilot-access.sh status

staging-pilot-journey-plan:
	./scripts/staging-pilot-journey.sh plan

staging-pilot-journey-seed:
	./scripts/staging-pilot-journey.sh seed

staging-pilot-journey-status:
	./scripts/staging-pilot-journey.sh status

staging-pilot-journey-run:
	./scripts/staging-pilot-journey.sh run

staging-pilot-journey-faults:
	./scripts/staging-pilot-journey.sh faults

staging-pilot-credential-faults:
	./scripts/staging-pilot-credential-faults.sh

staging-pilot-readiness:
	./scripts/generate-pilot-readiness-report.sh

.PHONY: staging-release-plan staging-release-deploy staging-release-status
staging-release-plan:
	./scripts/staging-release.sh plan

staging-release-deploy:
	./scripts/staging-release.sh deploy

staging-release-status:
	./scripts/staging-release.sh status

.PHONY: staging-integrations-plan staging-integrations-bootstrap staging-integrations-status staging-integrations-verify
staging-integrations-plan:
	./scripts/staging-integrations.sh plan

staging-integrations-bootstrap:
	./scripts/staging-integrations.sh bootstrap

staging-integrations-status:
	./scripts/staging-integrations.sh status

staging-integrations-verify:
	./scripts/staging-integrations.sh verify

.PHONY: staging-recovery-plan staging-recovery-init staging-recovery-backup staging-recovery-verify staging-recovery-restore staging-recovery-exercise
staging-recovery-plan:
	./scripts/staging-recovery.sh plan

staging-recovery-init:
	./scripts/staging-recovery.sh init-recipient

staging-recovery-backup:
	./scripts/staging-recovery.sh backup

staging-recovery-verify:
	@test -n "$(ARCHIVE)" || { echo "ARCHIVE is required" >&2; exit 2; }
	./scripts/staging-recovery.sh verify "$(ARCHIVE)"

staging-recovery-restore:
	@test -n "$(ARCHIVE)" || { echo "ARCHIVE is required" >&2; exit 2; }
	./scripts/staging-recovery.sh restore "$(ARCHIVE)"

staging-recovery-exercise:
	./scripts/staging-recovery.sh exercise

.PHONY: staging-acceptance-plan staging-acceptance-seed staging-acceptance-status staging-acceptance-run staging-execution-diagnostics-plan staging-execution-diagnostics-preflight staging-execution-diagnostics-measure staging-execution-diagnostics-verify
staging-acceptance-plan:
	./scripts/staging-acceptance.sh plan

staging-acceptance-seed:
	./scripts/staging-acceptance.sh seed

staging-acceptance-status:
	./scripts/staging-acceptance.sh status

staging-acceptance-run:
	./scripts/staging-acceptance.sh run

staging-execution-diagnostics-plan:
	./scripts/validate-staging-execution-diagnostics.sh plan

staging-execution-diagnostics-preflight:
	./scripts/validate-staging-execution-diagnostics.sh preflight

staging-execution-diagnostics-measure:
	./scripts/generate-staging-diagnostic-budgets.sh

staging-execution-diagnostics-verify:
	./scripts/validate-staging-execution-diagnostics.sh verify

.PHONY: validation-fixture-create validation-fixture-status validation-fixture-cleanup validation-ldap-operator-journey validation-execution-recovery delegated-fixture-plan delegated-fixture-setup delegated-fixture-validate delegated-fixture-cleanup delegated-fixture-rehearse
validation-fixture-bootstrap:
	./scripts/bootstrap-product-validation-base.sh

validation-fixture-create:
	./scripts/product-validation-fixture.sh create

validation-fixture-status:
	./scripts/product-validation-fixture.sh status

validation-fixture-cleanup:
	./scripts/product-validation-fixture.sh cleanup

validation-ldap-operator-journey:
	./scripts/validate-ldap-operator-journey.sh

validation-execution-recovery:
	./scripts/validate-execution-recovery-e2e.sh

delegated-fixture-plan:
	./scripts/staging-service-principal-fixture.sh plan

delegated-fixture-setup:
	./scripts/staging-service-principal-fixture.sh setup

delegated-fixture-validate:
	./scripts/staging-service-principal-fixture.sh validate

delegated-fixture-cleanup:
	./scripts/staging-service-principal-fixture.sh cleanup

delegated-fixture-rehearse:
	./scripts/staging-service-principal-fixture.sh rehearse

# Full suite against a throwaway, ISOLATED Postgres — the DB-gated integration
# tests (RBAC, reconciler, executor, ...) mutate shared rows, so they must NOT run
# against a live/in-use database. Spins up a fresh postgres, migrates it, runs
# everything with TEST_DATABASE_URL, then tears it down. Needs docker.
TESTDB_PORT ?= 5434
TESTDB_URL  := postgres://postgres:postgres@localhost:$(TESTDB_PORT)/praetor?sslmode=disable
.PHONY: test-db database-compatibility-test
test-db:
	@echo "Starting isolated test DB on :$(TESTDB_PORT)..."
	@docker rm -f praetor-testdb >/dev/null 2>&1 || true
	@docker run -d --name praetor-testdb -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=praetor \
		-p $(TESTDB_PORT):5432 postgres:15 >/dev/null
	@until docker exec praetor-testdb pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
	@echo "Migrating..."
	@DATABASE_URL="$(TESTDB_URL)" go run ./cmd/migrator >/dev/null
	@echo "Running full suite against isolated DB..."
	@TEST_DATABASE_URL="$(TESTDB_URL)" go test -count=1 ./... ; status=$$? ; \
		echo "Tearing down test DB..." ; docker rm -f praetor-testdb >/dev/null 2>&1 || true ; \
		exit $$status

# Exercise representative historical schemas with the real migrator, then prove
# the latest explicitly reversible migration can be rolled back and reapplied.
# Requires an isolated Postgres; CI provides one as a service container.
database-compatibility-test:
	@DATABASE_URL="$(TESTDB_URL)" ./scripts/database-compatibility.sh

clean:
	rm -rf $(BINARY_DIR)
	rm -rf $(KEYS_DIR)

# Database
DB_URL ?= postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable

# Migrations run via the `migrator` service (cmd/migrator), a build dependency of
# every service in docker-compose. There is deliberately no golang-migrate CLI
# target: the numbered schema reuses some version prefixes (two 000008_*, two
# 000009_*), which cmd/migrator tolerates (it keys schema_migrations by filename)
# but the golang-migrate CLI rejects.

# Runners (Use separate terminals)
run-api:
	DATABASE_URL=$(DB_URL) PORT=8080 go run ./cmd/api

# Docker Compose
.PHONY: up down restart logs clean-docker gen-keys

KEYS_DIR=keys
SSH_KEY=$(KEYS_DIR)/id_rsa

gen-keys:
	@echo "Ensuring SSH keys..."
	@mkdir -p $(KEYS_DIR)
	@chmod 700 $(KEYS_DIR)
	@if [ ! -f $(SSH_KEY) ]; then \
		echo "Generating new SSH keys..."; \
		ssh-keygen -t rsa -b 4096 -f $(SSH_KEY) -N "" -C "praetor-internal" || exit 1; \
	fi
	@chmod 600 $(SSH_KEY)
	@chmod 644 $(SSH_KEY).pub

up: gen-keys
	@echo "Starting the control plane (lean: no demo targets / metrics / docs)..."
	docker compose up --build -d

# Full local demo: control plane + demo SSH targets (web1/web2/db1/target-host),
# metrics (prometheus/grafana), and the docs site. Opt-in via compose profiles so
# a plain `make up` stays lean.
up-demo: gen-keys
	@echo "Starting full demo stack (control plane + demo targets + observability + docs)..."
	docker compose --profile demo --profile observability --profile docs up --build -d

down:
	@echo "Stopping Docker Compose stack..."
	docker compose down

restart: down up

logs:
	docker compose logs -f

clean-docker: down
	@echo "Cleaning up Docker resources..."
	docker compose down --volumes --remove-orphans
# Kubernetes / Helm
HELM_CHART = deployments/helm/praetor-v2
RELEASE_NAME = praetor
KIND_CLUSTER = praetor-cluster

.PHONY: helm-install helm-uninstall helm-template kind-load dev-k8s

helm-install:
	@echo "Installing/Upgrading Helm release..."
	helm upgrade --install $(RELEASE_NAME) $(HELM_CHART)

helm-uninstall:
	@echo "Uninstalling Helm release..."
	helm uninstall $(RELEASE_NAME)

helm-template:
	@echo "Rendering Helm templates..."
	helm template $(RELEASE_NAME) $(HELM_CHART)

KIND = $(HOME)/go/bin/kind

kind-load:
	@echo "Loading images into Kind..."
	$(KIND) load docker-image praetor-api:latest --name $(KIND_CLUSTER)
	$(KIND) load docker-image praetor-scheduler:latest --name $(KIND_CLUSTER)
	$(KIND) load docker-image praetor-executor:latest --name $(KIND_CLUSTER)
	$(KIND) load docker-image praetor-consumer:latest --name $(KIND_CLUSTER)
	$(KIND) load docker-image praetor-migrator:latest --name $(KIND_CLUSTER)
	$(KIND) load docker-image praetor-ui:latest --name $(KIND_CLUSTER)

# Complete K8s Dev Loop: Build -> Load -> Deploy
dev-k8s:
	@echo "Building images..."
	docker compose build
	$(MAKE) kind-load
	$(MAKE) helm-install
	@echo "Deploy complete. Check pods with: kubectl get pods"
