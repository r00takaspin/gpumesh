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
0. Если точное совпадение не найдено — отрезать префикс провайдера (всё до первого `/`), повторить поиск.
   Нужно для совместимости с LiteLLM/Aider, которые шлют `openai/llama3.2:latest`.
1. Запросить реестр: все доноры с model == запрошенная модель AND backend_ok == true
2. Если пусто → 503, тело: {"error": "Модель недоступна", "available_models": ["llama3.2:3b", ...]}
3. Отсортировать по: current_load / max_concurrent по возрастанию (наименее загруженные сначала)
4. Выбрать первого донора с current_load < max_concurrent
5. Если все доноры на максимуме → 503, тело: {"error": "Все доноры заняты", "retry_after_seconds": 30}
6. Отправить запрос → донору (с очищенным именем модели)
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
1. Загрузить конфиг из ~/.gpumesh.json (если существует)
2. Применить переменные окружения (MESH_*)
3. Применить CLI-флаги (если заданы явно)
4. Авто-детект Ollama: проверить OLLAMA_HOST, затем localhost:11434
5. Если токен пуст или Ollama недоступен — запустить интерактивный wizard
6. Открыть WebSocket к координатору (экспоненциальный реконнект: 1с → 60с)
7. Запросить список моделей у Ollama (POST /api/tags), отфильтровать по белому списку
8. Отправить register координатору (модели, max_concurrent, описание, hardware)
9. Запустить heartbeat-тикер (интервал 30с)
10. Войти в цикл обработки запросов
```

### 4.2 Конфигурация

Приоритет (от высшего к низшему): CLI-флаги > переменные окружения > `~/.gpumesh.json` > встроенные значения по умолчанию.

| Флаг | Переменная окружения | По умолчанию | Описание |
|---|---|---|---|
| `--coordinator` | `MESH_COORDINATOR` | `wss://gpumesh.net/ws/provider` | WS URL координатора |
| `--token` | `MESH_TOKEN` | — | Токен аутентификации донора |
| `--ollama-url` | `MESH_OLLAMA_URL` | авто-детект | Базовый URL Ollama (авто-детект: `OLLAMA_HOST` → `localhost:11434`) |
| `--models` | `MESH_MODELS` | (все обнаруженные) | Белый список моделей. Comma-separated: `llama3.2:3b,codellama:7b` |
| `--description` | `MESH_DESCRIPTION` | hostname | Публичное описание (напр. "RTX 4090, US-East") |
| `--max-concurrent` | `MESH_MAX_CONCURRENT` | `1` | Максимум одновременных запросов |
| `--wizard` | — | `false` | Принудительный запуск мастера настройки |
| `--no-wizard` | — | `false` | Пропустить мастер даже при неполном конфиге |
| `--config` | `MESH_CONFIG` | `~/.gpumesh.json` | Путь к файлу конфигурации |

Конфиг-файл (`~/.gpumesh.json`):

```json
{
  "coordinator_url": "wss://gpumesh.net/ws/provider",
  "token": "inf_xxxxxxxx",
  "ollama_url": "http://localhost:11434",
  "models": ["llama3.2:3b", "codellama:7b"],
  "max_concurrent": 2,
  "description": "RTX 4090, US-East"
}
```

Сохраняется автоматически после прохождения мастера настройки.
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
| `GET` | `/consumer` | Нет (публичный) | Страница потребителя (два состояния: logged-out/logged-in) |

Аутентификация:
- **API-ключ потребителя:** заголовок `Authorization: Bearer <key>`. Аналогично OpenAI.
- **GitHub OAuth:** сессионная cookie после логина через GitHub.
- **Токен донора:** API-ключ с scope `donor` или `both`, передаётся в WS как query-параметр `?token=<key>` при подключении.

**OAuth редирект:** Параметр `?redirect=<path>` в `/auth/github` позволяет указать целевой URL после успешного логина. По умолчанию — `/dashboard`. Пример: `/auth/github?redirect=/consumer` перенаправляет на страницу потребителя после логина.

**CORS:** Все эндпоинты `/v1/*` возвращают заголовки `Access-Control-Allow-Origin: *` и `Access-Control-Allow-Headers: Authorization, Content-Type`. Необходимо для работы из браузерных инструментов (Open WebUI, LobeChat).

