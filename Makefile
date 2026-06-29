.PHONY: build host-runner test clean run-api run-scheduler run-ingestion migrate-up migrate-down

BINARY_DIR=bin
API_BINARY=$(BINARY_DIR)/praetor-api
SCHEDULER_BINARY=$(BINARY_DIR)/praetor-scheduler
INGESTION_BINARY=$(BINARY_DIR)/praetor-ingestion

# Host-runner cross-compilation target. The binary is bootstrapped onto your
# MANAGED hosts, so this is their CPU arch, not necessarily the build machine's.
# It defaults to the build machine's arch (so the local docker-compose demo,
# whose containers run the host arch, works out of the box); override for
# cross-arch targets, e.g. `make host-runner HOST_RUNNER_ARCH=amd64`.
HOST_RUNNER_OS ?= linux
HOST_RUNNER_ARCH ?= $(shell go env GOHOSTARCH)
HOST_RUNNER_BINARY=build/$(HOST_RUNNER_OS)/praetor-host-runner

build: host-runner
	@echo "Building services..."
	mkdir -p $(BINARY_DIR)
	go build -o $(API_BINARY) ./cmd/api
	go build -o $(SCHEDULER_BINARY) ./cmd/scheduler
	go build -o $(INGESTION_BINARY) ./cmd/ingestion
	go build -o $(BINARY_DIR)/praetor-consumer ./cmd/consumer
	go build -o $(BINARY_DIR)/praetor-executor ./cmd/executor
	@echo "Build complete."

# Build the Linux host-runner the executor bootstraps onto target hosts. Served
# to the executor via the ./build directory mount in docker-compose.yml.
host-runner:
	@echo "Building host-runner for $(HOST_RUNNER_OS)/$(HOST_RUNNER_ARCH)..."
	mkdir -p build/$(HOST_RUNNER_OS)
	GOOS=$(HOST_RUNNER_OS) GOARCH=$(HOST_RUNNER_ARCH) CGO_ENABLED=0 go build -o $(HOST_RUNNER_BINARY) ./cmd/host-runner

test:
	@echo "Running tests..."
	go test -v ./tests/...
	@echo "Tests passed."

clean:
	rm -rf $(BINARY_DIR)
	rm -rf $(KEYS_DIR)

# Database
DB_URL ?= postgres://postgres:postgres@localhost:5432/praetor?sslmode=disable
MIGRATE ?= migrate

migrate-up:
	$(MIGRATE) -path db/migrations -database "$(DB_URL)" up

migrate-down:
	$(MIGRATE) -path db/migrations -database "$(DB_URL)" down

# Runners (Use separate terminals)
run-api:
	DATABASE_URL=$(DB_URL) PORT=8080 go run ./cmd/api

run-scheduler:
	DATABASE_URL=$(DB_URL) go run ./cmd/scheduler

run-ingestion:
	DATABASE_URL=$(DB_URL) INGESTION_PORT=8081 go run ./cmd/ingestion

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
	@echo "Starting full stack with Docker Compose..."
	docker compose up --build -d

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
HELM_CHART = deployments/helm/praetor
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


