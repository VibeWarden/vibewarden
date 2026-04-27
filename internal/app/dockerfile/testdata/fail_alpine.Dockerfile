FROM golang:1.26-alpine AS builder
WORKDIR /app
RUN go build -o /app/server .

FROM ubuntu:22.04
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 3000
USER nonroot
CMD ["./server"]
