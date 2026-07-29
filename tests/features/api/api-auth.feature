@api
Feature: Аутентификация и авторизация
  Проверка механизмов аутентификации: API-ключи потребителя (Bearer),
  GitHub OAuth (сессионные cookie), токены донора (WS query-параметр).

  Background:
    Given координатор запущен и доступен

  # ===== API-ключ потребителя (Bearer) =====

  Scenario: Доступ к /v1/* с валидным API-ключом
    Given существует валидный API-ключ "<valid_key>" со scope "consumer"
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_key>"
    Then статус ответа равен 200

  Scenario: Доступ к /v1/* с недействительным ключом
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer invalid_key"
    Then статус ответа равен 401

  Scenario: Доступ к /v1/* с отозванным ключом
    Given API-ключ "<revoked_key>" был отозван
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <revoked_key>"
    Then статус ответа равен 401

  Scenario: Доступ к /v1/* без заголовка Authorization
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" отсутствует
    Then статус ответа равен 401

  Scenario: Доступ к /v1/* с пустым токеном
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer "
    Then статус ответа равен 401

  Scenario: Неверный формат заголовка Authorization
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Basic dXNlcjpwYXNz"
    Then статус ответа равен 401

  Scenario: Ключ потребителя не даёт доступ к WS донора
    Given существует API-ключ "<consumer_key>" со scope "consumer"
    When донор пытается подключиться по WebSocket "/ws/provider?token=<consumer_key>"
    Then WebSocket-соединение отклоняется с кодом 401

  Scenario: Донорский токен не даёт доступ к /v1/chat/completions (если scope=donor)
    Given существует API-ключ "<donor_key>" со scope "donor"
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <donor_key>"
      And тело запроса содержит:
        """
        {
          "model": "test-model",
          "messages": [{"role": "user", "content": "Hello"}]
        }
        """
    Then статус ответа равен 200 или 503

  # ===== GitHub OAuth (сессионные cookie) =====

  Scenario: Доступ к /api/keys с валидной сессией
    Given пользователь аутентифицирован через GitHub OAuth
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 200

  Scenario: Доступ к /api/keys без сессии
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 302
  Scenario: Доступ к /api/keys с истёкшей сессией
    Given сессионная cookie истекла
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 302
  Scenario: Доступ к /api/consumer/stats только через сессию
    Given существует валидный API-ключ "<valid_key>"
    When пользователь отправляет GET-запрос на "/api/consumer/stats"
      And заголовок "Authorization" равен "Bearer <valid_key>"
    Then статус ответа равен 200

  Scenario: Доступ к /api/donor/stats только через сессию
    Given существует валидный API-ключ "<valid_key>"
    When пользователь отправляет GET-запрос на "/api/donor/stats"
      And заголовок "Authorization" равен "Bearer <valid_key>"
    Then статус ответа равен 200

  # ===== Токен донора (WS query-параметр) =====

  Scenario: Успешное WebSocket-подключение с валидным донорским токеном
    Given существует API-ключ "<donor_key>" со scope "donor"
    When донор подключается по WebSocket "/ws/provider?token=<donor_key>"
    Then WebSocket-соединение установлено
    And координатор отправляет сообщение с полем "type" равным "registered"
    And сообщение содержит поле "provider_id"

  Scenario: WS-подключение с токеном без scope "donor"
    Given существует API-ключ "<consumer_key>" со scope "consumer"
    When донор подключается по WebSocket "/ws/provider?token=<consumer_key>"
    Then WebSocket-соединение отклоняется с кодом 401 или 403

  Scenario: WS-подключение без токена
    When донор подключается по WebSocket "/ws/provider"
    Then WebSocket-соединение отклоняется с кодом 401

  Scenario: WS-подключение с недействительным токеном
    When донор подключается по WebSocket "/ws/provider?token=invalid_token"
    Then WebSocket-соединение отклоняется с кодом 401
