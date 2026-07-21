# GPU Mesh — Спецификация продукта

> **Статус:** Черновик для разработки  
> **Лицензия:** MIT  
> **Стек:** Go (координатор + агент, единый бинарник на компонент)

---

## 1. Обзор продукта

### 1.1 Что это

Пиринговая сеть для распределённого LLM-инференса. Владельцы GPU («доноры») делятся свободными вычислительными ресурсами с теми, кому нужен бесплатный доступ к LLM («потребители»). Координатор — лёгкий реестр + релей, который делает этот процесс прозрачным для обеих сторон.

### 1.2 Ценностное предложение

| Роль | Проблема | Решение |
|---|---|---|
| **Потребитель** | Нет GPU, нет денег на API | Бесплатный инференс, OpenAI-совместимый API — подключается к любому существующему инструменту |
| **Донор** | GPU простаивает 90% времени | Одна команда — и железо работает на сообщество; репутация, таблица лидеров |

### 1.3 Ключевое отличие: нулевая интеграция для потребителя

Потребители используют **любой существующий OpenAI-совместимый инструмент** — Continue.dev, Codex CLI, Aider, Open WebUI, LangChain, любой SDK. Не нужно ставить новый клиент. Не нужно учить новый API. Две переменные окружения — и всё работает.

---

## 2. Роли пользователей

### 2.1 Потребитель (Consumer)

- Хочет бесплатного доступа к LLM для кодинга, текстов, исследований
- Использует существующие инструменты (VS Code, терминал, чат-интерфейсы)
- Может иметь или не иметь GPU
- Может одновременно быть донором

### 2.2 Донор (Donor)

- Имеет GPU со свободной мощностью
- Имеет запущенный Ollama с одной или несколькими моделями
- Готов делиться вычислениями ради репутации или альтруизма
- Хочет минимальной настройки и нулевого сопровождения

### 2.3 Администратор (Coordinator Operator)

- Запускает публичный экземпляр координатора
- Управляет защитой от злоупотреблений, мониторит здоровье, задаёт глобальные лимиты

---

## 3. Архитектура

```
                         ┌─────────────────────┐
   Continue.dev ──HTTP──▶│                     │◀──WS─── Агент донора ──▶ Ollama
   Codex CLI    ──HTTP──▶│   Координатор        │◀──WS─── Агент донора ──▶ Ollama
   Aider        ──HTTP──▶│  (Публичный VPS)     │◀──WS─── Агент донора ──▶ Ollama
   Open WebUI   ──HTTP──▶│                     │
   Любой SDK    ──HTTP──▶│  Реестр + Релей     │
                         │  + Веб-дашборд       │
                         └─────────────────────┘
```

### 3.1 Сетевая модель

**WS Relay.** Доноры открывают исходящий WebSocket к координатору и держат его открытым. Координатор отправляет запросы на инференс через тот же сокет. Проблема NAT решается тривиально — все соединения исходящие со стороны донора.

HTTP-запросы потребителей приходят на координатор, который маршрутизирует их к свободному донору и стримит ответ обратно потребителю как SSE (или возвращает JSON для не-стримингового режима).

### 3.2 Компоненты

| Компонент | Бинарник | Описание |
|---|---|---|
| **Координатор** | `gpumesh-coordinator` | HTTP + WS сервер, реестр моделей, релей запросов, rate limiter, веб-дашборд |
| **Агент донора** | `gpumesh-provider` | Лёгкий агент рядом с Ollama, подключается к координатору, проксирует запросы |
| **Веб-дашборд** | (встроен в координатор) | Лендинг, управление API-ключами, статистика донора, доступность моделей |

### 3.3 Протокол Потребитель ↔ Координатор

**OpenAI-совместимый HTTP API.**

```
GET  /v1/models              — Список доступных моделей
POST /v1/chat/completions    — Chat completion (стриминг и не-стриминг)
```

Точные OpenAI-схемы запросов и ответов. Любой OpenAI SDK или совместимый инструмент работает через установку `OPENAI_BASE_URL` и `OPENAI_API_KEY`.

Ответ `GET /v1/models`:

```json
{
  "object": "list",
  "data": [
    {
      "id": "llama3.2:3b",
      "object": "model",
      "owned_by": "community",
      "donors_online": 12,
      "load": 0.3
    }
  ]
}
```

Дополнительные поля (`donors_online`, `load`) — не ломают совместимость, OpenAI-клиенты игнорируют неизвестные поля.

### 3.4 Протокол Координатор ↔ Донор
**JSON-сообщения поверх WebSocket. Все сообщения содержат обязательное поле `type`.**

#### Донор → Координатор

| Сообщение | Поля | Когда |
|---|---|---|
| `{"type": "register", ...}` | `models[]`, `max_concurrent`, `description` | При подключении |
| `{"type": "heartbeat"}` | — | Каждые 30с |
| `{"type": "chunk", ...}` | `request_id`, `content`, `done` | Токен стримингового ответа |
| `{"type": "response", ...}` | `request_id`, `content`, `model`, `usage` | Полный не-стриминговый ответ |
| `{"type": "error", ...}` | `request_id`, `code`, `message` | Отказ бэкенда, таймаут, и т.д. |

#### Координатор → Донор

| Сообщение | Поля | Когда |
|---|---|---|
| `{"type": "registered", ...}` | `provider_id` | После успешной регистрации |
| `{"type": "request", ...}` | `request_id`, `model`, `messages[]`, `stream`, `options` | Потребитель хочет инференс |
| `{"type": "cancel", ...}` | `request_id` | Потребитель отключился или таймаут |
| `{"type": "heartbeat_ack"}` | — | Ответ на heartbeat |

