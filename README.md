# GPU Mesh — Share your local models with friends

> **New to GPU Mesh?** → [What is GPU Mesh?](https://gpumesh.net/about)

Invite-first OpenAI-compatible relay: run Ollama at home, share a PIN, friends point Cursor / Cline / Pi / curl at your machine URL. No public community pool.

Product source of truth: [`SPEC-v2.md`](./SPEC-v2.md). (`SPEC.md` is historical v1.)

## How it works

```
Cursor / Cline / Pi / curl ──HTTP──▶ Coordinator ──WS──▶ Provider agent ──▶ Ollama
```

1. **Owner** runs `gpumesh-provider` next to Ollama and creates a PIN on `/share`.
2. **Friend** signs in with GitHub, enters the PIN on `/join`, gets a binding + API key.
3. They set the harness to `https://gpumesh.net/v1/machines/{machine_id}` + key + model name.

The coordinator is a registry + ACL + HTTP↔WS relay — not a CDN of strangers.

## Quick start

### Use a friend’s machine

After join (or if you own a machine), open **[Use](https://gpumesh.net/use)** → **Set up a tool**.

```text
https://gpumesh.net/v1/machines/{machine_id}
```

Consumer API key (`inf_…`) + Ollama model id from the machine card.

Setup tabs: **curl · Cursor · Cline · Pi · Python**.  
How-to + troubleshooting: [`SPEC-v2.md` §19](./SPEC-v2.md#19-harness-подключение-инструментов).

Cline example:

```bash
cline auth \
  -p openai-compatible \
  -k 'inf_…' \
  -m 'qwen3.5:9b' \
  -b 'https://gpumesh.net/v1/machines/mch_…'
```

### Share your GPU (owner)

**Prerequisites:** [Ollama](https://ollama.com) with at least one model pulled.

Linux & macOS:

```bash
curl -sSfL https://gpumesh.net/install-provider.sh | sh
gpumesh-provider
```

Windows (PowerShell) — from [Releases](https://github.com/r00takaspin/gpumesh/releases/latest):

```powershell
Invoke-WebRequest -Uri https://github.com/r00takaspin/gpumesh/releases/latest/download/gpumesh-provider_windows_amd64.exe -OutFile gpumesh-provider.exe
.\gpumesh-provider.exe
```

From source:

```bash
go install github.com/r00takaspin/gpumesh/cmd/provider@latest
gpumesh-provider
```

Config persists in `~/.gpumesh.json`. Then open `/share` to mint a provider key (if needed) and create invites.

## Features (v2)

- **Per-machine OpenAI API** — `/v1/machines/{id}/models`, `/chat/completions` (stream + tools)
- **Invite-first ACL** — owner or active binding; revoke anytime
- **Stable `machine_id`** — survives provider reconnect (until provider key regenerate)
- **GitHub OAuth** — no passwords
- **Harness snippets** — curl, Cursor, Cline, Pi, Python on `/use`
- **Self-hostable** — single Go binary + SQLite

## Architecture

| Component | Binary | Role |
|---|---|---|
| **Coordinator** | `gpumesh-coordinator` | HTTP API, WS `/ws/provider`, invites/bindings, web UI |
| **Provider agent** | `gpumesh-provider` | Outbound WS to coordinator; proxies to local Ollama |

Agents dial out — no open inbound port on the owner machine.

## Tech stack

| Choice | Why |
|---|---|
| **Go** | Single binary, solid WS concurrency |
| **SQLite** | Zero-ops persistence |
| **WS relay** | Works through NAT |
| **HTMX + CSS** | Dashboard without an SPA |
| **Ollama** | Default local backend |

## Project structure

```
gpumesh/
├── cmd/coordinator/
├── cmd/provider/
├── internal/coord/      # HTTP, WS, store, OAuth
├── internal/provider/   # Agent + Ollama proxy
├── internal/proto/      # Shared protocol
├── web/templates/       # Embedded UI
├── web/static/
├── tests/               # Cucumber + Playwright
├── SPEC-v2.md
└── README.md
```

## Privacy

Prompts go through the coordinator and are visible to whoever runs the machine. Short notice on `/join` and `/use`. Conversations are not stored as a product feature — the coordinator is a relay.

## License

MIT — see [LICENSE](LICENSE).

## Testing

BDD (Cucumber + Playwright) covers API and UI.

```bash
# Coordinator (separate terminal)
TEST_MODE=true MESH_DB=/tmp/gpumesh-test.db MESH_BASE_URL=http://localhost:8080 \
  go run ./cmd/coordinator

cd tests && npm ci

# API
COORDINATOR_URL=http://localhost:8080 npx cucumber-js -p api

# UI (needs Chromium once: npx playwright install chromium)
COORDINATOR_URL=http://localhost:8080 npx cucumber-js -p ui
```

Feature files: `tests/features/`. Steps: `tests/steps/`. Test helpers require `TEST_MODE=true`.
