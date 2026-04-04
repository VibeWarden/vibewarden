Build the Go application. Run:

```bash
go build ./...
```

To build the main binary:

```bash
go build -o bin/app ./cmd/app
```

After building locally, package it into a Docker image using:

```bash
vibew build
```

Then restart the containers without a full recreate:

```bash
vibew restart
```