Где `options` — опциональный объект, напрямую транслируемый в параметры Ollama `/api/chat` (например `{"temperature": 0.7, "top_p": 0.9}`).

#### Коды ошибок

| Код | Значение |
|---|---|
| `backend_unavailable` | Ollama недоступен на localhost |
| `model_not_found` | Запрошенная модель не загружена у этого донора |
| `timeout` | Ollama слишком долго отвечает |
| `overloaded` | Донор на максимальной загрузке |
| `internal` | Прочее |

### 3.5 Алгоритм маршрутизации запросов

```
Вход: имя модели
1. Запросить реестр: все доноры с model == запрошенная модель AND backend_ok == true
2. Если пусто → 503, тело: {"error": "Модель недоступна", "available_models": ["llama3.2:3b", ...]}
3. Отсортировать по: current_load / max_concurrent по возрастанию (наименее загруженные сначала)
4. Выбрать первого донора с current_load < max_concurrent
5. Если все доноры на максимуме → 503, тело: {"error": "Все доноры заняты", "retry_after_seconds": 30}
6. Отправить запрос → донору
7. При ошибке/таймауте донора: для стриминга — закрыть SSE (потребитель повторит запрос); для не-стриминга — прозрачно переиграть на другом доноре (до 3 попыток), только после исчерпания попыток вернуть 502.
```

### 3.6 Поток стримингового релея

```
Consumer                     Coordinator                        Donor
  │                              │                                 │
  │── POST /v1/chat/completions─▶│                                 │
  │   {stream: true}             │── WS: request {req_id, ...} ───▶│
  │                              │                                 │── POST /api/chat ▶ Ollama
  │                              │◀──── WS: chunk {req_id, ...} ───│◀─── NDJSON chunk ───
  │◀── SSE: data: {chunk} ───────│                                 │
  │                              │◀──── WS: chunk {req_id, ...} ───│◀─── NDJSON chunk ───
  │◀── SSE: data: {chunk} ───────│                                 │
  │                              │◀──── WS: chunk {done:true} ─────│
  │◀── SSE: data: [DONE] ────────│                                 │
```

Координатор преобразует формат NDJSON-чанков Ollama в формат SSE-чанков OpenAI. Это тонкий адаптер формата, а не тяжёлая трансформация.

### 3.7 Таймауты на стороне координатора

| Таймаут | Значение | Действие при превышении |
|---|---|---|
| **TTFT** (время до первого токена) | 15 секунд | Отменить запрос донору (`cancel`), закрыть SSE, потребитель повторяет запрос → другой донор |
| **Межтокеновый** (пауза между чанками) | 10 секунд | То же, что и TTFT |
| **Общий таймаут запроса** | 120 секунд | Принудительно завершить. Если есть токены — вернуть что сгенерировано + `finish_reason: "length"`. Если 0 токенов — закрыть SSE/вернуть 502 |

### 4.1 Порядок запуска

```
1. Прочитать конфиг (URL координатора, токен, URL Ollama, max_concurrent)
2. Если нет токена → вывести "No token. Get one at https://gpumesh.io/dashboard" и выйти с ошибкой
3. Подключить WS к координатору
4. Обнаружить локальные модели: GET http://localhost:11434/api/tags
5. Отправить register с обнаруженными моделями
6. Запустить heartbeat-тикер (интервал 30с)
7. Войти в цикл обработки запросов
```

### 4.2 Конфигурация

| Флаг | Переменная окружения | По умолчанию | Описание |
|---|---|---|---|
| `--coordinator` | `MESH_COORDINATOR` | `wss://gpumesh.io/ws/provider` | WS URL координатора |
| `--token` | `MESH_TOKEN` | — | Токен аутентификации донора |
| `--ollama-url` | `MESH_OLLAMA_URL` | `http://localhost:11434` | Базовый URL Ollama |
| `--models` | `MESH_MODELS` | (все обнаруженные) | Белый список моделей для шаринга. Comma-separated: `llama3.2:3b,codellama:7b` |
| `--description` | `MESH_DESCRIPTION` | hostname | Публичное описание (напр. "RTX 4090, US-East") |

### 4.3 Обработка запроса

```
При получении request:
1. Если current_load >= max_concurrent → ответить error: overloaded
2. Увеличить current_load
3. Создать контекст с отменой (context.WithCancel)
4. Вызвать Ollama POST /api/chat {model, messages, stream, options} с этим контекстом
5. Для стриминга: читать NDJSON-чанки, пересылать каждый как chunk
6. Для не-стриминга: прочитать полный ответ, переслать как response
7. При любой ошибке: переслать error с подходящим кодом
8. Уменьшить current_load

При получении cancel с тем же request_id:
→ Вызвать cancel() контекста, что прервёт HTTP-запрос к Ollama
```

### 4.4 Переподключение

- При обрыве WS: экспоненциальный backoff (1с → 2с → 4с → … → макс 60с) со случайным jitter
- При переподключении: полная перерегистрация (модели могли измениться)
- Активные запросы на момент обрыва теряются (координатор отвечает за повтор)

---

## 5. Координатор (`gpumesh-coordinator`)

### 5.1 HTTP-эндпоинты

