Start the dev environment using the VibeWarden sidecar. Run:

```bash
vibew dev
```

This generates runtime config, builds the Docker image (multi-stage, no local toolchain required), and starts the full stack including the app and sidecar.

Use `vibew dev --watch` to enable hot reload on config changes.

Never start the app directly with `go run`, `gradlew run`, `npm start`, or any equivalent command — always use `vibew dev`.
