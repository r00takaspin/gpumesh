@api
Feature: Жалобы POST /api/report
  Сигнал о злоупотреблении; опционально machine_id.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"

  Scenario: Успешная жалоба
    Given существует request_id "<request_id>"
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "request_id": "<request_id>",
          "reason": "spam",
          "machine_id": "mch_test"
        }
        """
    Then статус ответа равен 202
    And тело ответа содержит поле "status"

  Scenario: Без request_id — 400
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        { "reason": "spam" }
        """
    Then статус ответа равен 400

  Scenario: Без reason — 400
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        { "request_id": "req_12345" }
        """
    Then статус ответа равен 400

  Scenario: Несуществующий request_id — всё равно 202
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

  Scenario: Без auth — 401
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