| Метод | Путь | Аутентификация | Описание |
|---|---|---|---|
| `GET` | `/` | Нет | Лендинг |
| `GET` | `/v1/models` | API-ключ | Список доступных моделей |
| `POST` | `/v1/chat/completions` | API-ключ | Chat completion |
| `POST` | `/api/keys` | GitHub OAuth | Создать API-ключ |
| `GET` | `/api/keys` | GitHub OAuth | Список ключей пользователя |
| `DELETE` | `/api/keys/:id` | GitHub OAuth | Отозвать API-ключ |
| `GET` | `/api/donor/stats` | Токен донора | Статистика донора |
| `GET` | `/api/status` | Нет | Глобальная статистика (модели онлайн, доноры, аптайм) |
| `GET` | `/dashboard` | GitHub OAuth | Страница веб-дашборда |

Аутентификация:
- **API-ключ потребителя:** заголовок `Authorization: Bearer <key>`. Аналогично OpenAI.
- **GitHub OAuth:** сессионная cookie после логина через GitHub.
- **Токен донора:** API-ключ с scope `donor` или `both`, передаётся в WS как query-параметр `?token=<key>` при подключении.

**CORS:** Все эндпоинты `/v1/*` возвращают заголовки `Access-Control-Allow-Origin: *` и `Access-Control-Allow-Headers: Authorization, Content-Type`. Необходимо для работы из браузерных инструментов (Open WebUI, LobeChat).

**TLS:** В продакшене координатор работает за reverse-proxy (nginx/Caddy) с TLS. В разработке (`localhost`) агент донора подключается по `ws://`, а не `wss://`.
Дополнительные эндпоинты:
| Метод | Путь | Аутентификация | Описание |
|---|---|---|---|
| `GET` | `/health` | Нет | Liveness/readiness probe |
| `POST` | `/api/report` | API-ключ | Жалоба на ответ донора: `{"request_id": "...", "reason": "spam"}` |
| `GET` | `/api/consumer/stats` | GitHub OAuth | Статистика потребителя: requests/tokens сегодня, остаток лимита |
| `GET` | `/api/donor/status` | GitHub OAuth | Живой статус агентов донора (online, models, load) |
| `GET` | `/leaderboard/data` | Нет | Данные таблицы лидеров: `?period=weekly&limit=50` |
| `GET` | `/models/data` | Нет | Данные каталога моделей для динамического обновления |

Полный список фронтенд-эндпоинтов (включая HTMX-фрагменты) — см. §6.8.

### 5.2 WebSocket-эндпоинт

| Путь | Аутентификация | Описание |
|---|---|---|
| `/ws/provider` | Токен донора | Подключение агента донора |

### 5.3 Реестр (структура данных в памяти)

```
registry = {
  donors: Map<provider_id, {
    provider_id:      string             // генерируется координатором при регистрации
    user_id:          string             // извлечён из API-ключа при аутентификации WS
    models:           string[]           // напр. ["llama3.2:3b", "codellama:7b"]
    max_concurrent:   int
    current_load:     int
    description:      string
    connected_at:     timestamp
    last_heartbeat:   timestamp
    backend_ok:       bool               // false если Ollama ответил ошибкой
    session_requests: int                // счётчик с момента подключения
    session_tokens:   int
    avg_tokens_per_sec: float
    ws_conn:          *websocket.Conn
  }>

  model_index: Map<model_name, Set<provider_id>>  // обратный индекс для быстрого поиска
}
```

Связь с пользователем: при WS-подключении координатор валидирует токен из query-параметра, находит соответствующий API-ключ в SQLite и извлекает `user_id`. Этот `user_id` сохраняется в записи донора и используется для персистентной статистики в `donor_stats` (§7.1). `provider_id` генерируется случайно и уникален для каждого подключения — один пользователь может иметь несколько одновременных подключений с разных машин.

Счётчики `session_requests` и `session_tokens` — runtime, обнуляются при дисконнекте. При корректном отключении донора (graceful close) сессионные счётчики прибавляются к персистентным в SQLite (§7.1). При обрыве соединения сессионные счётчики теряются — приемлемо для MVP.

### 5.4 Мониторинг здоровья (сторона координатора)

- Таймаут heartbeat: если от донора нет heartbeat за 90 секунд → удалить из реестра
- Здоровье бэкенда: если донор сообщил `backend_unavailable` → пометить `backend_ok = false`, WS оставить открытым
- Донор с `backend_ok = false` дольше 5 минут → отключить и удалить
- При любом удалении донора из реестра: все активные запросы к нему отменяются, SSE-соединения с потребителями закрываются (потребители повторят запрос → другой донор)

### 5.5 Ограничение частоты запросов (Rate Limiting)

- На API-ключ: настраиваемый лимит (по умолчанию: 100 запросов/час)
- Реализация: token bucket
- Заголовок `X-RateLimit-Remaining` возвращается в каждом ответе
- При превышении: 429 + заголовок `Retry-After`

### 5.6 Управление API-ключами

- Потребитель создаёт ключи через GitHub OAuth на веб-дашборде
- **Полный ключ показывается только один раз** — при создании. После этого доступен только префикс (первые 8 символов) для идентификации
- Ключи имеют scope: `consumer` (использование моделей), `donor` (регистрация как донор), или `both`
- Ключи можно отозвать
- Ключи хранятся в виде хеша (SHA-256)

---

## 6. Веб-интерфейс — поэкранная спецификация

### 6.0 Общие элементы

**Навигация (хедер):**
- Логотип + название (ссылка на `/`)
- Для неавторизованных: кнопка «Login with GitHub»
- Для авторизованных: аватар + GitHub username (выпадающее меню: Dashboard, Models, Leaderboard, Status, Logout)
- Ссылки: Models, Leaderboard, Status

