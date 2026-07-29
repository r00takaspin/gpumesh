@api
Feature: Управление API-ключами
  CRUD-операции с API-ключами: создание, просмотр, отзыв, перевыпуск.
  Эндпоинты: POST /api/keys, GET /api/keys, DELETE /api/keys/{id},
  POST /api/keys/{id}/regenerate.

  Background:
    Given координатор запущен и доступен
    And пользователь аутентифицирован через GitHub OAuth
    And сессионная cookie валидна

  Scenario: Создание нового API-ключа (scope: consumer)
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        {
          "scope": "consumer"
        }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "id" типа "number"
    And тело ответа содержит поле "key" типа "string"
    And значение "key" начинается с "inf_"
    And тело ответа содержит поле "key_prefix" типа "string"
    And значение "key_prefix" равно первым 12 символам "key"
    And тело ответа содержит поле "scope" со значением "consumer"

  Scenario: Создание ключа со scope "donor"
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        {
          "scope": "donor"
        }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope" со значением "donor"

  Scenario: Создание ключа со scope "both"
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        {
          "scope": "both"
        }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope" со значением "both"

  Scenario Outline: Создание ключа с невалидным scope
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        {
          "scope": "<invalid_scope>"
        }
        """
    Then статус ответа равен 201

    Examples:
      | invalid_scope |
      | admin         |
      | ""            |
      | 123           |

  Scenario: Создание ключа без поля scope
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса содержит:
        """
        {}
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "scope"

  Scenario: Получение списка ключей пользователя
    Given у пользователя есть 2 API-ключа
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 200
    And тело ответа содержит поле "keys" типа "array"
    And массив "keys" содержит хотя бы 2 элемент
    And каждый элемент "keys" содержит поле "id" типа "number"
    And каждый элемент "keys" содержит поле "prefix" типа "string"
    And каждый элемент "keys" содержит поле "scope" типа "string"
    And каждый элемент "keys" содержит поле "created_at" типа "string"
    And ни один элемент "keys" не содержит поле "key"

  Scenario: Получение списка ключей — у пользователя нет ключей
    Given у пользователя нет API-ключей
    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 200
    And массив "keys" пуст

  Scenario: Отзыв API-ключа
    Given у пользователя есть API-ключ с id "<key_id>"
    When пользователь отправляет DELETE-запрос на "/api/keys/<key_id>"
    Then статус ответа равен 200
    And тело ответа содержит поле "revoked" со значением true

    When пользователь повторно отправляет DELETE-запрос на "/api/keys/<key_id>"
    Then статус ответа равен 404

  Scenario: Отзыв несуществующего ключа
    When пользователь отправляет DELETE-запрос на "/api/keys/99999"
    Then статус ответа равен 404

  Scenario: Отзыв чужого ключа
    Given существует API-ключ с id "<other_key_id>", принадлежащий другому пользователю
    When пользователь отправляет DELETE-запрос на "/api/keys/<other_key_id>"
    Then статус ответа равен 404

  Scenario: Перевыпуск донорского токена
    Given у пользователя есть донорский ключ с id "<key_id>" и scope "donor"
    And старый ключ равен "<old_key>"
    When пользователь отправляет POST-запрос на "/api/keys/<key_id>/regenerate"
    Then статус ответа равен 200
    And тело ответа содержит поле "key" типа "string"
    And значение "key" не равно "<old_key>"
    And значение "key" начинается с "inf_"

    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <old_key>"
    Then статус ответа равен 401

  Scenario: Перевыпуск consumer-ключа
    Given у пользователя есть consumer-ключ с id "<key_id>" и scope "consumer"
    When пользователь отправляет POST-запрос на "/api/keys/<key_id>/regenerate"
    Then статус ответа равен 200
    And тело ответа содержит поле "key"

  Scenario: Доступ к эндпоинтам ключей без аутентификации
    Given пользователь не аутентифицирован
    When пользователь отправляет POST-запрос на "/api/keys"
    Then статус ответа равен 302

    When пользователь отправляет GET-запрос на "/api/keys"
    Then статус ответа равен 302

    When пользователь отправляет DELETE-запрос на "/api/keys/1"
    Then статус ответа равен 302
