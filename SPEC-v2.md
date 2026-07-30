# GPU Mesh v2 — Спецификация продукта

> **Статус:** Черновик v2 — заменяет продуктовое направление [`SPEC.md`](SPEC.md) (v1)  
> **Лицензия:** MIT  
> **Стек:** Go (координатор + агент)  
> **Визуал:** Light calm — экраны и статусы: [`docs/visual-v2-screens.html`](docs/visual-v2-screens.html); moodboard: [`docs/visual-v2-sketch.html`](docs/visual-v2-sketch.html)  
> **Совместимость с v1:** breaking change; активных пользователей нет — чистый cut допустим

---

## 0. Терминология v2

Слово **«донор» / donor** в продукте v2 **не используется**. В v1 оно означало «отдаю GPU незнакомцам в commons» — это больше не модель.

| Термин v2 | Было в v1 | Смысл |
|---|---|---|
| **Owner** | Donor (роль человека) | Владелец машины; шарит доступ по PIN |
| **Provider** | Donor agent / `gpumesh-provider` | Агент на машине рядом с Ollama |
| **Provider token / key** | Donor token / scope `donor` | API-ключ для WS агента; scope в v2: `provider` (вместо `donor`), либо `both` |
| **Member** | Consumer (когда по invite) | Приглашённый пользователь с binding |
| **Machine** | Provider session / donor node | Логическая машина со стабильным `machine_id` |
| **Operator** | Coordinator operator | Кто эксплуатирует координатор |

В UI (English): *Share*, *Your machines*, *Provider token*, *Members* — не *Donate* / *Donor*.  
В коде/БД при миграции: переименовать `donor_*` → `owner_*` / `provider_*` (см. §10); старые пути `/api/donor/*` в v2 заменяются на `/api/owner/*`.

---

## 0a. Связь с v1 и цели документа

### 0a.1 Что меняется относительно v1

| v1 (`SPEC.md`) | v2 (этот документ) |
|---|---|
| Публичный community pool («доноры») | Invite-first: доступ только к **конкретной машине** |
| Балансировка / sticky affinity между нодами | Hard bind: запрос идёт только на машину из URL |
| `POST /v1/chat/completions` без выбора машины | `POST /v1/machines/{machine_id}/chat/completions` |
| Каталог `/models` как витрина commons | Удалён как marketplace; модели видны в контексте bindings |
| Мотивация: альтруизм / leaderboard | Мотивация: пошарить GPU своим по PIN |
| Термин «donor» | Owner / Provider / Machine |
| Терминальный dark UI | Light calm UI |

### 0a.2 Цели продукта v2

1. Owner поднимает Ollama + агент и выдаёт **PIN** доверенным людям.
2. Member один раз вводит PIN (после GitHub), получает binding и API-ключ.
3. Member настраивает **харнес** (Continue, Cline, Aider, curl/SDK и т.п.) на `OPENAI_BASE_URL` + key для **конкретной машины** и работает без повторного ввода PIN.
4. Координатор — реестр машин, ACL (invites/bindings), релей HTTP↔WS. Не «CDN незнакомцев».

### 0a.3 Критерий успеха

**Главная цель v2:** опубликовать на **Хабре** пошаговую статью:

> Как пошарить свою GPU другу по PIN и подключить её в LLM-харнес (OpenAI-compatible base URL).

Статья должна опираться на **работающий** публичный (или self-host) флоу из этой спеки, без «скоро будет».

Вторичные эффекты (регистрации, звёзды, ~N пользователей) — приятный побочный результат, не KPI продукта на этом этапе.

### 0a.4 Non-goals v2

- Public community mesh / анонимный пул
- WebRTC / P2P (промпты по-прежнему идут через координатор-релей)
- Не-Ollama бэкенды (vLLM и т.п.)
- Кредиты, thanks, stars, leaderboard
- Параллельные jobs / map-reduce
- Федерация координаторов
- Платёжная система
- Drop-in гарантия для всех OpenAI-инструментов без настройки base URL (форма API близка к OpenAI, но пути **per-machine**)
- Менеджмент токенов / статус машин / кабинет owner’а внутри `gpumesh-provider` (control plane только `/share`, см. §11.0)

---

## 1. Обзор продукта

### 1.1 Что это

**GPU Mesh v2** — сервис приглашений к домашнему (или серверному) LLM-железу. Owner шарит свою машину с Ollama по одноразовому/ограниченному PIN. Member подключается через GitHub и дальше ходит в модели этой машины через OpenAI-подобный HTTP API, где **идентификатор машины лежит в URL**.

### 1.2 Ценностное предложение

| Роль | Проблема | Решение |
|---|---|---|
| **Owner** | Хочет дать доступ своим, не открывая порт и не пуская весь интернет | PIN + GitHub redeem; revoke в один клик; LLM-only поверхность |
| **Member** | Нужен remote endpoint к чужой/своей второй машине в привычных инструментах | `BASE_URL=/v1/machines/{id}` + API key; имена моделей как в Ollama |
| **Оба** | Не доверяют public pool (контекст, чужие промпты, hop между нодами) | Нет балансировки по незнакомцам; один URL = одно железо |

### 1.3 Чем это не является

| Альтернатива | Разница |
|---|---|
| **ngrok / localhost.run** | Туннель открывает порт; трафик через туннель-оператора; нет ACL «GitHub-user + revoke grant» из коробки |
| **Tailscale** | Полный VPN/сеть; для семьи часто бесплатен; сильнее как general remote. Mesh — только LLM API + invite UX |
| **v1 community mesh** | Пул и балансировка; v2 сознательно от этого уходит |

