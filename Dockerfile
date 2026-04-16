# VibeWarden Dockerfile — multi-stage build
#
# Stage 1: build the binary using the official Go image.
# Stage 2: copy the binary into a minimal distroless image for the runtime.
#
# This image is used in the Docker Compose dev environment. Production images
# are built by CI and published to the container registry.

# ---------------------------------------------------------------------------
# Stage 1: builder
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

# Install git so `go mod download` can fetch VCS-based modules.
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Copy dependency manifests first to leverage layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source and build.
COPY . .

# Build flags:
#   -trimpath   — removes local file-system paths from stack traces
#   CGO_ENABLED=0 — fully static binary, no libc dependency
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-X main.version=${VERSION} -s -w" \
    -o /vibewarden \
    ./cmd/vibewarden

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
# Alpine provides wget for Docker healthchecks while remaining lightweight.
FROM alpine:3.21

# `apk upgrade` pulls the latest patch versions of the base packages so the
# shipped image picks up CVE fixes released after the alpine:3.21 tag was
# cut (e.g. libcrypto3/libssl3 CVE-2026-28390, musl CVE-2026-40200, zlib
# CVE-2026-22184). Without this the image keeps the tag-time versions
# indefinitely, which Trivy flags.
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates wget

# Copy the statically linked binary.
COPY --from=builder /vibewarden /vibewarden

# VibeWarden listens on 8080 by default.
EXPOSE 8080

ENTRYPOINT ["/vibewarden"]
CMD ["serve"]