**Футер:**
- Ссылка на GitHub-репозиторий
- Статус системы (количество доноров онлайн — live)
- «Powered by community • Open Source • MIT»

**Принципы:**
- HTMX для динамических обновлений (без SPA)
- Все страницы отдают полный HTML (можно открыть прямую ссылку)
- Минимальный CSS (Pico.css или аналогичный classless-фреймворк)
- Индикаторы загрузки: HTMX-атрибут `htmx-indicator` — показывать спиннер при ожидании
- Автообновление (где нужно): HTMX polling с `hx-trigger="every 30s"`

---

### 6.1 Лендинг (`/`)

**URL:** `/`  
**Доступ:** Публичный  
**Цель:** Объяснить ценность, конвертировать в регистрацию

#### Компоненты

| # | Компонент | Описание |
|---|---|---|
| 1 | **Hero** | Заголовок: «Free LLM Inference, Powered by Community GPUs». Подзаголовок: «Use any OpenAI-compatible tool. Zero cost. No credit card.». Две кнопки: «Get API Key →» (ведёт на GitHub OAuth) и «Become a Donor →» (якорь к секции для доноров ниже) |
| 2 | **Live stats bar** | Три числа: `N` models online, `N` donors online, `N` requests today. Данные: `GET /api/status`. HTMX polling каждые 10 секунд |
| 3 | **How it works** | Три шага с иконками: ① Donors share their GPU → ② Coordinator matches requests → ③ You use any OpenAI-compatible tool. Без цифр, визуально |
| 4 | **For Developers** | Заголовок: «Plug into your existing tools». Табы с примерами для 8 инструментов (см. §6.1.1 ниже). Каждый таб — готовый блок кода для копирования. Кнопка «Get your API key →» |
| 5 | **For GPU Owners** | Заголовок: «Share your GPU, earn reputation». Мини-инструкция: ① Install Ollama → ② Pull a model → ③ Run our agent. One-liner команда для копирования. Кнопка «Setup Instructions →» (ведёт на страницу входа, затем в dashboard/donor) |
| 6 | **FAQ** | Три вопроса: «Is it really free?» / «Is my data safe?» / «What models are available?». Короткие ответы. Ссылка «Read more in docs» |

#### Состояния

| Компонент | Нормальное | Пустое | Ошибка |
|---|---|---|---|
| Live stats bar | Числа обновляются | «0» — показывается нормально | Скрыть блок, не показывать ошибку |
| Model tabs (For Developers) | Названия реальных топ-моделей | Placeholder'ы | Placeholder'ы |

#### Действия пользователя
- Нажатие «Get API Key» → редирект на GitHub OAuth → callback → редирект на `/dashboard`
- Нажатие «Become a Donor» → скролл к секции For GPU Owners
- Копирование сниппета → нативная кнопка копирования (без JS: textarea + button)

#### 6.1.1 Примеры быстрого старта для всех инструментов

Каждый таб в секции «For Developers» содержит готовый к копированию блок с `OPENAI_BASE_URL="https://gpumesh.io/v1"` и плейсхолдером `$API_KEY`. На дашборде (после логина) плейсхолдер заменяется на реальный ключ пользователя.

**Список инструментов и их конфигурация:**

##### Continue.dev (VS Code / JetBrains)

```json
// ~/.continue/config.json
{
  "models": [{
    "title": "GPU Mesh (free)",
    "provider": "openai",
    "apiBase": "https://gpumesh.io/v1",
    "apiKey": "$API_KEY",
    "model": "llama3.2:3b"
  }]
}
```

##### Codex CLI (OpenAI)

```bash
export OPENAI_BASE_URL="https://gpumesh.io/v1"
export OPENAI_API_KEY="$API_KEY"
codex exec "add a DELETE /todos/:id endpoint"
```

##### Aider

```bash
aider --openai-api-base https://gpumesh.io/v1 \
      --openai-api-key $API_KEY \
      --model openai/llama3.2:3b
```

##### Cline (VS Code)

```json
// VS Code settings.json or Cline config
{
  "cline.apiProvider": "openai",
  "cline.openAiBaseUrl": "https://gpumesh.io/v1",
  "cline.openAiApiKey": "$API_KEY",
  "cline.openAiModel": "llama3.2:3b"
}
```

##### Open WebUI

```text
Admin Panel → Settings → Connections
  OpenAI API URL:  https://gpumesh.io/v1
  API Key:         $API_KEY
```
После сохранения модели из GPU Mesh появятся в выпадающем списке моделей.

##### Python (OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://gpumesh.io/v1",
    api_key="$API_KEY"
)

response = client.chat.completions.create(
    model="llama3.2:3b",
    messages=[{"role": "user", "content": "Hello!"}],
    stream=True
)
for chunk in response:
    print(chunk.choices[0].delta.content or "", end="")
```

##### curl

```bash
curl -s https://gpumesh.io/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3.2:3b",
    "messages": [{"role": "user", "content": "Say hi!"}],
    "stream": true
  }'