Честное позиционирование: не «убийца Tailscale», а **приглашение к LLM-машине с base URL для харнесов**.

### 1.4 Ключевой DX для харнесов

```bash
export OPENAI_BASE_URL="https://gpumesh.net/v1/machines/mch_01HZX..."
export OPENAI_API_KEY="inf_..."
# model в запросах — как у Ollama: llama3.2:3b
```

Нет кастомных заголовков. Нет переименования моделей. PIN в inference **не участвует**.

---

## 2. Роли пользователей

### 2.1 Owner (владелец машины)

- Имеет GitHub-аккаунт
- Запускает `gpumesh-provider` с **provider token** рядом с Ollama
- Создаёт invites (PIN), видит members, делает revoke
- **Автоматически** имеет право вызывать `/v1/machines/{id}/*` для своих машин (без PIN)

### 2.2 Member (приглашённый)

- Имеет GitHub-аккаунт
- Redeem PIN → binding на машину owner’а
- Может иметь **несколько** bindings (несколько PIN от разных owner’ов / машин)
- Для каждой машины использует свой `BASE_URL` с соответствующим `machine_id`
- Не видит чужие машины и публичный пул

### 2.3 Operator

- Эксплуатирует координатор (`MESH_*` env)
- Задаёт rate limits, следит за `/health`
- Не читает содержимое промптов из БД (они не персистятся); на релею оператор теоретически может видеть трафик — опенсорс + self-host как митигация (как в v1)

Публичный инстанс (`gpumesh.net`): деплой через Dokku. **Deploy branch = `main`**. Push в другую ветку (например `master`) обновляет git на сервере, но **не** пересобирает контейнер — после деплоя обязательны `/health` → `OK` и smoke лендинга (H1 из §9.3). Застрявший процесс: `dokku ps:rebuild gpumesh` (не ручной docker/nginx).

### 2.4 Совмещение ролей

Один GitHub-пользователь может быть owner и member одновременно (своя машина + чужие invites).

---

## 3. Архитектура

### 3.1 Схема

```
Harness / curl / SDK
        │  HTTPS OpenAI-like
        │  /v1/machines/{machine_id}/...
        ▼
┌───────────────────────────┐
│  Coordinator              │
│  - GitHub OAuth           │
│  - invites / bindings     │
│  - API keys               │
│  - machine registry       │
│  - HTTP ↔ WS relay        │
└─────────────┬─────────────┘
              │ WebSocket /ws/provider
              ▼
     gpumesh-provider ──▶ Ollama (localhost)
```

PIN используется **только** на этапе join. После redeem путь данных: Member → Coordinator → Provider → Ollama.

### 3.2 Почему не WebRTC в v2

WebRTC улучшает privacy (direct path), но усложняет MVP (signaling, TURN, consumer sidecar). Для статьи на Хабр и подключения харнеса достаточно текущего WS-релея. WebRTC — §15 «Будущее».

### 3.3 Стабильный `machine_id`

Проблема v1: `provider_id` генерировался на каждое WS-подключение → URL харнеса ломался бы при реконнекте.

**Правило v2:**

- В SQLite есть таблица `machines`.
- Одна logical machine = один **provider API key** (scope `provider` или `both`).
- При первом успешном WS-подключении с этим key создаётся (или находится) запись `machines.id = machine_id` (например `mch_` + ULID/UUID).
- Пока используется тот же provider key, `machine_id` **не меняется** при disconnect/reconnect.
- Runtime session id / ws conn остаётся эфемерным в памяти; маршрутизация идёт: `machine_id` → текущий online session (если есть).
- Перевыпуск provider key (`regenerate`) → **новый** `machine_id` (старые invites/bindings revoke’атся, старый URL перестаёт работать; UI предупреждает). Машина со старым (revoked) provider key **не показывается** в `/use` и `/share`.

Один человек может иметь несколько **активных** provider keys → несколько machines (несколько агентов).

---

## 4. Invite и PIN

### 4.1 Назначение PIN

PIN — **код приглашения** (capability на redeem), не секрет inference.

| Свойство | Значение по умолчанию |
|---|---|
| Формат | `XXXX-XXXX` (8 символов из алфавита без `0/O/1/I/L`) |
| Хранение | Только SHA-256 hash в БД; plaintext показывается при создании |
| TTL | 7 дней с момента создания |
| max_uses | 1 |
| После redeem | Membership (binding) живёт независимо от TTL PIN, пока не revoke |

Owner может задать при создании: `max_uses` (1–10), `ttl_days` (1 / 7 / 30).

### 4.2 Создание invite (Owner)

1. Owner залогинен, имеет хотя бы одну machine (provider key; агент желательно online — иначе PIN создать можно, но UI показывает warning «machine offline»).
2. `POST` create invite с привязкой к `machine_id`.
3. Ответ один раз содержит: plaintext PIN, join link `https://<host>/join?pin=XXXX-XXXX`, expiry, max_uses.
4. В списке дальше: masked PIN (`7K4Q-****`), status (`active` / `exhausted` / `expired` / `revoked`); если PIN уже redeem’или — **Used by @login** (по `bindings.invite_id`, включая позже отозванных members).

### 4.3 Redeem (Member)

