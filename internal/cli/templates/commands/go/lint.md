Run the Go linter. Run:

```bash
golangci-lint run ./...
```

Fix all reported issues before opening a PR. To auto-fix issues where possible:

```bash
golangci-lint run --fix ./...
```

Also run the Go vet tool for additional static analysis:

```bash
go vet ./...
```
