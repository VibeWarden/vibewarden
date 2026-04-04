Generate a dev JWT token for testing authenticated endpoints. Run:

```bash
vibew token
```

To generate a token with custom claims:

```bash
vibew token --email user@test.com --role admin
```

Use the token in requests:

```bash
curl -H "Authorization: Bearer $(vibew token)" https://localhost:8443/api/...
```

This command is only valid when the auth plugin is enabled in `vibewarden.yaml`.