**TLS:** В продакшене координатор работает за reverse-proxy (nginx/Caddy) с TLS. В разработке (`localhost`) агент донора подключается по `ws://`, а не `wss://`.
Дополнительные эндпоинты:
| Метод | Путь | Аутентификация | Описание |
|---|---|---|---|
| `GET` | `/health` | Нет | Liveness/readiness probe |
| `POST` | `/share/tokens` | GitHub OAuth | Создать донорский токен. Возвращает HTML-фрагмент модального окна с полным токеном и кнопкой копирования |
| `GET` | `/install-provider.sh` | Нет | Универсальный скрипт установки провайдера. `curl -sSfL https://gpumesh.net/install-provider.sh \| sh`. Download base настраивается через `MESH_INSTALL_SCRIPT_DOWNLOAD_BASE`.
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

### 5.3 Конфигурация координатора

| Переменная окружения | По умолчанию | Описание |
|---|---|---|
| `MESH_ADDR` | `:8080` | Адрес HTTP-сервера |
| `MESH_DB` | `data/gpumesh.db` | Путь к SQLite-базе |
| `MESH_BASE_URL` | `http://localhost:8080` | Внешний URL координатора |
| `MESH_RATE_LIMIT` | `100` | Лимит запросов в час на API-ключ |
| `MESH_AFFINITY_TTL` | `120` | TTL sticky-аффинити consumer→donor (секунды) |
| `MESH_INSTALL_SCRIPT_DOWNLOAD_BASE` | `https://github.com/r00takaspin/gpumesh/releases/latest/download` | Базовый URL для загрузки бинарников в `/install-provider.sh` |

### 5.4 Реестр (структура данных в памяти)

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
| 1 | **Hero** | ASCII-логотип «GPU MESH». Командная строка: `[user@mesh]:~$ free_llm_inference --powered-by community_gpus`. Подзаголовок: «Use any OpenAI-compatible tool. Zero cost. No credit card.». Две CTA-кнопки: «Get API Key →» (ведёт на `/login.html` → GitHub OAuth) и «Become a Donor →» (якорь `#donor-section` к секции для доноров). Кнопки в терминальном стиле: основная (акцентный фон) и вторичная (outline) |
| 2 | **What is this** | Один абзац в CLI-стиле (`$ whatis gpumesh`): краткое описание P2P-сети, ценность для энтузиастов, отсутствие счетов и лимитов. Компактная альтернатива развёрнутому описанию — не дублирует «How it works» |
| 3 | **Live stats bar** | Три числа: `▶ N` models online, `▶ N` donors online, `▶ N` req today. Данные: `GET /api/status`. Статика в мокапе, в продакшене — HTMX polling каждые 10 секунд |
| 4 | **How it works** | Три шага с терминальным разделителем `──▶`: ① SHARE — Donors run gpumesh-provider → ② MATCH — Coordinator routes requests → ③ USE — Your tools just work. Лаконично, без цифр в описании |
| 5 | **Why GPU Mesh?** | Сравнение с альтернативами в трёх карточках: **$0** (Ollama бесплатен локально, но не по сети; OpenRouter берёт за токены — мы нет), **OpenAI API** (drop-in совместимость, две переменные окружения), **P2P** (нет дата-центров, нет vendor lock-in, комьюнити-доверие) |
| 6 | **For Developers** | Заголовок: «FOR DEVELOPERS». Табы с примерами для 8 инструментов: Continue.dev, Codex CLI, Aider, Cline, Open WebUI, Python SDK, curl, Oh My Pi (см. §6.1.1). Каждый таб — готовый блок кода с кнопкой `[Copy]`. Кнопка `./get-api-key.sh` ведёт на `/dashboard.html` |
| 7 | **For GPU Owners** | Заголовок: «SHARE YOUR GPU, EARN REPUTATION». Мотивационный абзац: «Run the agent, climb the leaderboard, earn badges. Every request you serve builds your reputation in the mesh.» Мини-инструкция из трёх шагов: ① INSTALL Ollama → ② PULL a model → ③ RUN our agent. Пять платформенных табов с командами установки: Linux arm64, Linux amd64, macOS, Go install, Docker. Ссылки: `man gpumesh-donor` (→ `/dashboard.html`) и «All releases →» (→ GitHub releases) |
| 8 | **Join the Community** | Призыв для early adopters: «GPU Mesh is in early development. Star the repo, join the discussion, help shape what comes next.» Три кнопки: «★ Star on GitHub», «Join Discord», «Discussions» |
| 9 | **Top Models Right Now** | Сетка из 5 карточек: название модели (mono), вендор (Meta/Alibaba/Mistral AI/Microsoft), счётчик доноров (`▶ N donors`). Мокап-данные. Ссылка «Browse all models →» на `/models.html` |
| 10 | **Top Donors This Week** | Подиум с топ-3 донорами: 🥇🥈🥉, никнейм, значок-бейдж (⚡🔋🫐), количество токенов за неделю. Мокап-данные. Ссылка «Full leaderboard →» на `/leaderboard.html` |
| 11 | **FAQ** | Аккордеон с тремя вопросами. «Is it really free?» — да, комьюнити, без лимитов. «Is my data safe?» — промпты обрабатываются GPU комьюнити так же, как на серверах OpenAI/Anthropic; промпты и ответы не хранятся. «What models are available?» — все Ollama-совместимые модели доноров, ссылка на `/models.html` |

