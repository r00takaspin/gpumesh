# GPT-OSS-20B с контекстом 128K на RX 7800 XT через llama.cpp + ROCm

Полное руководство по запуску 21B-MoE модели с окном 131 072 токена на AMD-видеокарте с 16 ГБ VRAM.

## Железо

| Ресурс | Характеристика |
|--------|----------------|
| GPU | RX 7800 XT (Navi 32, `gfx1101`), 16 ГБ VRAM |
| RAM | 30 ГБ DDR5 |
| CPU | Ryzen 7 7800X3D, 8C/16T |
| Диск | NVMe 468 ГБ (свободно 363 ГБ) |
| ОС | Arch Linux, ROCm 7.2.0 |
| llama.cpp | 0.18.0 (сборка из исходников) |

### Критично: `HSA_OVERRIDE_GFX_VERSION`

Без этой переменной llama-server **не видит GPU**, даже если `rocminfo` его показывает. Причина: gfx1101 нет в списке известных таргетов внутри hip runtime для данной версии ROCm.

```bash
# Проверка: без переменной — пусто
llama-server --list-devices
# → ggml_cuda_init: failed to initialize ROCm: no ROCm-capable device is detected

# С переменной — GPU найден
HSA_OVERRIDE_GFX_VERSION=11.0.1 llama-server --list-devices
# → ROCm0: AMD Radeon RX 7800 XT (16368 MiB, 16170 MiB free)
```

**Всегда** добавляйте `HSA_OVERRIDE_GFX_VERSION=11.0.1` при запуске llama-server и при сборке.

## Почему это сложно

GPT-OSS-20B — это MoE-модель: 21B общих параметров, 4B активных на токен. В квантизации Q4_K_M веса занимают **15 ГБ**. KV-кеш при 128K в FP16 — ещё **6.4 ГБ**. Итого >21 ГБ — не влезает в 16 ГБ VRAM.

**Решение:** квантизация KV-кеша. Q4_0 сжимает 6.4 → **1.6 ГБ**. Веса (15 ГБ) + KV-кеш (1.6 ГБ) + накладные (~0.5 ГБ) ≈ **17.1 ГБ** — всё ещё не влезает, поэтому часть весов автоматически выгружается в RAM. На практике VRAM занята на 97–99%, остальное в RAM.

## Шаг 1: Проверка GPU-таргета

```bash
rocminfo | grep -i "gfx"
# → gfx1101  (Navi 32 / RX 7800 XT)
```

**Важно:** `gfx1102` и `gfx1101` — оба Navi 32. Какой именно у вас — показывает `rocminfo`. Подставьте свой в `-DAMDGPU_TARGETS=`.

Проверьте наличие ROCm-тулчейна:

```bash
ls /opt/rocm/llvm/bin/clang++     # компилятор
ls /opt/rocm/lib/libamdhip64.so   # HIP runtime
pacman -Q rocm-hip-libraries       # хидеры и библиотеки
```

## Шаг 2: Сборка llama.cpp с ROCm

```bash
git clone https://github.com/ggml-org/llama.cpp.git
cd llama.cpp
cmake -B build \
  -DGGML_HIP=ON \
  -DCMAKE_C_COMPILER=/opt/rocm/llvm/bin/clang \
  -DCMAKE_CXX_COMPILER=/opt/rocm/llvm/bin/clang++ \
  -DAMDGPU_TARGETS=gfx1101 \
  -DCMAKE_PREFIX_PATH=/opt/rocm
cmake --build build --config Release -j$(nproc)
```

### Workaround: битый пакет `comgr`

Если сборка падает с ошибкой:

```
Could not find a package configuration file provided by "amd_comgr"
```

…но `pacman -Ql comgr` показывает файлы, а на диске их нет — пакет повреждён.
Решение: скачать и распаковать вручную, создать shim:

```bash
mkdir -p ~/comgr-pkg && cd ~/comgr-pkg
curl -sLO "https://archive.archlinux.org/packages/c/comgr/comgr-2%3A7.2.0-1-x86_64.pkg.tar.zst"
tar --zstd -xf comgr-2%3A7.2.0-1-x86_64.pkg.tar.zst

# Создаём локальный shim
mkdir -p ~/rocm-shim/lib/cmake/amd_comgr
cp opt/rocm/lib/cmake/amd_comgr/* ~/rocm-shim/lib/cmake/amd_comgr/
cp -a opt/rocm/lib/libamd_comgr* ~/rocm-shim/lib/
mkdir -p ~/rocm-shim/include/amd_comgr
cp opt/rocm/include/amd_comgr/amd_comgr.h ~/rocm-shim/include/amd_comgr/
Затем добавить shim в `CMAKE_PREFIX_PATH`:

```bash
cmake -B build ... -DCMAKE_PREFIX_PATH="/home/$USER/rocm-shim;/opt/rocm"
```

Этот же shim нужен и при **запуске** (не только при сборке) — `libamd_comgr.so` загружается динамически. Если каталог `~/rocm-shim` не существует (свежая система, очистка), создайте его заново по инструкции выше или распакуйте comgr в `~/.local/lib/rocm`:

```bash
export LD_LIBRARY_PATH="$HOME/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH"
export HSA_OVERRIDE_GFX_VERSION=11.0.1
```


## Шаг 3: Загрузка модели

Использовался RotorQuant-вариант Q4_K_M от сообщества:

```bash
# Установить HF CLI
curl -LsSf https://hf.co/cli/install.sh | bash

# Скачать модель (~15 ГБ, ~25 минут)
mkdir -p ~/models/gpt-oss-20b-q4km
hf download majentik/gpt-oss-20b-RotorQuant-GGUF-Q4_K_M \
  --local-dir ~/models/gpt-oss-20b-q4km
```

Альтернатива: любой GGUF с `gpt-oss-20b` и `Q4_K_M` на HuggingFace.

**Размер файла:** 15 ГБ (ожидалось 12.8 — RotorQuant чуть больше стандартного Q4_K_M).

## Шаг 4: Расчёт памяти и выбор конфигурации

KV-кеш GPT-OSS-20B при 8K (FP16): 0.40 ГБ → при 128K: **6.4 ГБ**.

Три варианта для 16 ГБ VRAM:

### Вариант A: Q4_K_M + Q4_0 KV (использован)
| Параметр | Значение |
|----------|----------|
| Веса (GPU+RAM) | Q4_K_M, 15 ГБ |
| KV-кеш (GPU) | Q4_0, 1.6 ГБ |
| VRAM | ~15.5 ГБ (97–99%) |
| Качество весов | ★★★★☆ |
| Качество контекста | ★★★☆☆ |
| Скорость | ★★★★★ (41 tok/s) |

### Вариант B: Q3_K_M + Q8_0 KV (запасной)
| Параметр | Значение |
|----------|----------|
| Веса (GPU) | Q3_K_M, ~10 ГБ |
| KV-кеш (GPU) | Q8_0, 3.2 ГБ |
| VRAM | ~13.8 ГБ |
| Качество контекста | ★★★★☆ (лучше для длинных документов) |
| Скорость | ★★★★☆ (~35 tok/s ожид.) |

### Вариант C: Q4_K_M + FP16 KV в RAM (медленный)
| Параметр | Значение |
|----------|----------|
| KV-кеш (RAM) | FP16, 6.4 ГБ |
| Скорость | ★★☆☆☆ (5–10 tok/s) |
| Качество контекста | ★★★★★ |

## Шаг 5: Запуск сервера

**Рекомендованный вариант: все слои на GPU, 64K контекст.**

```bash
GGUF=~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf

LD_LIBRARY_PATH="$HOME/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH" \
llama.cpp/build/bin/llama-server \
  --model "$GGUF" \
  --ctx-size 65536 \
  --n-gpu-layers 99 \
  --cache-type-k q4_0 \
  --cache-type-v q4_0 \
  --host 127.0.0.1 \
  --port 8080 \
  --parallel 1 \
  --reasoning-budget 4096
