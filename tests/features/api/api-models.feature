@api
Feature: Список моделей (v2 discovery)
  GET /v1/models — owned+bindings.
  GET /v1/machines/{id}/models — одна машина.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And провайдер онлайн с моделью "llama3.2:3b" на машине "<machine_id>"

  Scenario: Discovery список моделей
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And тело ответа содержит поле "object" со значением "list"
    And тело ответа содержит поле "data" типа "array"
    And массив "data" содержит хотя бы 1 элемент

  Scenario: Элемент discovery содержит machine_id
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And каждый элемент массива "data" содержит поле "id" типа "string"
    And каждый элемент массива "data" содержит поле "machine_id" типа "string"
    And каждый элемент массива "data" содержит поле "online" типа "boolean"
    And каждый элемент массива "data" содержит поле "load" типа "number"

  Scenario: Per-machine models
    When пользователь отправляет GET-запрос на "/v1/machines/<machine_id>/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And тело ответа содержит поле "object" со значением "list"
    And массив "data" содержит хотя бы 1 элемент

  Scenario: Без API-ключа — 401
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" отсутствует
    Then статус ответа равен 401

  Scenario: CORS preflight
    When пользователь отправляет OPTIONS-запрос на "/v1/models"
      And заголовок "Origin" равен "https://example.com"
    Then статус ответа равен 200 или 204
    And заголовок "Access-Control-Allow-Origin" равен "*"

  Scenario: Нет доступных машин — пустой data
    Given у пользователя нет доступных машин
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And массив "data" пуст
