# ADR-099: `vibew prompt-template` — canonical agent kickoff prompt owned by the binary

**Date**: 2026-04-28
**Issue**: #1203
**Status**: Accepted

## Context

Three retrospectives — qr-dali on v0.17.0 and qr-code-blackhole on v0.18.1 (twice
through different facets of the same defect) — have converged on the same root
cause: the human-maintained "spin up VibeWarden with an agent" prompt has no
canonical home and rots silently every release.

The most recent failure mode (qr-code-blackhole, v0.18.1) had the agent invoke
`bash deploy.sh` from inside the bundle. The official docs are correct — there
is no `deploy.sh` since #1138 / ADR-088. The bad instruction came from the
user's privately-kept kickoff prompt, which still referenced the pre-#1138
deploy contract. The agent dutifully executed an instruction that no longer
matched the binary it was driving, and the deployment fell apart.

This is a structural problem, not a content problem. As long as the kickoff
prompt lives outside the repo and outside the binary, it cannot stay in sync
with whatever vibew version the user just installed. Every CLI surface change
or deploy contract change risks silently invalidating the prompt.

The fix is to give the prompt the same content authority that `llms-full.txt`
already has (CLAUDE.md §Content authority): vibew owns the canonical kickoff
prompt; the binary regenerates it on demand against its own current knowledge
of CLI flags and the deploy recipe; the website serves the same content via
`llms-full.txt` so URL-discoverable consumption stays in sync.

## Decision

Add a new CLI verb, `vibew prompt-template`, that prints the canonical agent
kickoff prompt to stdout. The prompt is generated from a template embedded in
the binary at build time (`embed.FS`, `text/template`), so it always matches
the binary version. Two flavors are supported via a `--deploy` flag:

1. **Dev only** (default) — install + scaffold + dev loop.
2. **Dev + deploy** (`--deploy`) — adds the `vibew bundle` + `scp` + `ssh` +
   `docker load && docker compose up -d` + healthcheck recipe straight from the
   bundle README contract.

The init step in the prompt always uses both `--name` and `--describe` flags
explicitly. There is no `deploy.sh` reference anywhere in either flavor.

The same canonical content is also embedded in `llms-full.txt` as a new
section so it is discoverable at `https://vibewarden.dev/llms-full.txt`. A
companion file `docs/agent-kickoff.md` is published for direct linkability.

### Alternatives considered

- **(a) `docs/agent-kickoff.md` only, no CLI command.** Rejected because it
  cannot be regenerated per binary version and it provides no path to
  parameterise on the user's project name, description, and domain.
- **(b) Generate the install command from goreleaser metadata.** Rejected as
  over-engineered for v1. The install URL `https://vibewarden.dev/install.sh`
  is already a stable contract used elsewhere; hardcoding it in the template
  is correct.
- **(c) Read the bundle deploy recipe from `internal/app/bundle/bundle_extras.go`'s
  `renderBundleReadme`.** Rejected for v1: the bundle README is
  prose-organised, the kickoff prompt needs a single fenced shell block. The
  v1 design hardcodes the literal command sequence in the prompt template
  with a top-of-file comment naming the bundle README contract as the source
  of truth. Companion issue #1204 is the gating change that lands a
  command-first shell block at the top of the bundle README; once that lands
  the two surfaces are obviously redundant and a follow-up can collapse them.
  This ADR does not block on #1204 — both surfaces simply need to stay in
  sync, which is enforced by the test described under Test strategy.

### CLI command name

The verb is `prompt-template`, not `prompt`, `kickoff`, or `agent-prompt`.
Justification:

- `vibew prompt` reads as a verb that prompts the user for input — the
  opposite of what this command does.
- `kickoff` is jargon (retro vocabulary leaking into the user-visible CLI).
- `agent-prompt` reads like it generates a prompt for the agent the user is
  about to invoke; that is exactly what it does, but `prompt-template` is
  more precise — what is printed is a template (filled with the supplied
  flag values) the user pastes into Claude/GPT/etc.
