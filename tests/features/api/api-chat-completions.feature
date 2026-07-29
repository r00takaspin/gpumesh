@api
Feature: Chat Completions
  Эндпоинт POST /v1/chat/completions — OpenAI-совместимый chat completion.
  Поддерживает стриминговый (SSE, stream: true) и не-стриминговый (JSON) режимы.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And в реестре есть онлайн-донор с моделью "llama3.2:3b"

  # ===== Не-стриминговый режим =====

  Scenario: Успешный не-стриминговый запрос
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And заголовок "Content-Type" равен "application/json"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 200
    And тело ответа содержит поле "id" типа "string"
    And тело ответа содержит поле "object" со значением "chat.completion"
    And тело ответа содержит поле "model" со значением "llama3.2:3b"
    And тело ответа содержит поле "choices" типа "array"
    And первый элемент "choices" содержит поле "message"
    And "choices[0].message" содержит поле "role" со значением "assistant"
    And "choices[0].message" содержит поле "content" типа "string"
    And тело ответа содержит поле "usage"
    And "usage" содержит поле "completion_tokens" типа "number"
    And "usage" содержит поле "prompt_tokens" типа "number"
    And "usage" содержит поле "total_tokens" типа "number"

  Scenario: Успешный стриминговый запрос (SSE)
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": true
        }
        """
    Then статус ответа равен 200
    And заголовок "Content-Type" содержит "text/event-stream"
    And тело ответа содержит строки в формате "data: {...}"
    And последняя строка ответа равна "data: [DONE]"

  Scenario: Запрошенная модель недоступна — 503
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "nonexistent-model:99b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 503
    And тело ответа содержит поле "error" со значением "Model not available"
    And тело ответа содержит поле "available_models" типа "array"

  Scenario: Model name с префиксом провайдера (совместимость с LiteLLM)
    Given в реестре есть онлайн-донор с моделью "llama3.2:3b"
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "openai/llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 200
    And тело ответа содержит поле "model" со значением "llama3.2:3b"

  Scenario: Все доноры заняты — 503 с retry_after_seconds
    Given все доноры модели "llama3.2:3b" на максимальной загрузке
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 503
    And тело ответа содержит поле "error" со значением "All donors busy"
    And тело ответа содержит поле "retry_after_seconds" типа "number"

  Scenario: Запрос без API-ключа — 401
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" отсутствует
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}]
        }
        """
    Then статус ответа равен 401
    And тело ответа содержит поле "error"

  Scenario: Запрос без поля model — 400
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса не содержит поле "model"
      And тело запроса содержит:
        """
        {
          "messages": [{"role": "user", "content": "Hello"}]
        }
        """
    Then статус ответа равен 400

  Scenario: Запрос без поля messages — 400
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса не содержит поле "messages"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b"
        }
        """
    Then статус ответа равен 400

  Scenario Outline: Валидация поля messages
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": <messages_value>
        }
        """
    Then статус ответа равен 400

    Examples:
      | messages_value                        |
      | []                                    |
      | null                                  |
      | "not_an_array"                        |

  Scenario: CORS-заголовки для chat completions
    When пользователь отправляет OPTIONS-запрос на "/v1/chat/completions"
      And заголовок "Origin" равен "https://example.com"
    Then заголовок "Access-Control-Allow-Origin" равен "*"

  Scenario: Параметр temperature передаётся в Ollama
    Given в реестре есть онлайн-донор с моделью "llama3.2:3b"
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}],
          "temperature": 0.7,
          "top_p": 0.9,
          "stream": false
        }
        """
    Then статус ответа равен 200
    And параметры "temperature" и "top_p" были переданы донору
