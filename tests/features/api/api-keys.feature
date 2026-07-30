@api
Feature: Управление API-ключами
  CRUD: POST/GET/DELETE /api/keys, regenerate.

  Background:
    Given координатор запущен и доступен
    And пользователь аутентифицирован через GitHub OAuth
    And сессионная cookie валидна

  Scenario: Создание consumer ключа
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        { "scope": "consumer" }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "id" типа "number"
    And тело ответа содержит поле "key" типа "string"
    And значение "key" начинается с "inf_"
    And тело ответа содержит поле "key_prefix" типа "string"
    And значение "key_prefix" равно первым 12 символам "key"
    And тело ответа содержит поле "scope" со значением "consumer"

  Scenario: Создание provider ключа
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        { "scope": "provider" }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope" со значением "provider"

  Scenario: Legacy donor нормализуется в provider
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        { "scope": "donor" }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope" со значением "provider"

  Scenario: Создание both ключа
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        { "scope": "both" }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope" со значением "both"

  Scenario: Создание без scope — default consumer
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        {}
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope" со значением "consumer"

  Scenario: Список ключей
    Given у пользователя есть 2 API-ключа
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 200
    And тело ответа содержит поле "keys" типа "array"
    And массив "keys" содержит хотя бы 2 элемент
    And каждый элемент "keys" содержит поле "id" типа "number"
    And каждый элемент "keys" содержит поле "prefix" типа "string"
    And каждый элемент "keys" содержит поле "scope" типа "string"
    And ни один элемент "keys" не содержит поле "key"

  Scenario: Список пуст
    Given у пользователя нет API-ключей
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 200
    And массив "keys" пуст

  Scenario: Отзыв ключа
    Given у пользователя есть API-ключ с id "<key_id>"
    When пользователь отправляет DELETE-запрос на "/api/keys/<key_id>"
    Then статус ответа равен 200
    And тело ответа содержит поле "revoked" со значением true
    When пользователь повторно отправляет DELETE-запрос на "/api/keys/<key_id>"
    Then статус ответа равен 404

  Scenario: Отзыв несуществующего — 404
    When пользователь отправляет DELETE-запрос на "/api/keys/99999"
    Then статус ответа равен 404

  Scenario: Отзыв чужого — 404
    Given существует API-ключ с id "<other_key_id>", принадлежащий другому пользователю
    When пользователь отправляет DELETE-запрос на "/api/keys/<other_key_id>"
    Then статус ответа равен 404

  Scenario: Regenerate provider — новый machine_id
    Given у пользователя есть provider ключ с id "<key_id>" и scope "provider"
    And старый ключ равен "<old_key>"
    When пользователь отправляет POST-запрос на "/api/keys/<key_id>/regenerate"
    Then статус ответа равен 200
    And тело ответа содержит поле "key" типа "string"
    And тело ответа содержит поле "machine_id" типа "string"
    And значение "key" не равно "<old_key>"
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <old_key>"
    Then статус ответа равен 401

  Scenario: Regenerate consumer
    Given у пользователя есть consumer-ключ с id "<key_id>" и scope "consumer"
    When пользователь отправляет POST-запрос на "/api/keys/<key_id>/regenerate"
    Then статус ответа равен 200
    And тело ответа содержит поле "key"

  Scenario: Без сессии — 302
    Given пользователь не аутентифицирован
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 302
