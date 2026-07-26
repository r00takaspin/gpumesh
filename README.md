# GPU Mesh — Free LLM Inference, Powered by Community GPUs

GPU Mesh is a peer-to-peer network for **distributed LLM inference**. GPU owners ("donors") share their idle compute resources, and anyone can use them — for free — through a standard **OpenAI-compatible API**.

No new client. No new API. No credit card. Just two environment variables and every OpenAI-compatible tool you already use (Continue.dev, Aider, Codex CLI, Cline, Open WebUI, LangChain, any SDK) just works.

## How It Works

```
                         ┌──────────────┐
Continue.dev ──HTTP──┐   │  Coordinator │   ┌──────────────┐
Codex CLI    ──HTTP──┤   │              │   │ Donor Agent  │── Ollama
Aider        ──HTTP──┼──▶│ (Public VPS) │──▶│ Donor Agent  │── Ollama
Open WebUI   ──HTTP──┤   │              │   └──────────────┘
LangChain    ──HTTP──┤   └──────────────┘
curl         ──HTTP──┘
```

1. **Donors** run `gpumesh-provider` next to their Ollama instance — one command, zero ongoing maintenance.
2. **The Coordinator** maintains a registry of available models and routes inference requests to the least-loaded donor.
3. **Consumers** set `OPENAI_BASE_URL` and `OPENAI_API_KEY` and use their existing tools as if they were talking to OpenAI.

## Quick Start

### Consumer (use models)

```bash
export OPENAI_BASE_URL="https://gpumesh.io/v1"
export OPENAI_API_KEY="inf_xxxxxxxx"
```

Then use any OpenAI-compatible tool:

```bash
# Aider
aider --openai-api-base $OPENAI_BASE_URL --openai-api-key $OPENAI_API_KEY --model openai/llama3.2:3b

# Python
from openai import OpenAI
client = OpenAI(base_url=os.environ["OPENAI_BASE_URL"], api_key=os.environ["OPENAI_API_KEY"])
```

### Donor (share your GPU)

**Prerequisites:** [Ollama](https://ollama.com) installed with at least one model pulled.

#### Linux & macOS

```bash
curl -sSfL https://gpumesh.io/install-provider.sh | sh
gpumesh-provider
```

The setup wizard will guide you through configuration on first run.

#### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri https://gpumesh.io/static/gpumesh-provider.exe -OutFile gpumesh-provider.exe
.\gpumesh-provider.exe
```

#### From source (any platform)

```bash
go install github.com/r00takaspin/gpumesh/cmd/provider@latest
gpumesh-provider
```

The agent auto-detects your Ollama instance, discovers available models, and starts serving requests. Configuration is persisted to `~/.gpumesh.json`. Earn reputation badges (Bronze → Silver → Gold → Platinum) and climb the public leaderboard.

## Features

- **OpenAI-compatible API** — `/v1/models` and `/v1/chat/completions` with streaming (SSE)
- **Zero consumer setup** — any existing OpenAI-compatible tool works
- **GitHub OAuth** — no password management, natural for the developer audience
- **Rate limiting** — token bucket per API key, configurable
- **Leaderboard** — public donor reputation, weekly/monthly/all-time
- **Web dashboard** — manage keys, view donor stats, copy ready-to-use tool configs
- **Load-aware routing** — requests go to the least-loaded donor for a given model
- **Resilience** — donor disconnect mid-stream triggers transparent retry on another donor
- **Privacy by design** — prompts and responses are never stored

## Architecture

| Component | Binary | Description |
|---|---|---|
| **Coordinator** | `gpumesh-coordinator` | HTTP + WS server, model registry, request relay, rate limiter, web dashboard |
| **Provider Agent** | `gpumesh-provider` | Lightweight agent next to Ollama, connects to coordinator, proxies inference requests |

The coordinator is a single Go binary with an embedded SQLite store. No external database required. Agents maintain an outgoing WebSocket to the coordinator — NAT traversal is trivial.

## Tech Stack

| Decision | Rationale |
|---|---|
| **Go** | Single binary, no runtime deps, excellent concurrency for WS relay |
| **SQLite** | Zero-dependency persistence for users and API keys |
| **WS relay** (not WebRTC) | Works through any NAT, simpler implementation |
| **HTMX + Pico.css** | Functional dashboard, no SPA complexity |
| **Ollama-only (MVP)** | 80%+ of local LLM users use Ollama |

## Project Structure

```
gpumesh/
├── cmd/
│   ├── coordinator/main.go
│   └── provider/main.go
├── internal/
│   ├── proto/          # Shared protocol types and constants
│   ├── coord/          # Coordinator: HTTP handlers, WS, registry, relay
│   ├── provider/       # Provider agent: WS client, Ollama proxy
│   └── dashboard/      # Web dashboard: templates, static assets, OAuth
├── web/
│   ├── templates/
│   └── static/
├── go.mod
├── go.sum
├── README.md
├── SPEC.md
└── LICENSE
```

## Scope

**In MVP:**
- OpenAI-compatible `/v1/models` and `/v1/chat/completions`
- Streaming (SSE) and non-streaming responses
- Rate limiting (token bucket per API key)
- GitHub OAuth + API key management
- Web dashboard with landing page, leaderboard, model catalog
- Donor heartbeat monitoring, reconnection with exponential backoff
- Transparent retry on donor failure

**Out of scope (future):**
- WebRTC direct P2P
- Non-Ollama backends (vLLM, llama.cpp)
- Request queuing / batching
- Credit system / dynamic priority
- Content moderation
- Federation across coordinators

## Safety & Privacy

- **Prompts and responses are never stored.** The coordinator is a relay, not a database of conversations.
- **Donors see prompts** — clearly disclosed to consumers ("the donor can see your requests").
- **Open source.** Anyone can audit, anyone can self-host a coordinator.
- **Abuse reporting.** Consumers can report spam/abuse from donors; repeated reports trigger manual review.

## License

MIT — see [LICENSE](LICENSE) for details.

## Status

MVP — under active development.

