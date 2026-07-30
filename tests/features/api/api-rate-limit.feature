@api
Feature: Rate Limiting
  Token bucket на API key; заголовки X-RateLimit-Remaining и Retry-After.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And провайдер онлайн с моделью "llama3.2:3b" на машине "<machine_id>"

  Scenario: Rate-limit заголовок на /v1/models
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" присутствует
    And значение заголовка "X-RateLimit-Remaining" — целое число

  Scenario: Rate-limit заголовок на per-machine chat
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hi"}],
          "stream": false
        }
        """
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" присутствует

  Scenario: Rate limit уменьшается
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then заголовок "X-RateLimit-Remaining" равен "<initial>"
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then заголовок "X-RateLimit-Remaining" < <initial>

  Scenario: Превышение лимита — 429
    Given лимит запросов для ключа "<valid_api_key>" исчерпан
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 429
    And заголовок "Retry-After" присутствует
    And тело ответа содержит поле "error"

  Scenario: 429 на per-machine chat
    Given лимит запросов для ключа "<valid_api_key>" исчерпан
    When пользователь отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит валидный chat completion запрос
    Then статус ответа равен 429
    And заголовок "Retry-After" присутствует

  Scenario: Независимые лимиты ключей
    Given существует второй API-ключ "<second_key>"
    And лимит для "<valid_api_key>" исчерпан
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <second_key>"
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" присутствует

  Scenario: 401 не расходует rate limit чужого ключа
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer invalid_key"
    Then статус ответа равен 401

  Scenario: Сброс лимита через test endpoint
    Given лимит запросов для ключа "<valid_api_key>" исчерпан
    And прошёл 1 час с момента первого запроса
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" > 0