```

### Вариант: Multi-slot (parallel=3, 32K на слот)

`--ctx-size` задаёт **общий** размер KV-кеша. На каждый слот приходится `ctx_size / n_parallel`.
Чтобы получить 32K на слот при `--parallel 3`, нужно `--ctx-size 98304` (32 768 × 3).
Если указать `--ctx-size 32768 --parallel 3`, каждый слот получит лишь ~10 922 токена.

С 3 слотами KV-кеш занимает больше — используйте `-ngl auto`, чтобы авто-фит выгрузил часть слоёв на CPU. С `q8_0` KV-кешем flash attention **работает** (в отличие от `q4_0`, где FA-буферы вызывают OOM).

```bash
GGUF=~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf

HSA_OVERRIDE_GFX_VERSION=11.0.1 \
LD_LIBRARY_PATH="$HOME/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH" \
llama.cpp/build/bin/llama-server \
  --model "$GGUF" \
  --ctx-size 98304 \
  --parallel 3 \
  --n-gpu-layers auto \
  --cache-type-k q8_0 \
  --cache-type-v q8_0 \
  --flash-attn on \
  --host 127.0.0.1 \
  --port 8080 \
  --reasoning-budget 4096
```

Результат: 3 слота × 32 768 токенов, q8_0 KV-кеш, flash attention включён.

Если нужен полный 128K контекст — убрать `--n-gpu-layers 99` и `--ctx-size 65536`, вернуть `--ctx-size 131072`. Авто-фит выгрузит 2–3 слоя на CPU, скорость на большом контексте упадёт до 2–6 tok/s.

### Ключевые флаги

| Флаг | Назначение |
|------|-----------|
| `--ctx-size N` | Общий размер KV-кеша. **На слот:** `N / parallel`. Для 32K/слот при 3 параллельных: `--ctx-size 98304`. Округляется вниз до числа кратного `parallel`. |
| `--parallel N` | Количество слотов (одновременных запросов). **По умолчанию 4** — если не указан, KV-кеш фрагментируется. `--parallel 1` для одного диалога, `--parallel 3` для многопользовательского режима. |
| `--n-gpu-layers N` | Слоёв на GPU. `99` = все 28 слоёв. `auto` = авто-фит под VRAM. При multi-slot или большом контексте `auto` безопаснее. |
| `--cache-type-k TYPE` | Квантизация K-кеша: `q4_0` (4-бит, мин. VRAM), `q8_0` (8-бит, баланс качество/VRAM), `f16` (без потерь). |
| `--cache-type-v TYPE` | Квантизация V-кеша — то же что для K. |
| `--flash-attn on` | Flash Attention. С `q4_0` KV-кешем — OOM. С `q8_0` и `-ngl auto` — **работает**. Тест: включать только если хватает VRAM. |
| `--reasoning-budget N` | Лимит на раздумья для GPT-OSS. Без него модель уходит в бесконечное рассуждение. |
| `HSA_OVERRIDE_GFX_VERSION` | **Обязательно** `=11.0.1` для RX 7800 XT. Без этого llama.cpp не видит GPU. |
### Почему не 128K

Конфигурация со 128K контекстом (`--ctx-size 131072`) возможна только без `--n-gpu-layers`: авто-фит выгружает 2–3 слоя на CPU, освобождая VRAM под полуторный KV-кеш. На коротких запросах разница незаметна (30–65 tok/s), но при заполнении контекста до 65K+ токенов каждый новый токен гоняет attention через CPU-слои, и скорость падает до 2–6 tok/s.

С 64K и всеми слоями на GPU скорость стабильна на любом заполнении контекста. Для большинства задач 64K достаточно.

### Лог успешного запуска

```
load_model: initializing, n_slots = 1, n_ctx_slot = 65536, kv_unified = 'true'
llama_server: model loaded
llama_server: listening on http://127.0.0.1:8080
```

VRAM: 94% занято. `n_slots = 1` — один активный диалог.

## Шаг 6: Проверка

### Health check

```bash
curl -s http://127.0.0.1:8080/health
# → {"status":"ok"}
```

### Короткий запрос

```bash
curl -s http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Расскажи анекдот про программиста."}],"max_tokens":200}'
```

**Особенность GPT-OSS:** модель — «думающая» (reasoning). Ответ приходит в поле `reasoning_content`, а `content` — пустой. При использовании склеивайте оба поля:

```bash
curl -s ... | python3 -c "
import sys, json
m = json.load(sys.stdin)['choices'][0]['message']
print(m.get('reasoning_content','') + m.get('content',''))
"
```

### Иголка в стоге сена (120K контекст)

Проверка, что модель извлекает факт из начала очень длинного контекста:

```bash
python3 -c "
import json
needle = 'The secret password is: SWORDFISH-42. Remember this! '
filler = 'The quick brown fox jumps over the lazy dog. ' * 11000
content = (needle + filler)[:500000]
payload = json.dumps({
    'messages': [{'role': 'user', 'content': content +
        '\n\nWhat was the secret password mentioned at the very beginning? "
        "Reply with just the password.'}],
    'max_tokens': 200
})
with open('/tmp/needle_test.json', 'w') as f:
    f.write(payload)
"

curl -s http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d @/tmp/needle_test.json
```

**Результат:** `SWORDFISH-42` — модель извлекла пароль из начала 110 098 токенов.

## Производительность (измеренная)

| Метрика | Значение |
|----------|----------|
| Контекст (prompt_tokens) | 110 098 |
| Prefill (обработка контекста) | 414 tok/s |
| Prefill, время на 110K | 4 мин 26 сек |
| Генерация | 41 tok/s (24 ms/токен) |
| Prefill с кешем | мгновенно |
| VRAM | 97–99% (15.5+ ГБ из 16) |

**Кеширование контекста работает:** при повторном запросе с тем же префиксом `cached_tokens: 110085` — перевычисление не требуется.

## Мониторинг VRAM

```bash
rocm-smi --showmemuse
# → GPU[0]: GPU Memory Allocated (VRAM%): 99
```

Если приближается к 100% — уменьшить контекст:

```bash
--ctx-size 98304   # 96K — запас ~10% VRAM
--ctx-size 65536   # 64K — запас ~15% VRAM
```

## Возможные проблемы

### GPU не обнаружен (`no ROCm-capable device`)

**Симптом:** `ggml_cuda_init: failed to initialize ROCm: no ROCm-capable device is detected`, хотя `rocminfo` показывает GPU.

**Причины и решения (проверять по порядку):**
1. **Не установлен `HSA_OVERRIDE_GFX_VERSION=11.0.1`** — самая частая причина. Добавить в окружение.
2. **`comgr` сломан** — `pacman -Qikk comgr` покажет missing files. Решение: переустановить (`sudo pacman -S comgr`) или распаковать вручную (см. Workaround в Шаге 2).
3. **Нет прав на `/dev/kfd` и `/dev/dri/render*`** — пользователь должен быть в группе `render` и `video`.

### Flash Attention: не ускоряет GPT-OSS

С `q8_0` KV-кешем flash attention работает без OOM, но **не даёт прироста prefill** (635 vs 634 tok/s). Узкое место GPT-OSS — MoE-слои, а не attention. Можно не включать.
### OOM при запуске

**Симптом:** `cudaMalloc failed: out of memory` на этапе загрузки модели или создания контекста.

**Причины и решения:**
1. **`--flash-attn on` с q4_0 KV-кешем** — перейти на `q8_0` (см. секцию выше) или убрать FA.
2. **`--n-gpu-layers 99` (все слои)** — заменить на `auto`, дать авто-фиту выгрузить часть слоёв.
3. **Слишком большой контекст** — уменьшить `--ctx-size`.
4. **Много слотов** (`--parallel > 1`) — каждый слот умножает KV-кеш. Уменьшить `--parallel` или `--ctx-size`.

### Multi-slot: генерация падает до нуля

**Симптом:** при `--parallel 3` генерация на слотах падает до 0.14 tok/s, prefill на одном слоте отжирает весь GPU.

**Причина:** 7800 XT не тянет 3 параллельных prefill/generation. Каждый слот конкурирует за compute и memory bandwidth.

**Решение:** `--parallel 1`. Для multi-slot нужна карта с 24+ GB VRAM.

### 503 machine_busy

**Симптом:** клиенты gpumesh получают `503 machine_busy` при параллельных запросах.

**Причина:** `MaxConcurrent` в `~/.gpumesh.json` меньше количества слотов llama-server.

**Решение:** установить `"MaxConcurrent": 1` (равно `--parallel`).

### Модель долго отвечает / reasoning

GPT-OSS — думающая модель. Без `--reasoning off` генерирует 300–600 reasoning-токенов перед каждым ответом, которые клиент не видит. Ответ приходит в `reasoning_content`, `content` — пустой.

**Решение:** `--reasoning off` — убирает задержку, ответ идёт сразу в `content`.

### `/v1/models` показывает путь вместо имени

**Причина:** В GGUF `general.name` — хеш коммита, а не человекочитаемое имя. llama-server использует путь к файлу как идентификатор.

**Решение:** symlink с коротким именем:

```bash
ln -s ~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf \
      ~/models/gpt-oss-20b.gguf
# Перезапустить с --model ~/models/gpt-oss-20b.gguf
```


## Prompt Caching

### Проблема

По умолчанию каждый запрос заново вычисляет KV-кеш для всего промпта. При 17K токенов это 26 секунд prefill, даже если 16 990 токенов совпадают с предыдущим запросом.

### Решение: правка исходников

В llama.cpp есть баг (коммит `ccee42642`, PR #23280), ломающий переиспользование KV-кеша для MoE-моделей. Исправление — откат двух строк в `tools/server/server-context.cpp`:

```diff
- const auto pos_min_thold = std::max(0, pos_next - n_swa - (has_new_tokens ? 0 : 1));
+ const auto pos_min_thold = std::max(0, pos_next - n_swa);

- if (n_past > 0 && n_past <= slot.prompt.n_tokens()) {
+ if (n_past > 0 && n_past < slot.prompt.n_tokens()) {
```

После правки — `cmake --build build -j$(nproc)`, рестарт.

### Результат

| Запрос | Prefill | Токенов |
|---|---|---|
| Новый диалог | 26 568 ms | 16 857 |
| Повтор в том же диалоге | **27 ms** | 1 новый, остальное кеш |
| Ещё повтор | **68 ms** | 11 новых, остальное кеш |

**95%+ токенов из кеша** при продолжении диалога. Новый диалог — полный prefill (неизбежно).

### Важно

Кеш требует `--cache-prompt --cache-ram 4096`. Без `--cache-ram` кеш не сохраняется. Флаг `--cache-reuse` **не работает** с GPT-OSS — будет отключён с предупреждением.

## Parallel vs Single-slot

### Multi-slot (parallel=3): не взлетело

Теоретически 3 слота × 32K с q4_0 KV-кешем (1.15 GB) + модель (14.7 GB) = 15.85 GB — влезает в 16 GB. На практике:

- prefill на одном слоте отжирает GPU, генерация на других падает до **0.14 tok/s**
- три параллельных prefill по 17K токенов — GPU захлёбывается
- клиенты получают 504 `machine_busy` от координатора gpumesh

### Single-slot (parallel=1): стабильно

Один слот × 32K, q8_0 KV-кеш (768 MB). Prefill 635 tok/s, генерация 82–94 tok/s. Без конкуренции за GPU.

Для multi-slot нужна видеокарта с 24+ GB VRAM.

## Flash Attention

С q8_0 KV-кешем и `-ngl 24` flash attention (`--flash-attn on`) **не вызывает OOM** и **не даёт прироста** prefill для этой модели: 635 tok/s с FA vs 634 tok/s без. Узкое место — MoE-слои, а не attention. Для GPT-OSS flash attention можно не включать.

## Reasoning

GPT-OSS — думающая модель. Без `--reasoning off` генерирует 300–600 reasoning-токенов перед каждым ответом. Клиент ждёт десятки секунд, не видя вывода. `--reasoning off` убирает задержку — ответ идёт сразу.

## Provider: MaxConcurrent

Провайдер gpumesh (`~/.gpumesh.json`) должен иметь `MaxConcurrent`, равный количеству слотов llama-server. При `MaxConcurrent: 1` и `--parallel 3` — два из трёх запросов получат `503 machine_busy`.

```json
{
  "MaxConcurrent": 1,
  "OllamaURL": "http://localhost:8080"
}
```

## Итоговая рабочая команда

```bash
HSA_OVERRIDE_GFX_VERSION=11.0.1 \
LD_LIBRARY_PATH="$HOME/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH" \
llama.cpp/build/bin/llama-server \
  --model ~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf \
  --ctx-size 32768 \
  --parallel 1 \
  --n-gpu-layers 24 \
  --cache-type-k q8_0 \
  --cache-type-v q8_0 \
  --batch-size 4096 \
  --ubatch-size 1024 \
  --cache-prompt \
  --cache-ram -1 \
  --cache-idle-slots \
  --reasoning off \
  --host 127.0.0.1 \
  --port 8080
```

> **Важно:** `--cache-ram 0` = кеш **выключен**. `-1` = безлимитный. Это самая частая причина неработающего кеша.

### Кеш между диалогами: стратегия 2 слота + MaxConcurrent=1

Prompt cache сохраняет KV-кеш неактивных слотов в RAM. При 1 слоте кеш затирается каждым новым запросом. Решение — 2 слота + `MaxConcurrent=1` в провайдере:

- llama-server: `--parallel 2 --ctx-size 65536` (2 слота × 32K)
- Provider `~/.gpumesh.json`: `"MaxConcurrent": 1`

Координатор шлёт запросы строго по одному. Первый диалог занимает слот 0, второй — слот 1. Когда слот 0 освобождается, его KV уходит в prompt cache. При возврате к диалогу 0 — восстановление из кеша, prefill ~100 ms.

```bash
# 2 слота, q4_0 (влезает в 16 GB), кеш включён
HSA_OVERRIDE_GFX_VERSION=11.0.1 \
LD_LIBRARY_PATH="$HOME/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH" \
llama.cpp/build/bin/llama-server \
  --model ~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf \
  --ctx-size 65536 \
  --parallel 2 \
  --n-gpu-layers 24 \
  --cache-type-k q4_0 \
  --cache-type-v q4_0 \
  --batch-size 4096 \
  --ubatch-size 1024 \
  --cache-prompt \
  --cache-ram -1 \
  --cache-idle-slots \
  --reasoning off \
  --host 127.0.0.1 \
  --port 8080
 ```
## Структура файлов после настройки

```
~/llama.cpp/                  # репозиторий llama.cpp
~/llama.cpp/build/bin/        # собранные бинарники (llama-server, etc.)
~/rocm-shim/                  # workaround для comgr
~/rocm-shim/lib/libamd_comgr.so*
~/rocm-shim/lib/cmake/amd_comgr/
~/models/gpt-oss-20b-q4km/    # модель
~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf
```

## Использованные источники

- [llama.cpp](https://github.com/ggml-org/llama.cpp)
- [GPT-OSS-20B на HuggingFace](https://huggingface.co/openai/gpt-oss-20b) — оригинальная модель (Apache 2.0)
- [RotorQuant GGUF](https://huggingface.co/majentik/gpt-oss-20b-RotorQuant-GGUF-Q4_K_M) — использованная квантизация
- [Arch Linux ROCm packages](https://wiki.archlinux.org/title/GPGPU#ROCm)

---

*Собрано и проверено 2026-07-31 на RX 7800 XT + ROCm 7.2.0.*
