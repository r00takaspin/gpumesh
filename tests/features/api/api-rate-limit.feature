@api
Feature: Rate Limiting
  Ограничение частоты запросов на API-ключ (token bucket, по умолчанию 100 запросов/час).
  Проверка заголовков X-RateLimit-Remaining и Retry-After.

  Background:
    Given координатор запущен с MESH_RATE_LIMIT=10
    And существует валидный API-ключ "<valid_api_key>"
    And в реестре есть онлайн-донор с моделью "llama3.2:3b"

  Scenario: Заголовки rate-limit присутствуют в ответе /v1/models
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" присутствует
    And значение заголовка "X-RateLimit-Remaining" — целое число

  Scenario: Заголовки rate-limit присутствуют в ответе /v1/chat/completions
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
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

  Scenario: Rate limit уменьшается с каждым запросом
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then заголовок "X-RateLimit-Remaining" равен "<initial>"

    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит "stream: false"
    Then заголовок "X-RateLimit-Remaining" < <initial>

  Scenario: Превышение лимита — 429 Too Many Requests
    Given лимит запросов для ключа "<valid_api_key>" исчерпан
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 429
    And заголовок "Retry-After" присутствует
    And тело ответа содержит поле "error"

  Scenario: Превышение лимита для chat completions — 429
    Given лимит запросов для ключа "<valid_api_key>" исчерпан
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит валидный chat completion запрос
    Then статус ответа равен 429
    And заголовок "Retry-After" присутствует

  Scenario: Разные ключи имеют независимые лимиты
    Given существует второй API-ключ "<second_key>"
    And лимит для "<valid_api_key>" исчерпан
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <second_key>"
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" присутствует
    And значение "X-RateLimit-Remaining" > 0

  Scenario: Запросы без аутентификации не учитываются в rate limit
    Given лимит запросов для ключа "<valid_api_key>" не исчерпан
    When пользователь отправляет 5 GET-запросов на "/v1/models"
      And заголовок "Authorization" отсутствует во всех запросах
    Then лимит для ключа "<valid_api_key>" не изменился

  Scenario: 401-ответы не расходуют rate limit
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer invalid_key"
    Then статус ответа равен 401
    And лимит для любых ключей не изменился

  Scenario: Rate limit сбрасывается через час
    Given лимит запросов для ключа "<valid_api_key>" исчерпан
    And прошёл 1 час с момента первого запроса
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And заголовок "X-RateLimit-Remaining" > 0
