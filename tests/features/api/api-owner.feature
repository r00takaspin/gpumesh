@api
Feature: Статистика и статус owner
  GET /api/owner/stats и GET /api/owner/status.

  Background:
    Given координатор запущен и доступен
    And пользователь аутентифицирован через GitHub OAuth
    And у пользователя есть provider token
    And провайдер онлайн с моделью "llama3.2:3b" на машине "<machine_id>"

  Scenario: Получение статистики owner
    When пользователь отправляет GET-запрос на "/api/owner/stats"
    Then статус ответа равен 200
    And тело ответа содержит поле "total_requests" типа "number"
    And тело ответа содержит поле "total_tokens" типа "number"
    And тело ответа содержит поле "total_uptime_seconds" типа "number"
    And тело ответа содержит поле "badge" типа "string"

  Scenario: Статистика owner без аутентификации
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/owner/stats"
    Then статус ответа равен 302

  Scenario: Статус машин owner
    When пользователь отправляет GET-запрос на "/api/owner/status"
    Then статус ответа равен 200
    And тело ответа содержит поле "agents" типа "array"
    And массив "agents" содержит хотя бы 1 элемент
    And каждый элемент "agents" содержит поле "machine_id" типа "string"
    And каждый элемент "agents" содержит поле "online" со значением true

  Scenario: Статус без агентов
    Given у пользователя нет подключённых агентов
    When пользователь отправляет GET-запрос на "/api/owner/status"
    Then статус ответа равен 200
    And массив "agents" пуст

  Scenario: Статус без аутентификации
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/owner/status"
    Then статус ответа равен 302
