# vibewarden
Open source security sidecar for vibe-coded apps. Zero-to-secure in minutes.

## Contributing

### Quality checks

Before submitting a pull request, run all quality checks locally:

```sh
make check
```

This runs `gofmt`, `go vet`, `go build`, and `go test -race` across both the
main module and `examples/demo-app`.

### Git pre-push hook (opt-in)

To have `make check` run automatically before every `git push`, install the
provided hook after cloning:

```sh
make setup-hooks
```

The hook is opt-in and can always be bypassed with `git push --no-verify`.
