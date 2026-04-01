# Schema Evolution Policy

VibeWarden's structured event schema is a public API surface. Consumers (AI agents,
log pipelines, dashboards) depend on the shape of every event payload. This document
defines the rules for evolving the schema without breaking existing consumers.

---

## v1 stability guarantee

**The v1 schema is frozen.** Once a payload definition ships in a release, its
fields are immutable:

- No existing field may be removed, renamed, or have its type changed.
- No existing field may have its semantics altered.
- No new required fields may be added to an existing payload.
- The set of `required` fields for each payload is locked.

This guarantee applies to every `$defs/*Payload` object in
`internal/schema/v1/event.json`.

---

## Adding new event types

New `event_type` values (e.g. `waf.blocked`, `egress.forwarded`) **may** be added
to the v1 schema because the `event_type` field is intentionally open-ended:

> Unknown event types must be accepted and passed through.

Consumers must already handle unknown event types gracefully, so adding a new
event type with a new payload definition is a backward-compatible change.

---

## When to create v2

A new schema version (`schema_version: "v2"`) is required when any of the
following changes are needed:

1. Adding a required field to an existing payload.
2. Removing or renaming a field in an existing payload.
3. Changing the type or format of an existing field.
4. Changing the semantics of an existing field.
5. Restructuring the envelope (top-level properties).

When v2 is introduced:

- v1 events continue to be emitted for a minimum of two major releases
  (deprecation period).
- Consumers can opt in to v2 by checking the `schema_version` field.
- The v1 schema file remains in the repository and is never deleted.

---

## Versioning in practice

Every emitted event carries `"schema_version": "v1"`. Consumers should:

1. Check `schema_version` first.
2. Reject or skip events with an unknown version rather than guessing.
3. Use `event_type` to dispatch to type-specific parsing logic.
4. Pass through events with unknown `event_type` values without error.

---

## Summary

| Change | v1 compatible? | Action required |
|--------|----------------|-----------------|
| New event type + payload | Yes | Add to v1 schema |
| New optional field on existing payload | No | Requires v2 |
| New required field on existing payload | No | Requires v2 |
| Remove/rename field | No | Requires v2 |
| Change field type | No | Requires v2 |