1. Member открывает `/join` или `/join?pin=...`.
2. Если нет сессии → GitHub OAuth с `redirect=/join?pin=...`.
3. Submit PIN.
4. Координатор:
   - проверяет hash, expiry, revoked, uses < max_uses;
   - создаёт `binding (member_user_id, machine_id)` если ещё нет;
   - инкрементирует `uses`;
   - если нет consumer API key — создаёт scope `consumer` и показывает raw key один раз;
   - если key уже есть — использует существующий (не плодит ключи без нужды).
5. Redirect на `/use` с контекстом новой машины.

**Owner redeem своего PIN:** допускается (no-op binding или «already owner»); не ошибка безопасности.

### 4.4 Ошибки redeem

| Код | Когда |
|---|---|
| `invalid_pin` | Неверный формат / нет в БД |
| `expired` | TTL истёк |
| `exhausted` | uses >= max_uses |
| `revoked` | Invite отозван |
| `machine_gone` | Machine/key удалены |
| `rate_limited` | Слишком много попыток PIN с IP/user |

### 4.5 Revoke

| Действие | Эффект |
|---|---|
| Revoke invite | Новые redeem невозможны; существующие bindings **не** трогаются |
| Revoke binding | Member теряет доступ к `machine_id`; активные SSE к этой машине отменяются |
| Revoke all bindings for machine | Panic-кнопка owner’а |
| Rotate / regenerate provider key | Новый `machine_id`; старые invites/bindings на старый id невалидны |

### 4.6 Sequence

```
Owner                    Coordinator                 Member
  │                           │                         │
  │ GitHub + provider token   │                         │
  │ Provider WS register      │                         │
  │ CreateInvite(machine)     │                         │
  │──────────────────────────▶│                         │
  │◀── PIN + link ────────────│                         │
  │                           │                         │
  │   (PIN в мессенджер)      │                         │
  │                           │  GET /join?pin=...      │
  │                           │◀────────────────────────│
  │                           │  GitHub OAuth           │
  │                           │  Redeem PIN             │
  │                           │◀────────────────────────│
  │                           │  binding + API key      │
  │                           │────────────────────────▶│
  │                           │                         │
  │                           │  POST .../machines/id/  │
  │                           │       chat/completions  │
  │◀──── WS request ──────────│◀────────────────────────│
  │──── chunk/response ───────│──── SSE/JSON ──────────▶│
```

---

## 5. Bindings и доступ

### 5.1 Правила авторизации inference

Запрос к `/v1/machines/{machine_id}/...` разрешён, если API key принадлежит user U и:

1. U — **owner** машины (`machines.owner_user_id = U`), или
2. Существует **active binding** `(U, machine_id)` без `revoked_at`.

Иначе → `403`.

### 5.2 Несколько машин у member (2A)

- Member может redeem несколько PIN → несколько строк bindings.
- В UI `/use` — список машин с online/offline, models, готовый `BASE_URL`.
- Харнес настраивается **на одну машину за раз** (один base URL). Переключение = смена base URL в конфиге инструмента.
- Конфликт имён моделей между машинами **не возникает на уровне API**, потому что машина зафиксирована путём.

### 5.3 Hard pin (семантика маршрутизации)

- Нет выбора «наименее загруженной чужой ноды».
- Нет sticky affinity между разными machines.
- Нет прозрачного failover на другую machine того же member.
- Если machine offline → `503` с телом вроде `{"error":"machine_offline","machine_id":"..."}`.
- Если online, но `current_load >= max_concurrent` → `503` `machine_busy` (+ опционально `retry_after_seconds`).
- Mid-stream обрыв WS → закрыть SSE; client retries сам (как в v1 для streaming). Для non-stream: до 3 попыток **только на ту же machine** (не на другую).

### 5.4 Owner и свои машины

`GET /v1/models` (discovery) и `/use` показывают owner’у все его machines без PIN.  
Snippets копируют `/v1/machines/{id}` для self-use (remote к своему GPU).

---

## 6. Протоколы

### 6.1 Потребитель ↔ Координатор (HTTP)

Форма близка к OpenAI Chat Completions, пути — per-machine.

#### Discovery (все доступные машины пользователя)

```
GET /v1/models
Authorization: Bearer <api_key>
```

Ответ:

```json
{
  "object": "list",
  "data": [
    {
      "id": "llama3.2:3b",
      "object": "model",
      "owned_by": "mch_01HZX...",
      "machine_id": "mch_01HZX...",
      "machine_name": "home-lab",
      "online": true,
      "load": 0.0
    }
  ]
}
```

Используется UI и отладкой. Харнесы с одним `BASE_URL` обычно бьют в per-machine endpoint ниже.

#### Per-machine models

```
GET /v1/machines/{machine_id}/models
Authorization: Bearer <api_key>
```

```json
{
  "object": "list",
  "data": [
    {
      "id": "llama3.2:3b",
      "object": "model",
      "owned_by": "owner",
      "online": true,
      "load": 0.25
    }
  ]
}
```

Имена `id` = имена Ollama **без** префикса машины.

#### Chat completions

```
POST /v1/machines/{machine_id}/chat/completions
Authorization: Bearer <api_key>
Content-Type: application/json
```

Тело — OpenAI-совместимое (`model`, `messages`, `stream`, `temperature`, `tools`, `tool_choice`, …).  
Поле `model` уходит в Ollama как есть (после возможного strip префикса `openai/` как в v1 §3.5 шаг 0 — сохранить для LiteLLM/Aider).  
`tools` / `tool_choice` прокидываются в Ollama; ответные `tool_calls` возвращаются в OpenAI-форме (`finish_reason: tool_calls` когда применимо). Нужно для Cursor Ask/Agent и других tool-calling харнесов.