```

##### Oh My Pi (AI Coding Harness)

```bash
export OPENAI_BASE_URL="https://gpumesh.io/v1"
export OPENAI_API_KEY="$API_KEY"
# Oh My Pi automatically picks up OPENAI_* env vars.
# Start a session and select any model from the GPU Mesh catalog.
```

---

### 6.2 Личный кабинет (`/dashboard`)

**URL:** `/dashboard`  
**Доступ:** GitHub OAuth (сессионная cookie)  
**Цель:** Управление ключами, просмотр статистики, настройка донора

#### Структура

Страница с двумя табами: **Consumer** (активен по умолчанию) и **Donor**. Переключение через HTMX — каждый таб загружает свой фрагмент с сервера.
---

#### 6.2.1 Таб «Consumer»

#### Компоненты

| # | Компонент | Описание | HTMX |
|---|---|---|---|
| 1 | **Welcome** | «Hello, {github_login}». Кнопка Logout | — |
| 2 | **Quickstart** | Заголовок «Start in 30 seconds». Две строки для копирования: `export OPENAI_BASE_URL="https://gpumesh.io/v1"` и `export OPENAI_API_KEY="<ваш ключ>"`. Ключ подставляется динамически если есть хотя бы один. Кнопка копирования | — |
| 3 | **API Keys** | Таблица ключей. Колонки: Prefix (первые 8 символов), Scope, Created, Actions (кнопка Revoke). Кнопка «Create New Key» над таблицей. При создании — модальное окно с полным ключом + предупреждение «Copy now — shown only once» + кнопка копирования | Нет |
| 4 | **Create Key Modal** | Оверлей: показывает полный ключ (`inf_7a3b...`), кнопка копирования, предупреждение «This key will not be shown again». После закрытия — таблица обновляется HTMX | `hx-post`, обновление таблицы через `HX-Trigger` |
| 5 | **Revoke confirmation** | Диалог «Are you sure? Any tool using this key will stop working.» с кнопками Confirm/Cancel | `hx-delete`, строка удаляется из DOM |
| 6 | **Usage Stats** | Карточки: Requests today `/` Rate limit, Tokens today. Rate limit показывается как progress bar «67/100». Данные: `GET /api/consumer/stats` | Polling 60s |
| 7 | **Tool Configs** | Аккордеон с теми же 8 инструментами что в §6.1.1, но с **реальным ключом пользователя** вместо `$API_KEY`. Порядок: Continue.dev, Codex CLI, Aider, Cline, Open WebUI, Python SDK, curl, Oh My Pi | — |

#### Состояния

| Компонент | Нормальное | Пустое | Загрузка | Ошибка |
|---|---|---|---|---|
| API Keys | Таблица с ключами | «No API keys yet. Create one to start.» + кнопка Create | Спиннер | «Failed to load keys. Retry?» |
| Create Key | Модальное окно с ключом | — | Спиннер на кнопке | «Failed to create key. Try again.» |
| Usage Stats | Числа + progress bar | «No usage yet» с нулями | Спиннер | Скрыть карточки |
| Tool Configs | Код с реальным ключом | Код с плейсхолдером `$API_KEY` | — | — |

---

#### 6.2.2 Таб «Donor»

Отображается **только** если у пользователя есть хотя бы один API-ключ со scope `donor` или `both`.

#### Компоненты

| # | Компонент | Описание | HTMX |
|---|---|---|---|
| 1 | **Setup (если не настроен)** | Заголовок «Share your GPU». Инструкция из 3 шагов: ① Install Ollama, ② Pull a model, ③ Run agent. One-liner с подставленным донорским токеном (сервер генерирует если нет). Кнопка копирования | — |
| 2 | **Donor Token** | Поле с токеном (первые 8 + маска), кнопка копирования. Кнопка «Regenerate» (инвалидирует старый, создаёт новый) | `hx-post` на regenerate |
| 3 | **Agent Status** | Карточка: статус (🟢 Online / 🔴 Offline), моделей расшарено, текущая нагрузка `1/2`, uptime текущей сессии. Если несколько агентов — по карточке на каждый `provider_id`. Данные: `GET /api/donor/status` | Polling 15s |
| 4 | **Agent Status — Offline** | Карточка серая, статус 🔴 Offline, «Last seen: 5 min ago». Кнопка «Copy run command» для перезапуска | — |
| 5 | **Agent Status — Empty** | Если ни разу не подключался: «Waiting for agent… Run this command: …» | — |
| 6 | **Donor Stats** | Карточки: Requests served (lifetime), Tokens generated (lifetime), Avg tokens/sec, Total uptime. Данные: `GET /api/donor/stats` | Polling 60s |
| 7 | **Badge** | Иконка бейджа: 🥉 Bronze / 🥈 Silver / 🥇 Gold / 💎 Platinum. Прогресс-бар до следующего уровня (напр. «12,340 / 50,000 tokens to Gold») | — |
| 8 | **My Leaderboard Position** | Мини-карточка: «You are #42 of 230 donors this week». Ссылка «View full leaderboard →» | Polling 60s |

#### Пороги бейджей (lifetime tokens)

| Бейдж | Токенов |
|---|---|
| 🥉 Bronze | 1,000+ |
| 🥈 Silver | 10,000+ |
| 🥇 Gold | 100,000+ |
| 💎 Platinum | 1,000,000+ |

#### Состояния

| Компонент | Нормальное | Пустое/не настроен | Загрузка | Ошибка |
|---|---|---|---|---|
| Agent Status | Зелёная карточка | Секция Setup (см. выше) | Спиннер | «Unable to fetch status» |
| Donor Stats | Числа | «No data yet» | Спиннер | Скрыть |
| Badge | Бейдж + прогресс | «No badge yet. Serve 1,000 tokens to earn Bronze.» | — | — |

---

### 6.3 Каталог моделей (`/models`)

**URL:** `/models`  
**Доступ:** Публичный  
**Цель:** Показать все доступные модели с деталями

#### Компоненты

| # | Компонент | Описание | HTMX |
|---|---|---|---|
| 1 | **Search/filter** | Текстовый инпут с debounce (фильтрация на клиенте или через HTMX `hx-get` с параметром `?q=`) | `hx-get`, замена списка |
| 2 | **Model cards** | Карточка модели: название (`llama3.2:3b`), доноров онлайн (зелёный/жёлтый/красный индикатор), загрузка (progress bar), теги (chat, code, etc.), минимальный размер VRAM | Polling 30s |
| 3 | **Empty state** | «No models available right now. Check back later or become a donor!» с кнопкой «Become a Donor» | — |
| 4 | **Refresh indicator** | «Updated 12 seconds ago» — таймер с последнего обновления | — |

#### Цветовая индикация доступности

| Доноров | Цвет | Текст |
|---|---|---|
| >= 5 | Green | «Fast — N donors» |
| 1–4 | Yellow | «Available — N donors» |
| 0 | Red (greyed card) | «Offline» |

#### Состояния

| Компонент | Нормальное | Пустое | Ошибка |
|---|---|---|---|
| Список моделей | Карточки | Пустое состояние (см. выше) | «Failed to load models. Retrying…» (HTMX автоматически перезапросит) |
| Одна модель | Карточка с N доноров | N=0: серая карточка | — |

---

### 6.4 Таблица лидеров (`/leaderboard`)

**URL:** `/leaderboard`  
**Доступ:** Публичный  
**Цель:** Мотивировать доноров через publicly visible reputation

#### Компоненты

| # | Компонент | Описание | HTMX |
|---|---|---|---|
| 1 | **Табы периода** | Weekly (по умолчанию) | Monthly | All-time. Переключение через HTMX | `hx-get` с параметром `?period=` |
| 2 | **Топ-3** | Выделенные карточки: #1 first-place, #2 second-place, #3 third-place. Аватар GitHub, username, токенов за период, бейдж | — |
| 3 | **Таблица** | Колонки: #, Donor (аватар + username), Tokens served, Requests, Badge. Текущий пользователь подсвечен (если залогинен и есть в топе) | Polling 120s |
| 4 | **Моя позиция (если залогинен)** | Если пользователь не в топ-50: отдельная строка под таблицей «Your position: #142 — 2,345 tokens this week». Если залогинен и донор | — |
| 5 | **Empty state (не залогинен)** | «Login with GitHub to see your position» + кнопка логина | — |

#### Состояния

| Компонент | Нормальное | Пустое | Ошибка |
|---|---|---|---|
| Топ-3 | Карточки с данными | Скрыты, таблица всё равно показывается (может быть <3 доноров) | Скрыты |
| Таблица | Строки | «No donors yet. Be the first!» | «Failed to load leaderboard» |
| Моя позиция | Строка с позицией | «Start sharing to appear here!» | — |

---

### 6.5 Статус системы (`/status`)

**URL:** `/status`  
**Доступ:** Публичный  
**Цель:** Прозрачность работы сервиса

#### Компоненты

| # | Компонент | Описание | HTMX |
|---|---|---|---|
| 1 | **System status banner** | Green «All systems operational» / Yellow «Degraded» / Red «Down». Определяется: donors_online > 0 → operational, иначе degraded. Автоматически | — |
| 2 | **Metrics grid** | Карточки: Donors online, Models available, Requests today, Tokens today, Uptime (в днях/часах). Данные: `GET /api/status` | Polling 15s |
| 3 | **Recent incidents** | (Не в MVP — заглушка «No incidents reported») | — |

#### Состояния
- Загрузка: скелетон-плейсхолдеры (серые прямоугольники)
- Ошибка: «Status temporarily unavailable»

---

### 6.6 Страницы ошибок

| Код | Когда | Содержание |
|---|---|---|
| **404** | Несуществующий URL | «Page not found» + ссылка на лендинг |
| **500** | Внутренняя ошибка | «Something went wrong» + «Our team has been notified» + ссылка на status page |
| **503** | Координатор перегружен | «Service temporarily overloaded» + автообновление через 30с (meta refresh) |
| **401** | Неавторизован (middleware) | Редирект на `/` с кнопкой логина |

---

### 6.7 Карта переходов (User Flow)

```
                    ┌─────────┐
                    │ Landing │
                    └────┬────┘
                         │
              ┌──────────┼──────────┐
              ▼          ▼          ▼
         /models    /leaderboard  /status
         (public)    (public)    (public)
              │          │          │
              └──────────┼──────────┘
                         │
                    «Login with GitHub»
                         │
                    GitHub OAuth
                         │
                         ▼
              ┌─────────────────┐
              │   /dashboard    │
              │  ┌───────────┐  │
              │  │ Consumer  │  │ ← default tab
              │  ├───────────┤  │
              │  │ Donor     │  │ ← visible if has donor key
              │  └───────────┘  │
              └─────────────────┘