- `prompt-template` is self-documenting in `vibew --help` output and in
  search-engine queries. The verbosity is acceptable: it is a one-shot
  command typed at most once per project.

### Domain model changes

None. This feature has no domain entities, value objects, or events. The
input is a small set of flag values; the output is a string. The "domain"
collapses to a parameter struct and a pure function that renders a template.

### Ports (interfaces)

No new ports. The existing `ports.TemplateRenderer` (defined in
`internal/ports/scaffold.go`) already provides the contract — render a named
template against a data payload and return bytes. The prompt-template
service composes against that interface exactly the way
`internal/app/scaffold` does.

### Adapters

No new adapters. The existing `internal/adapters/template.Renderer` already
implements `ports.TemplateRenderer` against an `embed.FS` and is the
adapter the new service will be wired to. A new sibling embed.FS is added
for the prompt-template templates (see File layout) — this is consistent
with how `internal/cli/templates` exposes its scaffold templates.

### Application service

A new application package `internal/app/promptkickoff` holds the use case.
Surface:

```go
// Package promptkickoff renders the canonical agent kickoff prompt that
// vibew owns. The package is the single source of truth for what the
// prompt says; both `vibew prompt-template` and the llms-full.txt section
// generator read from this package.
package promptkickoff

// Options is the parameter set for rendering the prompt.
type Options struct {
    Name        string // required: project name (sanitised, validated)
    Describe    string // required: one-line description (validated)
    Domain      string // required when Deploy is true
    Deploy      bool   // false: dev-only flavor; true: dev+deploy flavor
    VibewVersion string // version string; embedded in the prompt header
}

// Service renders the kickoff prompt against an embedded text/template.
type Service struct {
    renderer ports.TemplateRenderer
}

// NewService creates a Service.
func NewService(renderer ports.TemplateRenderer) *Service { ... }

// Render returns the kickoff prompt as bytes ready for stdout.
// Returns an error when Options fail validation.
func (s *Service) Render(opts Options) ([]byte, error) { ... }
```

The service is pure (no I/O): it validates `Options`, picks the template
name based on `opts.Deploy`, calls the renderer, and returns bytes. The
CLI command writes those bytes to stdout.

### File layout

New files:

- `internal/app/promptkickoff/promptkickoff.go` — `Service`, `Options`,
  `Render`, validation rules, error types.
- `internal/app/promptkickoff/promptkickoff_test.go` — unit tests for
  validation and option dispatch.
- `internal/app/promptkickoff/golden_test.go` — golden-file tests for both
  rendered flavors.
- `internal/app/promptkickoff/testdata/dev.golden` — expected output for
  dev-only flavor with canonical fixture inputs.
- `internal/app/promptkickoff/testdata/deploy.golden` — expected output for
  dev+deploy flavor with canonical fixture inputs.
- `internal/cli/cmd/prompt_template.go` — `NewPromptTemplateCmd()` cobra
  wiring.
- `internal/cli/cmd/prompt_template_test.go` — CLI-level tests covering
  flag parsing, validation errors, version surfacing, exit codes, and a
  smoke test that grep-asserts key landmarks (`vibew init --name`,
  `vibew add tls --domain`, `vibew dev`, no `deploy.sh`).
- `internal/cli/templates/prompts/dev.tmpl` — text/template for the
  dev-only flavor.
- `internal/cli/templates/prompts/deploy.tmpl` — text/template for the
  dev+deploy flavor.
- `docs/agent-kickoff.md` — published canonical reference; identical body
  to the rendered output of `vibew prompt-template` with placeholder
  parameters (`<your-project>`, `<your description>`, `<your-domain>`).
- `decisions/adr-099-vibew-prompt-template-canonical-agent-kickoff.md` —
  this ADR.

Modified files:

- `internal/cli/cmd/root.go` — register `NewPromptTemplateCmd()`.
- `internal/cli/templates/embed.go` — extend the `//go:embed` directive
  to include `prompts/*.tmpl`.
- `llms-full.txt` — add a new section `## 21. Agent Kickoff Prompt`
  containing the canonical body of both flavors as fenced blocks, plus a
  pointer to `vibew prompt-template --help`.
- `decisions/README.md` — add the ADR-099 row to the index.

No external dependencies are added. No new ports. No domain types.

### Validation rules

Implemented inside `promptkickoff.Service.Render` (not in the cobra
command, so the rules are unit-testable without exec'ing a binary):

- `Name` is required. Empty → `ErrNameRequired`. The name is sanitised
  using the same rules as `internal/config/config.go`'s
  `sanitizeProjectName` (lowercase, non-alphanumerics → hyphens, trim
  leading/trailing hyphens). The function is duplicated as a small
  unexported helper rather than exported from `config` because the
  config package is a heavyweight import and the rule itself is six
  lines. If it ever drifts from the config layer's rule, the
  `architecture` test suite (ADR-087) is the right place to enforce
  parity.
- `Describe` is required. Trimmed of whitespace. Empty after trim →
  `ErrDescribeRequired`. Contains `\n` or `\r` → `ErrDescribeMultiline`.
- `Domain` is required when `Deploy` is true. Empty → `ErrDomainRequired`.
  We do NOT render with a `<your-domain>` placeholder in the deploy
  flavor; failing loudly is preferred (per triage guidance).
  The domain is not validated as an FQDN — vibew's existing
  `vibew add tls --domain` flow does that downstream.
- `VibewVersion` is required. Empty → `ErrVersionRequired`. The CLI
  command always populates it from the cobra `Version` field.

### Sequence

User invocation:

```
vibew prompt-template --deploy --name foo --describe "bar" --domain demo.example.com
```

1. cobra parses the flags into local `string`/`bool` variables.
2. The command builds a `promptkickoff.Options` value and reads the
   binary version from `cmd.Root().Version`.
3. The command calls `Service.Render(opts)`.
4. `Render` validates the options, choosing one of the typed errors
   above on failure. Validation errors propagate to cobra's `RunE` and
   exit non-zero with the error text on stderr.
5. `Render` selects the template name: `prompts/dev.tmpl` or
   `prompts/deploy.tmpl`.
6. `Render` calls `ports.TemplateRenderer.Render(templateName, opts)` to
   produce the prompt bytes.
7. The command writes the bytes to `cmd.OutOrStdout()` followed by a
   single trailing newline. No log output, no preamble, no "OK" line —
   the output is pasteable directly into a chat with no editing.

The header at the top of every rendered prompt is:

```
# VibeWarden Agent Kickoff Prompt (vibew {{.VibewVersion}})
#
# Generated by `vibew prompt-template{{ if .Deploy }} --deploy{{ end }} \
#   --name <prjname> --describe "<desc>"{{ if .Deploy }} --domain <fqdn>{{ end }}`
# Regenerate any time with `vibew prompt-template --help`.
```

This makes the version visible to humans and to downstream agents that
read the prompt and need to confirm they are running an aligned binary.

### Error cases

| Condition | Behavior |
|---|---|
| `--name` empty | `ErrNameRequired`; exit 1; stderr message names the missing flag |
| `--describe` empty after trim | `ErrDescribeRequired`; exit 1 |
| `--describe` contains newline | `ErrDescribeMultiline`; exit 1 |
| `--deploy` set, `--domain` empty | `ErrDomainRequired`; exit 1; stderr message says "--domain is required when --deploy is set" |
| Binary version unset (`Version == ""`) | `ErrVersionRequired`; should never fire in production builds (main wires `version` via ldflags); guard exists for tests |
| Template parse error | wrapped error; exit 1; should be impossible at runtime — caught by golden tests |

No error path ever writes a partial prompt to stdout. The renderer
returns bytes; the CLI command only writes after `Render` returns nil
err.

### Test strategy

Unit tests live in `internal/app/promptkickoff/`:

- **Validation table tests** — each error path with a minimal `Options`
  diff from a valid baseline.
- **Sanitisation test** — `--name "My Cool App"` → rendered prompt
  contains `vibew init --name my-cool-app`.
- **Golden-file tests** — `dev.golden` and `deploy.golden`. Rendered
  bytes against canonical fixture inputs. The fixture uses
  `name=foo`, `describe="bar"`, `domain=demo.example.com`,
  `version=v0.0.0-test`. Updating the goldens is a deliberate,
  reviewable diff; this is the primary regression gate against version
  drift.
- **Bundle-recipe consistency test** — a smoke test that asserts the
  deploy flavor contains the four landmark commands `scp -r`,
  `docker load -i image.tar`, `docker compose up -d`, and
  `curl -fsSL https://demo.example.com/_vibewarden/health`. This is the
  guard against drift from the bundle README contract; if #1204 ever
  changes the canonical recipe and this template is not updated, the
  test fails. This is a test-with-prejudice — it must reference the
  exact strings that appear in the bundle README, not paraphrases.
- **No-deploy-sh test** — both flavors are asserted not to contain the
  string `deploy.sh`. This is the forensic guard the qr-code-blackhole
  retro asked for.

CLI-level tests live in `internal/cli/cmd/prompt_template_test.go`:

- Flag parsing — `--deploy`, `--name`, `--describe`, `--domain` round-trip.
- Stdout cleanliness — capture stdout, assert first line is the
  `# VibeWarden Agent Kickoff Prompt (...)` header (not a log line).
- Exit codes — validation failures exit non-zero with stderr error text.

Integration with the bundle README is covered by the consistency test
above; no testcontainers / shell-out tests are required for this work.

### New dependencies

None. `text/template` is stdlib; `embed` is stdlib; cobra is already in.
The license verification step is a no-op for this issue.

## Consequences

**Positive.**

- The kickoff prompt is no longer a private artifact owned by each user.
  vibew owns it, ships it inside the binary, and regenerates it on
  demand. Version drift between prompt and binary is structurally
  prevented.
- `llms-full.txt` carries the same canonical content, so URL-fetching
  agents see the same instructions vibew would print locally.
- `docs/agent-kickoff.md` provides a stable URL for users to share.
- Future deploy-contract changes (additional `vibew bundle` flags, new
  `vibew add` plugins, etc.) require a single template edit + golden
  refresh. The retro signature where the prompt rots silently is
  closed.

**Negative / trade-offs.**

- The deploy recipe is duplicated between `prompts/deploy.tmpl` and the
  bundle README. The bundle-recipe consistency test (above) catches
  drift; once #1204 lands a command-first block at the top of the bundle
  README, a follow-up can collapse the duplication by sharing a single
  template fragment between the two surfaces.
- The `llms-full.txt` section is hand-edited rather than generated from
  the same template. This is acceptable because `llms-full.txt` is
  already hand-edited content (CLAUDE.md §Content authority); a
  generator would be over-engineered for v1. If the surface grows
  further, the right move is to add a generator that writes the section
  from the same template, not to add a third hand-edited copy.
- `docs/agent-kickoff.md` is a third surface that mirrors the same
  content with placeholder values. It is intentionally simple — just
  the rendered output with `<your-project>` etc. substituted in — and
  exists for direct linkability. Drift risk is the same as
  `llms-full.txt` and addressed the same way.

**Future work.**

- A follow-up issue can add `vibew prompt-template --output FILE` once
  there is demand. v1 is stdout-only, which keeps the contract
  trivially pipeable.
- Once #1204 lands, fold the bundle deploy recipe into a shared
  template fragment so the bundle README and the prompt-template
  share a single source of truth.