#### Состояния

| Компонент | Нормальное | Пустое | Ошибка |
|---|---|---|---|
| Live stats bar | Числа обновляются | «0» — показывается нормально | Скрыть блок, не показывать ошибку |
| Top Models | 5 моделей с донорами | «No models online — check back soon» | Скрыть блок |
| Top Donors | Подиум с 3 донорами | «No donors this week — be the first!» | Скрыть блок |

#### Действия пользователя
- Нажатие «Get API Key» → редирект на `/login.html` → GitHub OAuth → callback → редирект на `/dashboard`
- Нажатие «Become a Donor» → скролл к секции For GPU Owners (`#donor-section`)
- Копирование сниппета → нативная кнопка `[Copy]` (textarea + button, обработчик JS)
- Раскрытие FAQ → аккордеон (onclick `toggleAccordion`, каретка поворачивается на 90°)

#### 6.1.1 Примеры быстрого старта для всех инструментов

Каждый таб в секции «For Developers» содержит готовый к копированию блок с `OPENAI_BASE_URL="https://gpumesh.net/v1"` и плейсхолдером `$API_KEY`. На дашборде (после логина) плейсхолдер заменяется на реальный ключ пользователя.

**Список инструментов и их конфигурация:**

##### Continue.dev (VS Code / JetBrains)

```json
// ~/.continue/config.json
{
  "models": [{
    "title": "GPU Mesh (free)",
    "provider": "openai",
    "apiBase": "https://gpumesh.net/v1",
    "apiKey": "$API_KEY",
    "model": "llama3.2:3b"
  }]
}
```

##### Codex CLI (OpenAI)

```bash
export OPENAI_BASE_URL="https://gpumesh.net/v1"
export OPENAI_API_KEY="$API_KEY"
codex exec "add a DELETE /todos/:id endpoint"
```

##### Aider

```bash
aider --openai-api-base https://gpumesh.net/v1 \
      --openai-api-key $API_KEY \
      --model openai/llama3.2:3b
```

##### Cline (VS Code)

```json
// VS Code settings.json or Cline config
{
  "cline.apiProvider": "openai",
  "cline.openAiBaseUrl": "https://gpumesh.net/v1",
  "cline.openAiApiKey": "$API_KEY",
  "cline.openAiModel": "llama3.2:3b"
}
```

##### Open WebUI

```text
Admin Panel → Settings → Connections
  OpenAI API URL:  https://gpumesh.net/v1
  API Key:         $API_KEY
```
После сохранения модели из GPU Mesh появятся в выпадающем списке моделей.