Streaming: SSE (`X-Accel-Buffering: no`; пустые content-keepalives не шлём). Non-stream: JSON.

**Legacy пути v1 (поведение v2):**

| Путь | Поведение v2 |
|---|---|
| `POST /v1/chat/completions` | **410 Gone** или **400** с сообщением использовать `/v1/machines/{machine_id}/chat/completions` (предпочтительно 410 + JSON error) |
| `GET /v1/models` | Остаётся как **discovery** по bindings/owned (см. выше), не как «все community models» |

CORS: `Access-Control-Allow-Origin: *` на `/v1/*`.

Rate limit: token bucket на API key; default `MESH_RATE_LIMIT=100` req/hour; заголовки `X-RateLimit-Remaining`, при 429 — `Retry-After`.

### 6.2 Координатор ↔ Provider (WebSocket)

Путь: `GET /ws/provider?token=<provider_api_key>` — **reused**.

Сообщения (переиспользовать `type` из v1 / `internal/proto`):

**Provider → Coordinator:** `register`, `heartbeat`, `chunk`, `response`, `error`  
**Coordinator → Provider:** `registered`, `request`, `cancel`, `heartbeat_ack`

В `register` по-прежнему: `models[]`, `max_concurrent`, `description`, `hardware`.

Ответ `registered` в v2 **обязан** включать стабильный `machine_id` (и может включать эфемерный `provider_id` для логов).

Коды ошибок provider: `backend_unavailable`, `model_not_found`, `timeout`, `overloaded`, `internal` — как в v1.

### 6.3 Таймауты

- TTFT (до первого токена): **120s** (cold load + большие промпты/tools от Cursor).
- Inter-token: **120s** (thinking-модели вроде qwen могут молчать между чанками).
- Общий таймаут запроса: **300s**.
- Heartbeat timeout: 90s без heartbeat → session offline (machine запись остаётся, online=false).
- `backend_ok=false` > 5 минут → disconnect WS.
- Provider прокидывает Ollama `thinking` в `content`, если `content` пуст (иначе SSE выглядит «мёртвым» для Cursor).

---

## 7. HTTP / UI карта endpoint’ов

Легенда: **R** = reused path (семантика может измениться), **N** = new, **X** = removed / replaced.

### 7.1 Публичные и auth

| Метод | Путь | Статус | Auth | Описание v2 |
|---|---|---|---|---|
| `GET` | `/` | R | Нет | Landing Light calm: invite narrative |
| `GET` | `/use` | R | Публичный / session | Member dashboard: bindings, URLs, keys |
| `GET` | `/share` | R | Публичный / session | Owner dashboard: setup, PIN, members, machines |
| `GET` | `/join` | N | Публичный / session | Ввод PIN / redeem |
| `GET` | `/about` | R | Нет | Переписанный about под invite-first |
| `GET` | `/login` | R | Нет | Sign in with GitHub |
| `GET` | `/auth/github` | R | Нет | OAuth start; `?redirect=` |
| `GET` | `/auth/github/callback` | R | Нет | OAuth callback |
| `GET` | `/logout` | R | Session | Logout |
| `GET` | `/dashboard` | R | — | 301 → `/use` |
| `GET` | `/models` | X | — | Удалить или 301 → `/use` (нет public catalog) |
| `GET` | `/health` | R | Нет | `OK` |
| `GET` | `/install-provider.sh` | R | Нет | Install script |
| `GET` | `/static/*` | R | Нет | CSS/JS; новый Light calm skin |

### 7.2 Inference API

| Метод | Путь | Статус | Auth | Описание v2 |
|---|---|---|---|---|
| `GET` | `/v1/models` | R* | API key | Discovery моделей по owned+bindings (+ `machine_id`) |
| `GET` | `/v1/machines/{machine_id}/models` | N | API key | Модели одной машины |
| `POST` | `/v1/machines/{machine_id}/chat/completions` | N | API key | Completion на машину |
| `POST` | `/v1/chat/completions` | X→410 | API key | Явная ошибка миграции |
| `OPTIONS` | `/v1/*` | R | Нет | CORS preflight |

### 7.3 Keys и stats API

| Метод | Путь | Статус | Auth | Описание v2 |
|---|---|---|---|---|
| `POST` | `/api/keys` | R | OAuth | Создать ключ (scope) |
| `GET` | `/api/keys` | R | OAuth | Список ключей |
| `DELETE` | `/api/keys/{id}` | R | OAuth | Revoke key |
| `POST` | `/api/keys/{id}/regenerate` | R | OAuth | Regenerate provider key (**меняет machine_id**) |
| `GET` | `/api/consumer/stats` | R | OAuth | Usage stats |
| `GET` | `/api/owner/stats` | N (was `/api/donor/stats`) | OAuth | Owner lifetime stats |
| `GET` | `/api/owner/status` | N (was `/api/donor/status`) | OAuth | Online machines для owner |
| `POST` | `/api/report` | R | API key | Жалоба; в v2 привязана к machine_id если возможно |

### 7.4 Invites / bindings API (new)

