Build the TypeScript application. Run:

```bash
npm run build
```

After building locally, package it into a Docker image using:

```bash
vibew build
```

Then restart the containers without a full recreate:

```bash
vibew restart
```
