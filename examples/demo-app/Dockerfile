# VibeWarden Demo App — Dockerfile
#
# Multi-stage build following the same pattern as the main VibeWarden Dockerfile.
#
# Stage 1: compile a static binary with the official Go image.
# Stage 2: copy the binary into a minimal distroless image.

# ---------------------------------------------------------------------------
# Stage 1: builder
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /demo-app \
    .

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /demo-app /demo-app

# The demo app listens on port 3000 by default.
EXPOSE 3000

ENTRYPOINT ["/demo-app"]