| Метод | Путь | Статус | Auth | Описание |
|---|---|---|---|---|
| `POST` | `/api/invites` | N | OAuth | Создать invite на `machine_id` → PIN plaintext один раз |
| `GET` | `/api/invites` | N | OAuth | Список invites owner’а |
| `DELETE` | `/api/invites/{id}` | N | OAuth | Revoke invite |
| `POST` | `/api/join` | N | OAuth | Redeem `{ "pin": "XXXX-XXXX" }` |
| `GET` | `/api/bindings` | N | OAuth | Список машин, доступных user’у (owned + member) |
| `DELETE` | `/api/bindings/{machine_id}` | N | OAuth | Member: убрать свой binding; Owner: revoke member через отдельный путь ниже |
| `DELETE` | `/api/machines/{machine_id}/members/{user_id}` | N | OAuth | Owner revoke access member’а |

Допустимы HTMX-обёртки тех же операций на `/share/*` и `/join` (см. §9).

### 7.5 WebSocket

| Путь | Статус | Auth | Описание |
|---|---|---|---|
| `/ws/provider` | R | Provider token query | Агент; `registered` включает `machine_id` |

### 7.6 HTMX-фрагменты

| Метод | Путь | Статус | Описание v2 |
|---|---|---|---|
| `GET/POST` | `/use/keys` | R | Keys UI |
| `GET` | `/use/machines` | N | Список bindings + setup panel (`?setup=` opens curl); polling ~10s |
| `GET` | `/share/panel` | N | Progressive owner surface (§9.5): token / waiting / online / invite |
| `POST` | `/share/tokens` | R | Create provider token (modal / fragment) |
| `POST` | `/share/invites` | N | Create invite → PIN modal (plaintext once) |
| `GET` | `/share/members` | N | Members + revoke (polling ~30s) |
| `POST` | `/join` | N | HTMX redeem PIN → result fragment |
| `GET` | `/share/setup` · `/share/models` · `/share/stats` | R→alias | Legacy paths → тот же handler, что `/share/panel` |

Удалены community-фрагменты v1: `/use/donor`, `/share/donor-stats`.

---

## 8. Алгоритм обработки chat completion

```
Вход: machine_id, API key, body.model, stream?

1. Аутентифицировать API key → user_id
2. Авторизовать доступ к machine_id (owner или binding)
3. Rate limit check
4. Нормализовать имя модели (strip provider prefix до первого `/`, если нужно)
5. Найти runtime session машины:
   - нет online WS → 503 machine_offline
   - backend_ok == false → 503 backend_unavailable
   - current_load >= max_concurrent → 503 machine_busy
6. Если model нет в session.models → 404 model_not_found
   (не искать модель на других machines)
7. Отправить WS request провайдеру
8. Релей chunk/response/error клиенту
9. При ошибке non-stream: retry до 3 раз только на ту же session/machine
```

Удалено из v1: выбор ноды по load, affinity TTL across machines, `available_models` из чужого пула как primary UX.

---

## 9. Веб-интерфейс (Light calm)

### 9.0 Visual source of truth

| Артефакт | Роль |
|---|---|
| [`docs/visual-v2-screens.html`](docs/visual-v2-screens.html) | **Утверждаемые экраны, статусы и переходы** (кликабельный прототип: сайдбар → screen/status, CTAs в превью) |
| [`docs/visual-v2-sketch.html`](docs/visual-v2-sketch.html) | Ранний moodboard Light calm (не карта статусов) |

Реализация UI в `web/templates/` должна совпадать со screens-прототипом по IA, копирайту и визуальным статусам ниже. При расхождении — сначала обновить screens + этот §9, потом код.

Общий chrome (nav / footer / head / privacy notice / scripts) — в **`web/templates/chrome.html`**; страницы подключают его через `{{template …}}`. Менять chrome в одном месте, не копипастить по страницам.

**Карта экранов в прототипе:** Home · Join · Share · Use · About · Login · Errors (404/500/503).  
**Auth chrome:** Logged out / Logged in переключается в прототипе и влияет на дефолтный статус экрана.

### 9.1 Design system

Опирается на Light calm (токены как в screens-прототипе):

| Токен | Значение |
|---|---|
| Background | `#F7F7F5` |
| Panel | `#FFFFFF` |
| Ink | `#1C1917` |
| Muted | `#78716C` |
| Line | `#E7E5E4` |
| Accent | `#0F766E` (teal) |
| OK | `#15803D` |
| Font UI | IBM Plex Sans (или эквивалент; не Inter-default stack) |
| Font display (hero) | Source Serif 4 (или близкий serene serif) |
| Font mono | IBM Plex Mono — PIN, keys, URLs, code |
| Radius | ~12–20px, спокойные карточки |
| Buttons | pill / soft rectangle, primary = ink fill |

Принципы:

- Без ASCII-баннеров и «terminal CRT»
- PIN — главный визуальный артефакт (крупный mono, «boarding pass»)
- На лендинге PIN в hero — **пример / illustration**, не живой invite (не генерировать запись в БД при `GET /`)
- Mono только для кода/PIN/ключей
- Язык UI: **English** (как v1)
- HTMX + полные HTML-страницы (без SPA)
- Polling-фрагменты (`/use/machines`, `/share/panel`, `/share/members`) **не должны сбрасывать** открытые `<details>`, значения форм и выбранный snippet при swap — иначе UI «сам закрывается» каждые ~10s
- Один job на секцию; без dashboard-каши из community stats
- Копирайт простой: friends / coworkers / local models — без «neurobullshit» (tunnel/mesh манифесты, «someone’s», jargon вроде TTL/bindings в user-facing тексте)

Предупреждение privacy (обязательно в v2): на `/join` и `/use` — короткий notice в духе: *Your prompts go through the GPU Mesh server and are visible to whoever runs the machine.*