```

### 6.8 Новые API-эндпоинты для фронтенда

В дополнение к эндпоинтам из §5.1, фронтенду нужны:

| Метод | Путь | Аутентификация | Описание |
|---|---|---|---|
| `GET` | `/api/consumer/stats` | GitHub OAuth | Статистика потребителя: `{"requests_today": N, "tokens_today": N, "rate_limit": 100, "rate_remaining": 67}` |
| `GET` | `/api/donor/status` | GitHub OAuth | Живой статус агентов: `{"agents": [{"provider_id": "...", "online": true, "models": [...], "load": "1/2", "uptime": "2h 15m"}]}` |
| `POST` | `/api/keys/:id/regenerate` | GitHub OAuth | Перевыпустить донорский токен (старый инвалидируется, возвращается новый полный ключ) |
| `GET` | `/leaderboard/data` | Нет | Данные таблицы лидеров: `?period=weekly&limit=50` → `{"entries": [{"rank": 1, "github_login": "...", "avatar_url": "...", "tokens": N, "requests": N, "badge": "gold"}]}` |
| `GET` | `/models/data` | Нет | Данные каталога моделей: `[{"id": "llama3.2:3b", "donors_online": 12, "load": 0.3, "tags": ["chat"]}]` |
| `GET` | `/dashboard/consumer` | GitHub OAuth | HTML-фрагмент таба Consumer (для HTMX-навигации) |
| `GET` | `/dashboard/donor` | GitHub OAuth | HTML-фрагмент таба Donor (для HTMX-навигации) |

---

## 7. Модель данных (персистентное хранение)

### 7.1 Что хранится

Координатор хранит минимум данных. Запросы и ответы инференса НИКОГДА не сохраняются.

| Сущность | Хранилище | Поля |
|---|---|---|
| **Пользователь** | SQLite | `id`, `github_id`, `github_login`, `created_at` |
| **API-ключ** | SQLite | `id`, `user_id`, `key_hash`, `key_prefix` (первые 8 символов для отображения), `scope`, `created_at`, `revoked_at` |
| **Статистика донора** | SQLite | `user_id` (один пользователь = одна строка, агрегация по всем его подключениям), `total_requests`, `total_tokens`, `total_uptime_seconds`, `last_seen_at` |

### 7.2 Что НЕ хранится

- Содержимое промптов
- Сгенерированные ответы
- Любое содержимое сообщений потребитель ↔ донор
- IP-адреса потребителей или доноров

---

## 8. Защита от злоупотреблений

### 8.1 Злоупотребления потребителей

| Проблема | Защита |
|---|---|
| Один человек создаёт 100 ключей | GitHub OAuth (один пользователь = одна личность) |
| Спам/мусорные запросы | Rate limit на ключ |
| NSFW/незаконные промпты | Без сканирования контента (приватность); доноры могут жаловаться → ручная проверка → бан |
| Накрутка токенов (фейковые запросы) | Rate limit + анализ паттернов запросов (будущее) |

### 8.2 Злоупотребления доноров

| Проблема | Защита |
|---|---|
| Донор возвращает мусор/спам | Кнопка «пожаловаться» у потребителя → N жалоб → флаг донора → ручная проверка → бан |
| Донор читает промпты | Прозрачность: «⚠️ Донор видит твои запросы» отображается на видном месте |
| Донор регистрирует несуществующие модели | Автообнаружение из Ollama; нельзя заявить модели, которых нет |
| Донор собирает данные потребителей | Промпты эфемерны; постоянное хранение контента отсутствует |

### 8.3 Злоупотребления координатора

| Проблема | Защита |
|---|---|
| Оператор координатора читает трафик | Опенсорс — любой может аудировать. Возможность self-host. |
| Зловредный экземпляр координатора | Потребители выбирают, какому координатору доверять (URL настраиваемый) |

---

## 9. Объём MVP

### 9.1 Входит в MVP

- [x] Координатор: Go HTTP/WS сервер, единый бинарник
- [x] Координатор: реестр моделей + релей запросов
- [x] Координатор: OpenAI-совместимые `/v1/models` + `/v1/chat/completions`
- [x] Координатор: стриминг (SSE) и не-стриминг ответы
- [x] Координатор: rate limiting (token bucket, на API-ключ)
- [x] Координатор: SQLite-хранилище пользователей и API-ключей
- [x] Агент донора: Go бинарник, WS клиент, автообнаружение моделей Ollama
- [x] Агент донора: проксирование запросов (стриминг + не-стриминг)
- [x] Агент донора: heartbeat + переподключение с backoff
- [x] Веб-дашборд: лендинг, GitHub OAuth, управление API-ключами
- [x] Веб-дашборд: статистика донора, таблица лидеров
- [x] Веб-дашборд: сниппеты быстрого старта для популярных инструментов
- [x] Восстановление после ошибок: обрыв донора mid-stream → повтор на другом доноре

### 9.2 НЕ входит в MVP

- [ ] WebRTC direct P2P (только релей через координатор)
- [ ] Не-Ollama бэкенды (vLLM, llama.cpp server)
- [ ] Очередь запросов (503 при занятости, без ожидания)
- [ ] Кредитная система / система приоритетов (только плоский rate limit)
- [ ] Модерация контента
- [ ] Федерация (несколько координаторов с общим реестром)
- [ ] Мобильное приложение
- [ ] Платёжная система (сервис бесплатный)

---

## 10. Не-цели (скорее всего, никогда)

- Замена платных API-провайдеров — GPU Mesh только для бесплатного уровня
- Гарантии задержки или доступности — сервис сообщества, best-effort
- Хранение или анализ промптов — приватность по дизайну
- Поддержка закрытых моделей — сообщество работает с open-weight моделями

---

## 11. Структура репозитория

```
gpumesh/
├── cmd/
│   ├── coordinator/
│   │   └── main.go
│   └── provider/
│       └── main.go
├── internal/
│   ├── proto/           # Общие типы и константы протокола
│   ├── coord/           # Координатор: HTTP-обработчики, WS, реестр, релей
│   ├── provider/        # Агент донора: WS-клиент, прокси к Ollama
│   └── dashboard/       # Веб-дашборд: шаблоны, статика, OAuth
├── web/                 # Статические ресурсы (HTMX + минимальный CSS, без SPA-фреймворка)
│   ├── templates/
│   └── static/
├── go.mod
├── go.sum
├── README.md
├── SPEC.md              # Этот документ
└── LICENSE
```

---

## 12. Дистрибуция

### 12.1 Как донор получает бинарник

Потребитель использует координатор удалённо — ему не нужен бинарник. Донору нужно установить `gpumesh-provider` на свою машину. Каналы доставки:

| Канал | Приоритет | Описание |
|---|---|---|
| `go install` | MVP | Нулевые трудозатраты на CI; аудитория — разработчики, Go часто уже стоит |
| GitHub Releases + goreleaser | MVP | Pre-built бинарники, кроссплатформенная установка без Go |
| Homebrew tap | Пост-MVP | Удобно для macOS-разработчиков |
| Install script (`install.sh`) | Пост-MVP | `curl \| bash` обёртка над GitHub Releases |

### 12.2 `go install`

```bash
go install github.com/gpumesh/gpumesh/cmd/provider@latest
```

- Требует установленного Go (≥1.21).
- Бинарник попадает в `$GOPATH/bin` (по умолчанию `~/go/bin`), который должен быть в `$PATH`.
- Это канал минимальных трудозатрат — работает сразу после `git push`, без CI-настройки.
- Подходит для технической аудитории (разработчики, которые и так являются целевыми пользователями GPU Mesh).

### 12.3 GitHub Releases + goreleaser

- **Инструмент:** [goreleaser](https://goreleaser.com/) — стандарт в Go-экосистеме.
- **Платформы:** `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- **Формат:** `.tar.gz` архив с бинарником внутри.
- **CI:** GitHub Actions workflow, триггер — git tag `v*` (например `v0.1.0`).
- **Конфиг:** `.goreleaser.yml` в корне репозитория.
- **Checksums:** Файл `checksums.txt` в каждом релизе.

