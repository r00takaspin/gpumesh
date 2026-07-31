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
```

Затем добавить shim в `CMAKE_PREFIX_PATH`:

```bash
cmake -B build ... -DCMAKE_PREFIX_PATH="/home/$USER/rocm-shim;/opt/rocm"
```

И при запуске:

```bash
export LD_LIBRARY_PATH="/home/$USER/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH"
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

```bash
GGUF=~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf

LD_LIBRARY_PATH="$HOME/rocm-shim/lib:/opt/rocm/lib:$LD_LIBRARY_PATH" \
llama.cpp/build/bin/llama-server \
  --model "$GGUF" \
  --ctx-size 131072 \
  --cache-type-k q4_0 \
  --cache-type-v q4_0 \
  --host 127.0.0.1 \
  --port 8080
```

### Ключевые флаги

| Флаг | Назначение |
|------|-----------|
| `--ctx-size 131072` | 128 × 1024 — контекст, кратность 1024 обязательна |
| `--cache-type-k q4_0` | Квантизация ключей KV-кеша до 4 бит |
| `--cache-type-v q4_0` | Квантизация значений KV-кеша до 4 бит |
| `--flash-attn on` | **Не использовать!** Вызвал OOM — доп. буферы FA не влезли |

### Что не было использовано

- **`--n-gpu-layers`** — не задан. Авто-фит сам решает, сколько слоёв на GPU, сколько в RAM. Модель имеет ~28 слоёв; часть уходит в RAM при нехватке VRAM.
- **`--no-mmap`** — не задан. Лог рекомендует его для тензоров на CPU, но работает и без него.

### Лог успешного запуска

```
llama_model_loader: tensor overrides to CPU are used with mmap enabled
load_model: initializing, n_slots = 4, n_ctx_slot = 131072, kv_unified = 'true'
llama_server: model loaded
llama_server: listening on http://127.0.0.1:8080
```

Строка `tensor overrides to CPU` — норма: часть весов не влезла в VRAM и ушла в RAM.

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

### OOM при запуске

**Симптом:** `cudaMalloc failed: out of memory` на этапе загрузки модели или создания контекста.

**Причины и решения:**
1. **`--flash-attn on`** — убрать. FA требует доп. буферы (~400 МБ), которых не хватает.
2. **`--n-gpu-layers 999`** — убрать. Дать авто-фиту решить, сколько слоёв на GPU.
3. **Слишком большой контекст** — уменьшить `--ctx-size`.

### Модель не отвечает / пустой content

**Причина:** GPT-OSS — reasoning-модель. Пишет в `reasoning_content`, не в `content`.

**Решение:** читать оба поля, склеивать.

### `/v1/models` показывает путь вместо имени

**Причина:** В GGUF `general.name` — хеш коммита, а не человекочитаемое имя. llama-server использует путь к файлу как идентификатор.

**Решение:** symlink с коротким именем:

```bash
ln -s ~/models/gpt-oss-20b-q4km/gpt-oss-20b-RotorQuant-Q4_K_M.gguf \
      ~/models/gpt-oss-20b.gguf
# Перезапустить с --model ~/models/gpt-oss-20b.gguf
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