### 9.2 Навигация

- Logo «GPU Mesh» → `/`
- Links: Home, Join, Use, Share, About
- Logged out: Sign in with GitHub
- Logged in: `@login` + Logout

Footer: GitHub repo, «MIT», tagline: **Share local models with friends** (не «Powered by community»).

### 9.3 Landing `/`

| # | Компонент | Содержание |
|---|---|---|
| 1 | Hero | Serif H1: «Share your local models with friends». Sub: send a PIN to a coworker/friend; they point Continue/Cline/curl at your machine URL. CTA: Create invite → `/share`, Enter a code → `/join` |
| 2 | Example invite card | Демо-PIN (не из БД). CTA ведёт в owner-флоу (`/share` → auth при необходимости), не копирует «живой» код |
| 3 | How it works | ① Run the agent ② Share a PIN ③ They use your URL (`/v1/machines/{id}`) |
| 4 | What you get | Коротко и по делу: только друзья с PIN; revoke anytime; без открытого порта и public catalog |

**Нет:** Models online / Nodes online / Requests today как community proof.  
Опционально позже: «Your machines online» только для logged-in.

### 9.4 `/join`

Статусы (см. прототип):

| Status | UI |
|---|---|
| Logged out · empty | PIN field + Sign in to connect (OAuth `redirect=/join` или `/join?pin=…`) |
| Logged out · `?pin=` | PIN prefilled + Sign in to connect |
| Logged in · form | PIN field (пустое, если не из query) + Connect |
| Success | Machine name + online, link to `/use`, one-time API key banner если ключ только что создан |
| Errors | Человекочитаемые сообщения из §4.4 (`invalid_pin`, `expired`, `exhausted`, `revoked`, `machine_gone`, `rate_limited`) — **без** сырого `code:` в UI |

Privacy notice обязателен.

### 9.5 `/share` (Owner) — progressive single-surface

Единственный control plane owner’а в MVP: provider token, setup/run command, machine online/models, Create invite, members/revoke, stats. Агент эти функции не дублирует (§11.0).

Не четыре равноправные вкладки. Одна страница: **состояние диктует, что видно**. Цель — минимум действий до PIN (повторный шаринг ≈ Create invite → Copy).

| State | Что на экране | Primary action |
|---|---|---|
| Logged out | Hero «Share your local models» + Sign in | Sign in |
| No provider token | Generate provider token + короткий why | Generate token |
| Token · waiting | «Waiting for provider…» + Setup (OS tabs) + Create invite disabled | Copy run command |
| Online · ready | Machine strip + Create invite (uses/TTL перед созданием) + Members | Create invite |
| Online · 2+ machines | Компактный select машины над CTA (без отдельной секции Machines) | Create invite |
| Machine offline | Warning; invite всё ещё можно создать (§4.2) | Create invite |
| PIN modal | Boarding-pass PIN + Copy code + Copy link + meta; shown once | Copy |
| Revoke confirm | Confirm dialog для member / provider token | Revoke / Cancel |
| Empty members | Одна строка empty, без большой пустой карточки | — |

**Setup & provider token** (waiting card + свёрнуто после ready):

- OS tabs: **macOS / Linux** | **Windows** — install + run с `YOUR_PROVIDER_TOKEN` (полный token только в Generate / Regenerate modal).
- macOS/Linux: `curl …/install-provider.sh \| sh` + `export MESH_*` + `gpumesh-provider`.
- Windows: PowerShell download `gpumesh-provider_windows_amd64.zip` с GitHub Releases + `$env:MESH_*` + `.\gpumesh-provider.exe`.
- Token prefix + **Revoke provider token** (key revoked → machine retired → UI → no-token) + **Regenerate** (новый machine URL + one-time token modal). Отдельный Advanced collapse не нужен.

После ready: **Invites** (список; active → **Revoke invite**; секция открыта, если есть active), **Setup & provider token**.

Автовыбор: 1 machine → без пикера; 2+ → select.

Polling: machines ~10s, members ~30s.

### 9.6 `/use` (Member / self)

Logged out: pitch + Sign in / Enter a code — job language: set up curl or an editor (base URL + API key).  
Logged in:

1. **Machines** — cards: name, owner (`owned by you` / `@login`), online/offline, models.
   - Primary: **Set up a tool** → setup panel with tabs **curl (default) / Continue / Cline / Python**. Snippet includes machine `BASE_URL` (`/v1/machines/{id}`), API key (live one-time key while banner visible, else `YOUR_API_KEY`), and model. Short one-liner: paste into the tool (curl: run as-is).
   - Secondary: **Copy base URL**; Member: **Remove access**.
   - After successful join: land on `/use?setup={machine_id}` — that card highlighted, setup panel open on curl.
2. **API Keys** — list / create / one-time key banner / empty (без jargon `scope:` в основном UI — «for tools» / «for provider»). Not required for happy path when one-time key banner is shown.
3. Empty: «No machines yet — ask a friend for a PIN, or share your own models.»

Убрать таб «browse all community models». Privacy notice обязателен. UI не завязан на один харнес (Continue) — curl равноправен.

### 9.7 `/about`

Invite-first: local models + PIN для friends/coworkers; Owner / Member; privacy; FAQ (PIN one-time? GitHub? vs Tailscale? offline?).

### 9.8 `/login` и ошибки HTML

`/login`: Sign in with GitHub; edge — OAuth unset warning (простым языком).  
404 / 500 / 503 — простые Light calm страницы + Go home.

