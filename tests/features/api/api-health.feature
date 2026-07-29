@api
Feature: Health Check
  Эндпоинт GET /health возвращает статус координатора для Kubernetes liveness/readiness проб.

  Background:
    Given координатор запущен и доступен

  Scenario: Успешный health check
    When пользователь отправляет GET-запрос на "/health"
    Then статус ответа равен 200
    And тело ответа равно "OK"

  Scenario: Health check доступен без аутентификации
    When пользователь отправляет GET-запрос на "/health"
      And заголовок "Authorization" отсутствует
    Then статус ответа равен 200
    And тело ответа равно "OK"

  Scenario: Health check доступен через любой HTTP-метод
    When пользователь отправляет HEAD-запрос на "/health"
    Then статус ответа равен 200

  Scenario: Health check доступен с некорректным Accept
    When пользователь отправляет GET-запрос на "/health"
      And заголовок "Accept" равен "application/json"
    Then статус ответа равен 200

  Scenario: Health check возвращает ошибку при недоступности БД
    Given база данных недоступна
    When пользователь отправляет GET-запрос на "/health"
    Then статус ответа равен 503
