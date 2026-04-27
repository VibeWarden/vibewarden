FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app/server ./cmd/server

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 3000
USER nonroot
CMD ["./server"]