##### Python (OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://gpumesh.net/v1",
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
curl -s https://gpumesh.net/v1/chat/completions \
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
export OPENAI_BASE_URL="https://gpumesh.net/v1"
export OPENAI_API_KEY="$API_KEY"
# Oh My Pi automatically picks up OPENAI_* env vars.
# Start a session and select any model from the GPU Mesh catalog.
```

---

### 6.2 Личный кабинет

**`/dashboard`** редиректит (302) на `/use` — страницу потребителя. Исторически был отдельной страницей, теперь унифицирован с `/use`.

---


### 6.2a Consumer Page (`/consumer`)

**URL:** `/consumer`  
**Доступ:** Публичный (два состояния: logged-out и logged-in)  
**Цель:** Привлечь новых потребителей и предоставить рабочее пространство для использования GPU Mesh

Страница имеет два принципиально разных состояния в зависимости от аутентификации.

---

#### 6.2a.1 Состояние «Logged Out» (публичный лендинг)

Отображается неавторизованным пользователям. Цель — объяснить ценность и конвертировать в регистрацию.

##### Компоненты

| # | Компонент | Описание |
|---|---|---|
| 1 | **Hero** | ASCII-логотип «GPU MESH». Заголовок «Free LLM inference». Подзаголовок: «Use any OpenAI-compatible tool with community GPUs. No credit card, no limits. One click to get started.». CTA-кнопка «Sign in with GitHub →» (ведёт на `/auth/github?redirect=/consumer`) |
| 2 | **How it works** | Карточка с заголовком «How it works», внутри которой размещён Community GPU banner («GPU Mesh is powered by community GPUs…») и три шага: ① Sign in (GitHub OAuth, API key created automatically), ② Pick a model (Browse live models from community donors), ③ Use it (Copy config for your tool, OpenAI-compatible) |
| 3 | **Live stats** | Три блока с числами: Models online (количество), Donors online (количество), Requests today (счётчик). Данные из реестра координатора |

---

#### 6.2a.2 Состояние «Logged In» (дашборд потребителя)

Отображается авторизованным пользователям. Три таба: Overview, API Keys, Models. По умолчанию активен Overview.

##### One-time API Key Display

При первом заходе (параметр `?new=1` в URL после OAuth-редиректа) отображается:
- Полоса с ключом (`inf_...`) жёлтого цвета с иконкой ⚠
- Кнопка «Copy» для копирования ключа
- Кнопка «✕» для скрытия полосы (dismiss)
- Предупреждение: «Copy this key now — it won't be shown again.»

При последующих заходах отображается префикс ключа (первые 8 символов) без возможности dismiss.

##### Таб «Overview»

| # | Компонент | Описание |
|---|---|---|
| 1 | **Usage stats** | Три блока: Requests today (X/Y rate limit), Tokens today, Models available (количество) |
| 2 | **Quickstart** | Блок кода с `export OPENAI_BASE_URL` и `export OPENAI_API_KEY`. API-ключ подставляется автоматически (префикс или плейсхолдер). Кнопка копирования |

##### Таб «API Keys»

Список API-ключей пользователя в виде карточек. Каждая карточка:
- Префикс ключа (синий моноширинный)
- Дата создания и scope (badge)
- Кнопка «Revoke» (HTMX: `DELETE /api/keys/{id}`)

Кнопка «+ Create new key» (HTMX: `POST /consumer/keys`) создаёт новый ключ и обновляет список. При создании новый ключ показывается полностью с предупреждением.

##### Таб «Models»

Список доступных моделей в виде раскрывающихся карточек. Каждая карточка:
- **Заголовок:** название модели (моноширинный), badge «available»/«unavailable», количество доноров, загрузка (%), вендор
- **Раскрытие:** клик по заголовку показывает/скрывает конфигурации для 7 инструментов

**7 инструментов (tool rows):**

Каждый инструмент — раскрывающаяся строка с названием, шевроном и кнопкой Copy:

1. **Continue.dev** — JSON-конфигурация для `config.json`
2. **Aider** — команда запуска с флагами `--openai-api-base` и `--openai-api-key`
3. **Cline** — JSON-конфигурация для VS Code settings
4. **Open WebUI** — переменные окружения
5. **curl** — пример запроса к `/v1/chat/completions`
6. **Python SDK** — код на Python с использованием `openai` библиотеки
7. **Oh My Pi** — переменные окружения + инлайн-поле с командой `omp run "... " --model {name}` и кнопкой Copy
Все сниппеты содержат реальное название модели (подстановка через шаблон) и префикс API-ключа пользователя.

##### Состояния

| Компонент | Нормальное | Пустое | Загрузка | Ошибка |
|---|---|---|---|---|
| Models | Карточки с донорами | «No models available. Check back soon.» | — | — |
| API Keys | Список ключей | Кнопка «Create new key» | HTMX-индикатор | — |
| One-time key | Полоса с ключом + ⚠ | Префикс ключа | — | — |

##### Навигация

В навбаре для авторизованных пользователей: «Consumer» (активная), «Dashboard», «Models», «Leaderboard», «Status».

---

#### 6.2a.3 Технические детали

- **Авто-создание ключа:** при первом заходе через OAuth с `redirect=/consumer` и отсутствии ключей у пользователя автоматически создаётся API-ключ со scope `consumer`. Ключ отображается один раз.
- **OAuth редирект:** `/auth/github?redirect=/consumer` — параметр `redirect` задаёт целевой путь после логина. При отсутствии ключей добавляется `?new=1`.
- **HTMX-фрагменты:** `GET /consumer/keys` (список ключей), `POST /consumer/keys` (создание ключа с показом полного значения).
- **Табы:** переключение через JavaScript `switchTab()`, активный таб определяется query-параметром `?tab=models|keys|overview`.
- **Раскрытие tool rows:** CSS-класс `.open` на `.tool-row` показывает следующий `.tool-snippet`.
- **Копирование:** `navigator.clipboard.writeText()` с визуальной обратной связью «Copied!» на 2 секунды.


### 6.2b Share GPU Page (`/share`)

**URL:** `/share`
**Доступ:** Публичный (два состояния: logged-out и logged-in)
**Цель:** Онбординг доноров — получить токен, скопировать команду, запустить провайдера.

Страница имеет два состояния: logged-out (публичный лендинг) и logged-in (рабочий дашборд донора).

---

#### 6.2b.1 Состояние «Logged Out»

##### Компоненты

| # | Компонент | Описание |
|---|---|---|
| 1 | **Hero** | ASCII-логотип. Заголовок «Share your GPU». Подзаголовок: «Run the agent, serve LLM requests, earn reputation in the mesh.» CTA-кнопка «Sign in with GitHub →» |
| 2 | **How it works** | Три шага: ① Install Ollama, ② Pull a model, ③ Run the agent |

---

#### 6.2b.2 Состояние «Logged In»

Страница состоит из трёх HTMX-фрагментов, которые загружаются асинхронно и обновляются поллингом.

##### Компоненты

| # | Компонент | Эндпоинт | Поллинг | Описание |
|---|---|---|---|---|
| 1 | **Setup + Token** | `GET /share/setup` | 5s | Основной блок. Если у пользователя нет донорского токена — баннер «⚠ No donor token» с кнопкой «+ Generate donor token». Если есть — OS-табы с инструкцией по установке и командой запуска (с префиксом токена). При клике на Generate открывается модальное окно с полным токеном и кнопкой копирования. |
| 2 | **Agent Status** | `GET /share/models` | 10s | Карточки агентов: 🟢 ONLINE / 🔴 Offline, hardware, models, load, uptime. Если агентов нет — секция скрыта. |
| 3 | **Stats + Badge + Tokens** | `GET /share/donor-stats` | 60s + refreshStats event | Карточки статистики (lifetime), бейдж с прогресс-баром, список донорских токенов с кнопками Revoke, и кнопка «+ Generate token» (создаёт токен через модальное окно). |

##### Модальное окно генерации токена

| Эндпоинт | Описание |
|---|---|
| `POST /share/tokens` | Создаёт донорский токен, возвращает HTML-фрагмент модального окна (`share-token-modal.html`) с полным токеном и кнопкой `[Copy]`. Окно фиксированное (overlay), закрывается по ✕. При закрытии триггерит обновление Setup и Stats через `htmx.ajax`. |

##### Технические детали

- **Авто-создание токена:** при первом заходе через OAuth с `redirect=/share` и отсутствии донорских ключей (`CountKeysByScope(userID, "donor") == 0`) автоматически создаётся токен. Отображается в модальном окне.
- **Revoke:** `DELETE /api/keys/:id` с `hx-target="#share-stats"`. Ответ рендерит обновлённый `share-stats.html` без `hx-trigger="load"` во избежание рекурсии.
- **Предупреждение «No donor token»:** отображается внутри `share-setup.html` когда `HasToken == false`. Заменяется на нормальный Setup после создания токена и закрытия модального окна.

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
              ┌──────────┴──────────┐
              ▼                     ▼
         /use (Consumer)      /share (Donor)
    ┌─────────────────┐    ┌─────────────────┐
    │ Overview         │    │ Setup + Token   │
    │ API Keys         │    │ Agent Status    │
    │ Models           │    │ Stats + Badge   │
    └─────────────────┘    └─────────────────┘
```


