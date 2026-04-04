Run the Go test suite. Run:

```bash
go test ./...
```

To run tests with the race detector:

```bash
go test -race ./...
```

To run a specific package or test:

```bash
go test ./internal/domain/...
go test -run TestMyFunction ./...
```

Always fix failing tests before opening a PR.
