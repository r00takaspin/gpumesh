# Agent Instructions

## Product Specification

All changes to this repository **MUST** align with `SPEC-v2.md` — the authoritative product specification for current development.
`SPEC.md` is historical (v1 community mesh) and must not drive new features.

### Before Starting Any Task

1. **Read the relevant section** of `SPEC-v2.md` that covers the area you're modifying.
2. If the change introduces new behaviour not described in the spec, **update the spec first** (or flag it for spec update).
3. Never implement a feature that contradicts what's already specified without explicit approval.

### Non-Negotiable

- Landing page (`/`) MUST match `SPEC-v2.md` §9.3
- API surface (`/v1/*`) MUST match `SPEC-v2.md` §6.1 and §7.2 (per-machine hard pin)
- Provider protocol (WebSocket `/ws/provider`) MUST match `SPEC-v2.md` §6.2
- Data model MUST match `SPEC-v2.md` §10 (machines, invites, bindings)
- Access is invite-first: owner OR active binding — no public community pool

### When in Doubt

SPEC-v2 wins. If SPEC-v2 is ambiguous, default to the interpretation that preserves existing v2 behaviour.

## Git & Deploy Operations

> **🚫 ЗАПРЕЩЕНО БЕЗ ЯВНОГО РАЗРЕШЕНИЯ:**
> - `git commit` — жди слов: «commit», «закоммить», «коммит»
> - `git push` (origin или dokku) — жди слов: «push», «deploy», «залей», «деплой», «в прод»
> - Ты НЕ решаешь сам когда коммитить/пушить. Даже если сделал правку и кажется очевидным — СПРОСИ.
> - `git push dokku` = PRODUCTION (gpumesh.net). Спрашивай ОТДЕЛЬНО.

- Make changes to files, but wait for the user's command before touching git.
- Before deploying: ALWAYS build (`go build ./...`) and vet (`go vet ./...`).
- On deploy: push to origin `main` first; GitHub Actions (`.github/workflows/deploy.yml`) pushes to Dokku with **`branch: main`** (Dokku deploy branch). Push to `master` updates git but does **not** rebuild the container.
- After deploy: verify `/health` returns OK and landing H1 matches SPEC-v2 §9.3 before reporting done.
- **NEVER** restart nginx or manually edit `/etc/nginx/` on the VPS — use `dokku proxy:build-config gpumesh`.
- **NEVER** manually kill/rename Docker containers — use `dokku ps:rebuild gpumesh`.
- If Dokku gets stuck or git sha is new but UI is old: `dokku ps:rebuild gpumesh` is the nuke-from-orbit fix.

## Local Development

- `.env` contains local OAuth credentials and `MESH_BASE_URL`.
- Start server: `export $(grep -v '^#' .env | grep -v '^$' | xargs) && nohup go run ./cmd/coordinator > /tmp/coordinator.log 2>&1 &` (MUST use `nohup` — bare `&` dies with bash)
- **AFTER starting: MUST verify** with `curl -s http://192.168.0.102:8080/health` returns `OK` before telling user it's ready.
- Access at `http://192.168.0.102:8080` (not localhost — OAuth callback is registered for this IP).
- Existing DB is `data/gpumesh.db` — use it, not `/tmp/` temp DB.
