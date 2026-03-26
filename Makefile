# VibeWarden Makefile

.PHONY: build test lint run docker-up docker-down observability-up observability-down grafana-open prometheus-open loki-open clean check setup-hooks

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
	open http://localhost:3000 2>/dev/null || xdg-open http://localhost:3000

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
check: ## Run all quality checks (build, format, vet, tests)
	@echo "==> Checking formatting (main module)..."
	@test -z "$$(gofmt -l .)" || (echo "gofmt: these files need formatting:" && gofmt -l . && exit 1)
	@echo "==> Running go vet (main module)..."
	go vet ./...
	@echo "==> Building (main module)..."
	go build ./...
	@echo "==> Running tests (main module)..."
	go test -race ./...
	@echo "==> Checking demo-app..."
	@test -z "$$(cd examples/demo-app && gofmt -l .)" || (echo "gofmt: these demo-app files need formatting:" && cd examples/demo-app && gofmt -l . && exit 1)
	cd examples/demo-app && go vet ./... && go build ./... && go test -race ./...
	@echo "==> All checks passed!"

# Install git hooks for local development
setup-hooks: ## Install git hooks for local development
	ln -sf ../../scripts/hooks/pre-push .git/hooks/pre-push
	@echo "Git pre-push hook installed"
