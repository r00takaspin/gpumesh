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

## Git & Deploy Operations

- **🚫 NEVER `git commit` WITHOUT EXPLICIT USER APPROVAL.**
  Acceptable triggers: "commit", "закоммить", "коммит".
- **🚫 NEVER `git push` (origin OR dokku) WITHOUT EXPLICIT USER APPROVAL.**
  Acceptable triggers: "push", "deploy", "залей", "деплой", "в прод".
- `git push dokku` = PRODUCTION (gpumesh.net). User MUST explicitly confirm before pushing to dokku.
- Make changes to files, but wait for the user's command before touching git.
- Before deploying: ALWAYS build (`go build ./...`) and vet (`go vet ./...`).
- On deploy: push to origin first, then push to dokku.
- After deploy: verify `/health` returns OK before reporting done.
- **NEVER** restart nginx or manually edit `/etc/nginx/` on the VPS — use `dokku proxy:build-config gpumesh`.
- **NEVER** manually kill/rename Docker containers — use `dokku ps:rebuild gpumesh`.
- If Dokku gets stuck: `dokku ps:rebuild gpumesh` is the nuke-from-orbit fix.

## Local Development

- `.env` contains local OAuth credentials and `MESH_BASE_URL`.
- Start server: `export $(grep -v '^#' .env | grep -v '^$' | xargs) && nohup go run ./cmd/coordinator > /tmp/coordinator.log 2>&1 &` (MUST use `nohup` — bare `&` dies with bash)
- **AFTER starting: MUST verify** with `curl -s http://192.168.0.102:8080/health` returns `OK` before telling user it's ready.
- Access at `http://192.168.0.102:8080` (not localhost — OAuth callback is registered for this IP).
- Existing DB is `data/gpumesh.db` — use it, not `/tmp/` temp DB.
