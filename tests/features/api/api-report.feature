@api
Feature: Жалобы на ответ донора
  Эндпоинт POST /api/report позволяет потребителю пожаловаться на ответ донора.
  Принимает request_id и причину жалобы.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"

  Scenario: Успешная отправка жалобы
    Given существует завершённый запрос с request_id "<request_id>"
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And заголовок "Content-Type" равен "application/json"
      And тело запроса содержит:
        """
        {
          "request_id": "<request_id>",
          "reason": "spam"
        }
        """
    Then статус ответа равен 200 или 202
    And тело ответа содержит поле "status"

  Scenario Outline: Жалоба с разными причинами
    Given существует завершённый запрос с request_id "<request_id>"
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "request_id": "<request_id>",
          "reason": "<reason>"
        }
        """
    Then статус ответа равен 200 или 202

    Examples:
      | reason          |
      | spam            |
      | harmful         |
      | offensive       |
      | low_quality     |
      | other           |

  Scenario: Жалоба без request_id
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "reason": "spam"
        }
        """
    Then статус ответа равен 400

  Scenario: Жалоба без поля reason
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "request_id": "req_12345"
        }
        """
    Then статус ответа равен 400

  Scenario: Жалоба на несуществующий request_id
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "request_id": "nonexistent_req_id",
          "reason": "spam"
        }
        """
    Then статус ответа равен 202

  Scenario: Жалоба без аутентификации
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" отсутствует
      And тело запроса содержит:
        """
        {
          "request_id": "req_12345",
          "reason": "spam"
        }
        """
    Then статус ответа равен 401

  Scenario: Жалоба с недействительным API-ключом
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer invalid_key"
      And тело запроса содержит:
        """
        {
          "request_id": "req_12345",
          "reason": "spam"
        }
        """
    Then статус ответа равен 401
