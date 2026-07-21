# Agent Instructions

## Product Specification

All changes to this repository **MUST** align with `SPEC.md` — the authoritative product specification.

### Before Starting Any Task

1. **Read the relevant section** of `SPEC.md` that covers the area you're modifying.
2. If the change introduces new behaviour not described in the spec, **update the spec first** (or flag it for spec update).
3. Never implement a feature that contradicts what's already specified without explicit approval.

### Non-Negotiable

- Landing page (`/`) MUST match `SPEC.md` §6.1
- API surface (`/v1/*`) MUST match `SPEC.md` §5.1 and §3.1–§3.4
- Donor protocol (WebSocket) MUST match `SPEC.md` §3.4 and §4
- Data model MUST match `SPEC.md` §7

### When in Doubt

SPEC wins. If SPEC is ambiguous, default to the interpretation that preserves existing behaviour.
