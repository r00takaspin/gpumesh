@api
Feature: Статистика потребителя
  Эндпоинт GET /api/consumer/stats возвращает статистику использования API
  для аутентифицированного пользователя: количество запросов, токенов, лимит.

  Background:
    Given координатор запущен и доступен
    And пользователь аутентифицирован через GitHub OAuth
    And у пользователя есть API-ключ потребителя

  Scenario: Получение статистики потребителя
    When пользователь отправляет GET-запрос на "/api/consumer/stats"
    Then статус ответа равен 200
    And тело ответа содержит поле "requests_today" типа "number"
    And тело ответа содержит поле "tokens_today" типа "number"
    And тело ответа содержит поле "rate_limit" типа "number"
    And тело ответа содержит поле "rate_remaining" типа "number"
    And значение "rate_limit" равно 100
    And значение "rate_remaining" <= значение "rate_limit"

  Scenario: Потребитель без ключей — статистика с нулевыми значениями
    Given у пользователя нет API-ключей
    When пользователь отправляет GET-запрос на "/api/consumer/stats"
    Then статус ответа равен 200
    And значение "requests_today" равно 0
    And значение "rate_remaining" равно значению "rate_limit"

  Scenario: Статистика после нескольких запросов
    Given пользователь отправил 5 запросов к "/v1/chat/completions" за последний час
    When пользователь отправляет GET-запрос на "/api/consumer/stats"
    Then статус ответа равен 200
    And значение "requests_today" >= 5
    And значение "rate_remaining" <= 95

  Scenario: Статистика без аутентификации
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/consumer/stats"
    Then статус ответа равен 302

  Scenario: Статистика с истёкшей сессией
    Given сессионная cookie истекла
    When пользователь отправляет GET-запрос на "/api/consumer/stats"
    Then статус ответа равен 302