```yaml
# .goreleaser.yml — минимальная конфигурация
builds:
  - id: provider
    main: ./cmd/provider
    binary: gpumesh-provider
    goos: [linux, darwin]
    goarch: [amd64, arm64]
  - id: coordinator
    main: ./cmd/coordinator
    binary: gpumesh-coordinator
    goos: [linux, darwin]
    goarch: [amd64, arm64]

archives:
  - formats: [tar.gz]
    name_template: "{{ .Binary }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"
```

### 12.4 Install script (будущее)

Однострочная установка с автоматическим детектом OS/arch:

```bash
curl -sSfL https://gpumesh.io/install.sh | bash
```

Скрипт делает:
1. Определяет OS и архитектуру.
2. Находит URL последнего релиза через GitHub API.
3. Скачивает нужный `.tar.gz`.
4. Распаковывает бинарник в `/usr/local/bin`.

Актуален после post-MVP, когда аудитория расширяется за пределы разработчиков.

### 12.5 Рекомендация для секции «For GPU Owners» на лендинге

На лендинге и в дашборде донора (§6.1, §6.2.2) one-liner для установки должен выглядеть так:

```bash
# Вариант 1: go install (если стоит Go)
go install github.com/gpumesh/gpumesh/cmd/provider@latest

# Вариант 2: GitHub Releases (без Go)
curl -sSfL "https://github.com/gpumesh/gpumesh/releases/latest/download/gpumesh-provider_$(uname -s)_$(uname -m).tar.gz" | tar xz
sudo mv gpumesh-provider /usr/local/bin/
```

