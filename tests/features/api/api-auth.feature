@api
Feature: Аутентификация и авторизация (v2)
  Bearer API keys, OAuth session, provider token на WS.

  Background:
    Given координатор запущен и доступен

  Scenario: Доступ к /v1/models с валидным API-ключом
    Given существует валидный API-ключ "<valid_key>" со scope "consumer"
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_key>"
    Then статус ответа равен 200

  Scenario: Недействительный ключ — 401
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer invalid_key"
    Then статус ответа равен 401

  Scenario: Без Authorization — 401
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" отсутствует
    Then статус ответа равен 401

  Scenario: Пустой Bearer — 401
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer "
    Then статус ответа равен 401

  Scenario: Неверный формат Authorization — 401
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Basic dXNlcjpwYXNz"
    Then статус ответа равен 401

  Scenario: Отозванный ключ — 401
    Given API-ключ "<revoked_key>" был отозван
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <revoked_key>"
    Then статус ответа равен 401

  Scenario: Consumer key не даёт доступ к WS provider
    Given существует API-ключ "<consumer_key>" со scope "consumer"
    When провайдер пытается подключиться по WebSocket "/ws/provider?token=<consumer_key>"
    Then WebSocket-соединение отклоняется с кодом 401 или 403

  Scenario: Legacy POST /v1/chat/completions — 410
    Given существует валидный API-ключ "<valid_key>" со scope "consumer"
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_key>"
      And тело запроса содержит:
        """
        {
          "model": "test-model",
          "messages": [{"role": "user", "content": "Hello"}]
        }
        """
    Then статус ответа равен 410
    And тело ответа содержит поле "error.code" со значением "gone"

  Scenario: /api/keys с сессией — 200
    Given пользователь аутентифицирован через GitHub OAuth
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 200

  Scenario: /api/keys без сессии — 302
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 302

  Scenario: /api/owner/stats без сессии — 302
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/owner/stats"
    Then статус ответа равен 302

  Scenario: WS connect с provider token — registered + machine_id
    Given существует API-ключ "<provider_key>" со scope "provider"
    When провайдер подключается по WebSocket "/ws/provider?token=<provider_key>"
    Then WebSocket-соединение установлено
    And сообщение содержит поле "machine_id"

  Scenario: WS без токена — 401
    When провайдер подключается по WebSocket "/ws/provider"
    Then WebSocket-соединение отклоняется с кодом 401

  Scenario: WS с недействительным токеном — 401
    When провайдер подключается по WebSocket "/ws/provider?token=invalid_token"
    Then WebSocket-соединение отклоняется с кодом 401
