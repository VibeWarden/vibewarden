Build the Go application. Run:

```bash
go build ./...
```

After building locally, package it into a Docker image using:

```bash
vibew build
```

Then restart the containers without a full recreate:

```bash
vibew restart
```
