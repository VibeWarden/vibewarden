# VibeWarden Makefile

.PHONY: build test lint run docker-up docker-down observability-up observability-down clean

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

# Clean build artifacts
clean:
	rm -rf bin/
