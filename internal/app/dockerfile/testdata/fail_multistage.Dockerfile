FROM golang:1.26-alpine
WORKDIR /app
COPY . .
RUN go build -o /app/server ./cmd/server
EXPOSE 3000
USER nonroot
CMD ["/app/server"]
