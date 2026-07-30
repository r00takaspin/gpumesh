@api
Feature: Health Check
  GET /health для liveness/readiness.

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

  Scenario: Health check через HEAD
    When пользователь отправляет HEAD-запрос на "/health"
    Then статус ответа равен 200

  Scenario: Health check с Accept application/json
    When пользователь отправляет GET-запрос на "/health"
      And заголовок "Accept" равен "application/json"
    Then статус ответа равен 200
