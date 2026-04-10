# VibeWarden Makefile

.PHONY: build test lint run docker-up docker-down observability-up observability-down grafana-open prometheus-open loki-open clean check setup-hooks demo demo-build demo-tls demo-down demo-clean deploy-demo

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
BINARY := bin/vibewarden

# Build the binary
build:
	@mkdir -p bin
	go build $(LDFLAGS) -o $(BINARY) ./cmd/vibewarden

# Run tests
test:
	go test -race -v ./...

# Run linter
lint:
	golangci-lint run

# Build and run
run: build
	./$(BINARY)

# Start dev environment
docker-up:
	docker compose up -d

# Stop dev environment
docker-down:
	docker compose down

# Start observability stack (Prometheus)
observability-up:
	docker compose --profile observability up -d

# Stop observability stack
observability-down:
	docker compose --profile observability down

# Open Grafana dashboard in the default browser (macOS/Linux)
grafana-open:
	open http://localhost:3001 2>/dev/null || xdg-open http://localhost:3001

# Open Prometheus UI in the default browser (macOS/Linux)
prometheus-open:
	open http://localhost:9090 2>/dev/null || xdg-open http://localhost:9090

# Open Loki UI in the default browser (macOS/Linux)
loki-open:
	open http://localhost:3100/ready 2>/dev/null || xdg-open http://localhost:3100/ready

# Clean build artifacts
clean:
	rm -rf bin/

# Run all quality checks (build, format, vet, tests)
check: ## Run all quality checks (lint, build, tests)
	@echo "==> Checking formatting (main module)..."
	@test -z "$$(gofmt -l .)" || (echo "gofmt: these files need formatting:" && gofmt -l . && exit 1)
	@echo "==> Running golangci-lint (main module)..."
	golangci-lint run ./...
	@echo "==> Building (main module)..."
	go build ./...
	@echo "==> Running tests (main module)..."
	go test -race ./...
	@echo "==> Checking demo-app..."
	@test -z "$$(cd examples/demo-app && gofmt -l .)" || (echo "gofmt: these demo-app files need formatting:" && cd examples/demo-app && gofmt -l . && exit 1)
	cd examples/demo-app && go vet ./... && go build ./... && go test -race ./...
	@echo "==> All checks passed!"

# Build the VibeWarden Docker image locally and tag it so demo targets work
# without pulling from ghcr.io. No Go toolchain required — Docker handles the build.
demo-build: ## Build the VibeWarden Docker image locally (required before running demo targets)
	docker build --tag ghcr.io/vibewarden/vibewarden:latest .

# Start the full local demo stack — no Go toolchain required.
# `vibewarden generate` runs inside the locally-built Docker image.
demo: demo-build ## Start the full local demo stack (http://localhost:8080, Grafana http://localhost:3001)
	docker run --rm \
	  -v "$(CURDIR)/examples/demo-app:/work" \
	  -w /work \
	  ghcr.io/vibewarden/vibewarden:latest \
	  generate
	cd examples/demo-app && \
	  COMPOSE_PROFILES=observability \
	  docker compose -f .vibewarden/generated/docker-compose.yml up -d
	@echo ""
	@echo "Demo stack is starting — wait ~30 s for all services to be healthy."
	@echo ""
	@echo "  App:        http://localhost:8080"
	@echo "  Grafana:    http://localhost:3001"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "Demo credentials: demo@vibewarden.dev / demo1234"
	@echo "Run 'vibew secret get postgres' to retrieve generated credentials."

# Start demo with TLS — no Go toolchain required.
demo-tls: demo-build ## Start the full local demo stack with self-signed TLS (https://localhost:8443)
	docker run --rm \
	  -v "$(CURDIR)/examples/demo-app:/work" \
	  -w /work \
	  -e VIBEWARDEN_TLS_ENABLED=true \
	  -e VIBEWARDEN_TLS_PROVIDER=self-signed \
	  -e VIBEWARDEN_SERVER_PORT=8443 \
	  ghcr.io/vibewarden/vibewarden:latest \
	  generate
	cd examples/demo-app && \
	  COMPOSE_PROFILES=observability \
	  docker compose -f .vibewarden/generated/docker-compose.yml up -d
	@echo ""
	@echo "Demo stack is starting — wait ~30 s for all services to be healthy."
	@echo ""
	@echo "  App:        https://localhost:8443   (accept the self-signed cert warning)"
	@echo "  Grafana:    http://localhost:3001"
	@echo "  Prometheus: http://localhost:9090"
	@echo ""
	@echo "Demo credentials: demo@vibewarden.dev / demo1234"

# Stop the full local demo stack
demo-down: ## Stop the full local demo stack
	cd examples/demo-app && \
	  docker compose -f .vibewarden/generated/docker-compose.yml down

# Stop and remove volumes
demo-clean: ## Stop the demo stack and remove all volumes
	cd examples/demo-app && \
	  docker compose -f .vibewarden/generated/docker-compose.yml down -v && \
	  rm -rf .vibewarden/generated/

# Deploy the public demo to a remote VM via SSH.
# Usage: make deploy-demo SSH=root@challenge.vibewarden.dev
# Rollback: make deploy-demo SSH=root@challenge.vibewarden.dev ROLLBACK=--rollback
deploy-demo: ## Deploy the public demo (SSH=<target> required; optional ROLLBACK=--rollback)
	@if [ -z "$(SSH)" ]; then \
		echo "error: SSH target is required. Usage: make deploy-demo SSH=root@challenge.vibewarden.dev"; \
		exit 1; \
	fi
	./scripts/deploy-demo.sh $(SSH) $(ROLLBACK)

# Install git hooks for local development
setup-hooks: ## Install git hooks for local development
	ln -sf ../../scripts/hooks/pre-push .git/hooks/pre-push
	@echo "Git pre-push hook installed"