---

## 10. Модель данных

### 10.1 Существующие сущности (reused)

| Сущность | Поля (сжато) |
|---|---|
| `users` | id, github_id, github_login, created_at |
| `api_keys` | id, user_id, key_hash, key_prefix, scope (`consumer` \| `provider` \| `both`), created_at, revoked_at |
| `sessions` | token, user_id, created_at, expires_at |
| `owner_stats` | user_id, total_requests, total_tokens, total_uptime_seconds, last_seen_at (миграция с `donor_stats`) |

### 10.2 Новые сущности

**`machines`**

| Поле | Тип | Описание |
|---|---|---|
| `id` | TEXT PK | `mch_...` стабильный id |
| `owner_user_id` | INT | Owner |
| `provider_key_id` | INT UNIQUE | FK на api_keys (scope `provider`/`both`) |
| `display_name` | TEXT | Из description/hostname |
| `created_at` | TEXT | |
| `updated_at` | TEXT | |

Runtime online/models/load — в памяти, не обязательно дублировать в SQLite на каждый heartbeat (допустимо кэш last_seen в machines).

**`invites`**

| Поле | Тип | Описание |
|---|---|---|
| `id` | INTEGER PK | |
| `machine_id` | TEXT | FK machines |
| `owner_user_id` | INT | |
| `pin_hash` | TEXT UNIQUE | SHA-256 |
| `pin_prefix` | TEXT | Для UI mask |
| `max_uses` | INT | default 1 |
| `uses` | INT | default 0 |
| `expires_at` | TEXT | |
| `revoked_at` | TEXT NULL | |
| `created_at` | TEXT | |
| `label` | TEXT NULL | optional «for alice» |

**`bindings`**

| Поле | Тип | Описание |
|---|---|---|
| `id` | INTEGER PK | |
| `machine_id` | TEXT | |
| `member_user_id` | INT | |
| `invite_id` | INT NULL | откуда пришли |
| `created_at` | TEXT | |
| `revoked_at` | TEXT NULL | |
| UNIQUE(machine_id, member_user_id) | | один active row; revoke = set revoked_at; повторный redeem той же пары — reactivate или no-op |

### 10.3 Что не хранится

Промпты, ответы, IP (как в v1), plaintext PIN после показа, plaintext API keys после создания.

### 10.4 In-memory registry (v2)

```
machines_runtime[machine_id] = {
  machine_id, owner_user_id, provider_key_hash,
  ws_conn?, models[], max_concurrent, current_load,
  description, hardware, backend_ok,
  connected_at?, last_heartbeat?,
  session_requests, session_tokens
}
```

Индекс `model_index` по всему пулу **не используется** для маршрутизации consumer-запросов. Может остаться вспомогательным для owner UI.

---

## 11. Агент провайдера (`gpumesh-provider`)

### 11.0 Граница scope: daemon ≠ control plane

Агент — лёгкий демон рядом с Ollama: thin wizard → WebSocket → proxy. **Control plane owner’а** (provider token CRUD, online/models/load, invites, members, stats) живёт **только** на веб `/share` (§9.5). Дублировать кабинет в CLI — два источника правды (токены/реестр на координаторе + OAuth) и ломает headless-кейсы.

| В агенте (MVP) | НЕ в агенте |
|---|---|
| Wizard один раз: paste provider token, Ollama URL, опциональный `--models` | Create / regenerate / revoke токенов |
| Авто-детект моделей + heartbeat + reconnect + proxy | Live model picker, pull models UI, TUI / local web UI |
| Понятные логи: connected, `machine_id`, models[], errors | Дашборд статуса машин, members, invites, badge |
| Конфиг `~/.gpumesh.json` | Device-login / OAuth в CLI; синхронизация конфига с кабинетом |

Источник правды «онлайн ли машина / какие модели» — реестр координатора, UI на `/share`. Локальный one-shot `gpumesh-provider status` (ollama ok / last error) — допустим пост-MVP, не замена `/share`.

### 11.1 Почти без изменений

Порядок запуска, конфиг `~/.gpumesh.json`, автодетект Ollama, heartbeat, reconnect backoff, proxy к `/api/chat` — как в v1 §4 (в рамках границы §11.0).

### 11.2 Изменения

- После `registered` агент может залогировать `machine_id` (для человека в `/share`).
- Copy в wizard/README: «get provider token at `/share`», не «join the public mesh».
- Default coordinator URL без изменения концепта (`MESH_COORDINATOR`).

### 11.3 Дистрибуция

`go install`, GitHub Releases, `/install-provider.sh` — reused.

---

## 12. Защита от злоупотреблений и privacy

### 12.1 PIN / auth

- GitHub OAuth обязателен для redeem и dashboard
- PIN brute-force: rate limit по IP и по user (например 10 попыток / 15 мин)
- PIN entropy: 8 символов из ~30 → достаточный для short-lived invites при rate limit
- Invite max_uses и TTL по умолчанию жёсткие

### 12.2 Inference

- Rate limit на API key
- Owner revoke binding мгновенно режет доступ
- Нет публичного доступа без binding/ownership

### 12.3 Privacy / trust

- Промпты видны owner’у машины (Ollama на его хосте) и могут быть видны оператору релея
- UI обязан показывать warning
- Контент не пишется в SQLite
- Self-host координатора — путь для параноиков

### 12.4 Reports

`POST /api/report` сохраняется как сигнал; ручная модерация. Менее критично, чем в public mesh, но полезно при злоупотреблении invite’ом.

