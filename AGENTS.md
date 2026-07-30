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

### Commit messages — NO Cursor co-author

- **НИКОГДА** не добавляй в коммиты `Co-authored-by: Cursor`, `Co-authored-by: cursoragent@cursor.com` и любые другие Cursor/agent trailers.
- Сообщение коммита = только смысл изменения (subject + optional body). Без маркетинга IDE.
- После `git commit` сразу проверь: `git log -1 --format=%B` — если trailer появился сам, **сразу** `git commit --amend` и вырежи его (до push; amend после push на `main` только с явного разрешения на force).
- При истории уже с этими trailer’ами: rebase/filter и вырезать, затем force-with-lease **только** если пользователь явно просит почистить историю.

- Make changes to files, but wait for the user's command before touching git.
- Before deploying: ALWAYS build (`go build ./...`) and vet (`go vet ./...`).
- On deploy: push to origin `main` first; GitHub Actions (`.github/workflows/deploy.yml`) pushes to Dokku with **`branch: main`** (Dokku deploy branch). Push to `master` updates git but does **not** rebuild the container.
- After deploy: verify `/health` returns OK and landing H1 matches SPEC-v2 §9.3 before reporting done.
- **NEVER** restart nginx or manually edit `/etc/nginx/` on the VPS — use `dokku proxy:build-config gpumesh`.
- **NEVER** manually kill/rename Docker containers — use `dokku ps:rebuild gpumesh`.
- If Dokku gets stuck or git sha is new but UI is old: `dokku ps:rebuild gpumesh` is the nuke-from-orbit fix.

### Production SSH (VPS)

- **Alias (preferred):** `ssh gpumesh` — configured in `~/.ssh/config` → `root@194.113.153.87` with `~/.ssh/id_ed25519_r00takaspin`
- **Also:** `ssh gpumesh.net` / `ssh gpumesh-vps` (same host)
- **App:** Dokku `gpumesh`
- **Canonical commands:**

```bash
ssh gpumesh 'dokku logs gpumesh -n 200'
ssh gpumesh 'dokku ps:report gpumesh'
ssh -o BatchMode=yes gpumesh 'hostname'   # non-interactive / agent use
```

- When debugging prod (Cursor hang, 5xx, offline machine): **use this SSH**, don’t claim there is no access.
- Still obey deploy/git rules above: logs/diagnostics OK; rebuild/restart/proxy only when the user asks or it’s clearly required to finish a requested debug.

## Local Development

- `.env` contains local OAuth credentials and `MESH_BASE_URL` — **это источник правды для URL**, не хардкод IP из памяти/старых чатов.
- Existing DB is `data/gpumesh.db` — use it, not `/tmp/` temp DB.
- Templates/CSS are **embedded** (`web.EmbeddedFS`): after UI changes rebuild the binary (`go build -o /tmp/gpumesh-coordinator ./cmd/coordinator`), don’t assume `go run` / old binary picked them up.

### Start coordinator (Cursor / agent shells)

`nohup … &` + `disown` в Cursor **часто умирает** сразу после конца tool-call — процесс пропадает, а агент уже пишет «готово». Так нельзя.

**Правильный старт:** запустить координатор как **persistent background shell** (`block_until_ms: 0` / foreground в фоне harness), например:

```bash
cd <repo> && export $(grep -v '^#' .env | grep -v '^$' | xargs) && \
  go build -o /tmp/gpumesh-coordinator ./cmd/coordinator && \
  /tmp/gpumesh-coordinator >> /tmp/coordinator.log 2>&1
```

(без `&` в конце — harness сам держит background job)

### BEFORE telling the user the server is ready — ALL of these MUST pass

1. `lsof -nP -iTCP:8080 -sTCP:LISTEN` показывает **наш** `gpumesh-coordinator` (не пусто).
2. `BASE=$(grep '^MESH_BASE_URL=' .env | cut -d= -f2-)` — curl **именно** `$BASE/health` → `OK` (не «какой-то IP из AGENTS/чата»).
3. `curl -sS "$BASE/"` → title/H1 содержат **`Share your local models with friends`** (v2). Если видишь **`Free LLM Inference`** — это **чужой/старый v1 хост**, не наш UI. Не отправляй юзера туда.
4. Повтори health через ~1–2с: процесс всё ещё жив и LISTEN на месте.
5. Только после пунктов 1–4 напиши юзеру URL = значение `MESH_BASE_URL` из `.env`.

### Известный проеб (не повторять)

- Хардкод `http://192.168.0.102:8080` в инструкциях/ответах: IP машины меняется; `.102` может быть **другим хостом в LAN** со старым v1, пока локальный координатор мёртв.
- «Сервер готов» после одного `curl` / без проверки title / без проверки что pid ещё слушает — **ложь**. Сначала факты из чеклиста выше, потом слова.
