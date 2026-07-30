@api
Feature: Chat Completions (v2 hard-pin)
  POST /v1/machines/{machine_id}/chat/completions.
  Legacy pool path — 410.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And провайдер онлайн с моделью "llama3.2:3b" на машине "<machine_id>"

  Scenario: Успешный не-стриминговый запрос
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
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
    And тело ответа содержит поле "object" со значением "chat.completion"
    And тело ответа содержит поле "model" со значением "llama3.2:3b"
    And тело ответа содержит поле "choices" типа "array"

  Scenario: Успешный стриминговый запрос (SSE)
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
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
    And последняя строка ответа равна "data: [DONE]"

  Scenario: Модель не на этой машине — 404 model_not_found
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "nonexistent-model:99b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 404
    And тело ответа содержит поле "error" со значением "model_not_found"

  Scenario: Машина offline — 503
    Given машина "<machine_id>" offline
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
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
    And тело ответа содержит поле "error" со значением "machine_offline"

  Scenario: Нет ACL — 403
    Given пользователь не имеет доступа к машине "<other_machine_id>"
    When пользователь отправляет POST-запрос на "/v1/machines/<other_machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 403

  Scenario: Strip префикса openai/
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
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

  Scenario: Legacy pool path — 410
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
    Then статус ответа равен 410
    And тело ответа содержит поле "error" со значением "gone"

  Scenario: Машина busy — 503
    Given машина "<machine_id>" на максимальной загрузке
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
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
    And тело ответа содержит поле "error" со значением "machine_busy"
