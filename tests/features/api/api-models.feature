@api
Feature: Список доступных моделей
  Эндпоинт GET /v1/models возвращает список моделей, доступных для инференса,
  в OpenAI-совместимом формате с дополнительными полями donors_online и load.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And в реестре зарегистрированы доноры с моделями

  Scenario: Получение списка моделей с валидным API-ключом
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And заголовок "Content-Type" содержит "application/json"
    And тело ответа содержит поле "object" со значением "list"
    And тело ответа содержит поле "data" типа "array"

  Scenario: Получение списка моделей — структура элемента data
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And каждый элемент массива "data" содержит поле "id" типа "string"
    And каждый элемент массива "data" содержит поле "object" со значением "model"
    And каждый элемент массива "data" содержит поле "owned_by" типа "string"
    And каждый элемент массива "data" содержит поле "donors_online" типа "number"
    And каждый элемент массива "data" содержит поле "load" типа "number"
    And значение "donors_online" для каждого элемента >= 0
    And значение "load" для каждого элемента между 0 и 1

  Scenario: Запрос без API-ключа — пустой список моделей
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" отсутствует
    Then статус ответа равен 401

  Scenario: Запрос с недействительным API-ключом
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer invalid_key_xxx"
    Then статус ответа равен 401
    And тело ответа содержит поле "error"

  Scenario: Запрос с отозванным API-ключом
    Given существует отозванный API-ключ "<revoked_key>"
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <revoked_key>"
    Then статус ответа равен 401

  Scenario: CORS-заголовки присутствуют
    When пользователь отправляет OPTIONS-запрос на "/v1/models"
      And заголовок "Origin" равен "https://example.com"
    Then статус ответа равен 200 или 204
    And заголовок "Access-Control-Allow-Origin" равен "*"

  Scenario Outline: Пустой реестр — возвращается пустой список
    Given реестр доноров пуст
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
    Then статус ответа равен 200
    And массив "data" пуст

  Examples:
    | scenario          |
    | нет доноров онлайн |