### 6.8 Новые API-эндпоинты для фронтенда

В дополнение к эндпоинтам из §5.1, фронтенду нужны:

| Метод | Путь | Аутентификация | Описание |
|---|---|---|---|
| `GET` | `/api/consumer/stats` | GitHub OAuth | Статистика потребителя: `{"requests_today": N, "tokens_today": N, "rate_limit": 100, "rate_remaining": 67}` |
| `GET` | `/api/donor/status` | GitHub OAuth | Живой статус агентов: `{"agents": [{"provider_id": "...", "online": true, "models": [...], "load": "1/2", "uptime": "2h 15m"}]}` |
| `POST` | `/api/keys/:id/regenerate` | GitHub OAuth | Перевыпустить донорский токен (старый инвалидируется, возвращается новый полный ключ в модальном окне) |
| `GET` | `/leaderboard/data` | Нет | Данные таблицы лидеров: `?period=weekly&limit=50` → `{"entries": [{"rank": 1, "github_login": "...", "avatar_url": "...", "tokens": N, "requests": N, "badge": "gold"}]}` |
| `GET` | `/models/data` | Нет | Данные каталога моделей: `[{"id": "llama3.2:3b", "donors_online": 12, "load": 0.3, "tags": ["chat"]}]` |
| `GET` | `/share/setup` | GitHub OAuth | HTMX-фрагмент: блок Setup + предупреждение/токен. Поллинг 5s |
| `GET` | `/share/models` | GitHub OAuth | HTMX-фрагмент: карточки агентов донора. Поллинг 10s |
| `GET` | `/share/donor-stats` | GitHub OAuth | HTMX-фрагмент: статистика, бейдж, список токенов. Поллинг 60s. Слушает `refreshStats` event |

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
go install github.com/r00takaspin/gpumesh/cmd/provider@latest
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
curl -sSfL https://gpumesh.net/install.sh | bash
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
go install github.com/r00takaspin/gpumesh/cmd/provider@latest

