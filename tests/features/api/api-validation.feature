@api
Feature: Валидация запросов (v2)
  Граничные случаи на per-machine inference и смежных API.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And провайдер онлайн с моделью "llama3.2:3b" на машине "<machine_id>"

  Scenario Outline: Невалидный JSON на per-machine chat
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And заголовок "Content-Type" равен "application/json"
      And тело запроса содержит:
        """
        <body>
        """
    Then статус ответа равен 400

    Examples:
      | body            |
      | {invalid json}  |
      | ""              |
      | null            |
      | "just a string" |
      | 12345           |

  Scenario: Пустое поле model — 400
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "",
          "messages": [{"role": "user", "content": "Hello"}]
        }
        """
    Then статус ответа равен 400

  Scenario: Пустой messages — 400
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": []
        }
        """
    Then статус ответа равен 400

  Scenario: Успешный запрос с пустым content
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": ""}],
          "stream": false
        }
        """
    Then статус ответа равен 200

  Scenario: Legacy path всегда 410 даже при валидном теле
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит валидный chat completion запрос
    Then статус ответа равен 410

  Scenario: Создание ключа с пустым телом
    Given пользователь аутентифицирован через GitHub OAuth
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса пустое
    Then статус ответа равен 201

  Scenario: Несуществующий /v1 эндпоинт — 404
    When пользователь отправляет GET-запрос на "/v1/nonexistent"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 404

  Scenario: Жалоба с пустым reason — 400
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "request_id": "req_123",
          "reason": ""
        }
        """
    Then статус ответа равен 400