После установки — запуск:
```bash
export MESH_TOKEN="inf_xxxxxxxx"
gpumesh-provider
```

## 13. Ключевые технические решения

| Решение | Обоснование |
|---|---|
| **Go** | Единый бинарник, нет зависимостей рантайма, отличная конкурентность для WS релея, быстрая компиляция |
| **SQLite** | Персистентное хранилище без зависимостей, достаточно для управления пользователями и ключами, лёгкий бэкап |
| **WS relay (не WebRTC)** | Работает через любой NAT, проще в реализации, стоимость трафика незначительна на масштабе MVP |
| **OpenAI-совместимый API** | Нулевая стоимость интеграции для потребителей; каждый инструмент уже говорит на этом языке |
| **GitHub OAuth** | Низкий порог входа, не нужно управление паролями, естественно для аудитории разработчиков |
| **HTMX + минимальный CSS** | Дашборд функциональный, а не красивый. Без сложности React/SPA |
| **Только Ollama (MVP)** | 80%+ пользователей локальных LLM используют Ollama. Расширим позже |
| **Без хранения контента** | Приватность + простота GDPR + ниже операционные риски |

---

## 14. Открытые вопросы / будущие решения

1. **Название:** «GPU Mesh» — рабочее. Финальное название TBD.
2. **Кредитная система доноров:** Плоский rate-limit против «заработай приоритет шарингом» — вернуться после данных MVP о соотношении доноров и потребителей.
3. **Мультимодельные доноры:** Регистрировать ВСЕ модели Ollama или только выбранные? MVP: все, с флагом исключения.
4. **Федерация координаторов:** Если несколько людей запускают координаторы, могут ли они делить пул доноров? Не в MVP.
5. **Приоритезация запросов:** Простой rate-limit против динамического приоритета на основе вклада донора. Не в MVP.
6. **Прогрев моделей:** Должен ли координатор отправлять «разогревающий» запрос, когда модель долго простаивала? Не в MVP.
7. **Пропускная способность донора:** Должен ли агент сообщать скорость сети? Не в MVP — предполагаем достаточной.