---

## 13. Конфигурация координатора

| Переменная | Default | Описание |
|---|---|---|
| `MESH_ADDR` | `:8080` | HTTP listen |
| `MESH_DB` | `data/gpumesh.db` | SQLite |
| `MESH_BASE_URL` | `http://localhost:8080` | Внешний URL (для join links / snippets) |
| `MESH_RATE_LIMIT` | `100` | Req/hour на API key |
| `MESH_INVITE_TTL_DAYS` | `7` | Default invite TTL |
| `MESH_INVITE_MAX_USES` | `1` | Default max uses |
| `MESH_PIN_ATTEMPT_LIMIT` | `10` | Попыток redeem / окно |
| `MESH_INSTALL_SCRIPT_DOWNLOAD_BASE` | (как v1) | Releases base |
| `MESH_AFFINITY_TTL` | — | **Удалить / игнорировать** в v2 |

OAuth secrets — как в v1 (env).

---

## 14. Объём MVP v2

### 14.1 Входит

- [ ] Таблица `machines`, `invites`, `bindings` + миграции
- [ ] Стабильный `machine_id` на provider key
- [ ] Create/list/revoke invites + PIN UI
- [ ] `/join` redeem + GitHub
- [ ] Multi-binding для member
- [ ] `GET/POST /v1/machines/{id}/models|chat/completions`
- [ ] `GET /v1/models` discovery с `machine_id`
- [ ] `410` на legacy `POST /v1/chat/completions`
- [ ] Hard pin routing (no cross-machine failover)
- [ ] Owner self-access без PIN
- [ ] Light calm redesign ключевых страниц (`/`, `/join`, `/share`, `/use`, `/about`)
- [ ] Privacy warning в UI
- [ ] WS `registered` возвращает `machine_id`
- [ ] Документация README под v2

### 14.2 Не входит

- [ ] WebRTC / direct P2P
- [ ] Public pool / leaderboard / thanks
- [ ] vLLM и др. бэкенды
- [ ] Очередь запросов (только 503 busy)
- [ ] Approve-before-join workflow
- [ ] PIN без GitHub
- [ ] Browser-only chat без API key
- [ ] Биллинг
- [ ] Менеджмент токенов / статус моделей / кабинет owner’а в `gpumesh-provider` (только `/share`, §11.0)
- [ ] TUI или локальный web UI агента
- [ ] Device-login / OAuth flow внутри CLI провайдера

---

## 15. Будущее (не спецификация MVP)

1. **WebRTC family link** — signaling-only coordinator, промпты мимо релея при direct.
2. **Guest hour** — поверх family binding эфемерный guest PIN на час.
3. **Household policies** — quiet hours, per-member token caps, fair queue.
4. **Плагины Continue/Open WebUI** — «Connect machine» one-click.
5. **Opt-in public discovery** — только если появится спрос; не default.

---

## 16. Миграция с v1

- Нет обязательства сохранять данные community affinity / ожидания public `/models`.
- Существующие API keys можно оставить в БД; семантика доступа меняется (нужны bindings / ownership).
- Provider keys при первом коннекте после деплоя v2 получают строку `machines`.
- Scope в БД: значения `donor` мигрировать в `provider` (или принимать оба на переходном шаге только в коде миграции, в продукте — только `provider`).
- Рекомендация оператору: backup DB, задеплоить v2, прогнать сценарий owner→PIN→member→harness.
- `SPEC.md` остаётся историческим описанием v1; **продуктовый source of truth для новой разработки — `SPEC-v2.md`**.

---

## 17. Критерии готовности к статье на Хабр

Статья: **пошарить GPU другу → PIN → подключить харнес**.

Готово к публикации, когда воспроизводимо:

1. **Owner:** provider online, видит `machine_id`, создаёт PIN, копирует join-link.
2. **Friend (второй GitHub):** redeem PIN на `/join`, переходит на `/use?setup={machine_id}`, открывает setup (curl по умолчанию или другой инструмент), копирует snippet с `OPENAI_BASE_URL=/v1/machines/{id}` и API key.
3. **Харнес:** любой OpenAI-compatible клиент (Continue, Cline, Aider, curl, Python SDK, …) с base URL + key + именем модели как в Ollama; completion реально идёт с машины owner’а.
4. Provider restart → тот же `machine_id`, харнес без смены base URL продолжает работать.
5. Revoke binding → харнес получает отказ (403), без «тихого» hop на другую машину.
6. Offline machine → предсказуемый 503.
7. В UI есть короткий privacy warning (промпты видит owner / релей).

В статье достаточно **одного** конкретного харнеса как примера (на выбор автора); продукт и UI не завязаны на Continue.

Структура статьи (ориентир):

1. Зачем не ngrok/Tailscale именно для LLM-харнеса  
2. Owner: Ollama + provider + PIN  
3. Friend: GitHub + PIN + ключ  
4. Харнес: base URL + key + model  
5. Ограничения (релей, trust, offline)

После статьи — по желанию кросс-пост; это не блокер.

---

## 18. Краткая шпаргалка для реализации

```
PIN        → только join (GitHub + redeem)
API key    → все /v1 запросы
machine_id → в URL path; стабилен на provider key
model      → имя Ollama без маппинга
routing    → только эта machine; иначе ошибка
UI         → Light calm; PIN boarding-pass; без слова donor
pool       → нет
roles      → Owner / Member / Provider (agent) / Operator
```
