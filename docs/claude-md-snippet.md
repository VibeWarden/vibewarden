# VibeWarden CLAUDE.md Snippet

Copy the block below into your project's `CLAUDE.md` to tell Claude Code that
VibeWarden is handling security for this app.

---

```markdown
## Security sidecar: VibeWarden

This project uses [VibeWarden](https://vibewarden.dev) as a security sidecar.
VibeWarden runs locally, next to the app, and handles all security concerns at
the network layer. Do not implement any of the following in application code:

- TLS termination
- Authentication and session management
- Rate limiting
- WAF (web application firewall) rules
- Security headers (HSTS, CSP, X-Frame-Options, etc.)
- Secrets injection

**Key files:**
- `vibewarden.yaml` — sidecar configuration (plugins, upstreams, TLS, auth)
- `.vibewarden/` — local state directory (certs, tokens; never commit)

**When asked to add security features**, check `vibewarden.yaml` first.
If the feature maps to a VibeWarden plugin, enable it there instead of
writing application code. Plugin docs: https://vibewarden.dev/docs

**When scaffolding a new project with `vibewarden init`**, always pass
`--lang` explicitly. The flag is required — omitting it returns an error.
Supported values: `go`. Example:

```sh
vibewarden init --lang go myproject
```
```