# Вариант 2: GitHub Releases (без Go)
curl -sSfL "https://github.com/r00takaspin/gpumesh/releases/latest/download/gpumesh-provider_0.1.4_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar xz
sudo mv gpumesh-provider /usr/local/bin/
```

После установки — запуск:
```bash
export MESH_TOKEN="inf_xxxxxxxx"
gpumesh-provider
```


## 13. Деплой координатора

TBD — будет добавлено позже.

---
## 14. Ключевые технические решения

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

## 15. Открытые вопросы / будущие решения

1. **Название:** «GPU Mesh» — рабочее. Финальное название TBD.
2. **Кредитная система доноров:** Плоский rate-limit против «заработай приоритет шарингом» — вернуться после данных MVP о соотношении доноров и потребителей.
3. **Мультимодельные доноры:** Регистрировать ВСЕ модели Ollama или только выбранные? MVP: все, с флагом исключения.
4. **Федерация координаторов:** Если несколько людей запускают координаторы, могут ли они делить пул доноров? Не в MVP.
5. **Приоритезация запросов:** Простой rate-limit против динамического приоритета на основе вклада донора. Не в MVP.
6. **Прогрев моделей:** Должен ли координатор отправлять «разогревающий» запрос, когда модель долго простаивала? Не в MVP.
7. **Пропускная способность донора:** Должен ли агент сообщать скорость сети? Не в MVP — предполагаем достаточной.
