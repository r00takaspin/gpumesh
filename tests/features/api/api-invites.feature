@api
Feature: Invites и bindings
  Owner создаёт PIN; member redeem; ACL на machine.

  Background:
    Given координатор запущен и доступен
    And owner аутентифицирован через GitHub OAuth
    And у owner есть машина "<machine_id>" с провайдером онлайн

  Scenario: Создание invite
    When owner отправляет POST-запрос на "/api/invites"
      And тело запроса содержит:
        """
        {
          "machine_id": "<machine_id>",
          "max_uses": 1,
          "ttl_days": 7
        }
        """
    Then статус ответа равен 201
    And тело ответа содержит поле "pin" типа "string"
    And тело ответа содержит поле "join_link" типа "string"
    And тело ответа содержит поле "machine_id" со значением "<machine_id>"

  Scenario: Список invites
    Given у owner есть активный invite
    When owner отправляет GET-запрос на "/api/invites"
    Then статус ответа равен 200
    And тело ответа содержит поле "invites" типа "array"
    And массив "invites" содержит хотя бы 1 элемент

  Scenario: Redeem PIN создаёт binding
    Given существует активный PIN "<pin>" для машины "<machine_id>"
    And member аутентифицирован через GitHub OAuth
    When member отправляет POST-запрос на "/api/join"
      And тело запроса содержит:
        """
        { "pin": "<pin>" }
        """
    Then статус ответа равен 200
    And тело ответа содержит поле "machine_id" со значением "<machine_id>"
    And тело ответа содержит поле "base_url"

  Scenario: Неверный PIN — invalid_pin
    Given member аутентифицирован через GitHub OAuth
    When member отправляет POST-запрос на "/api/join"
      And тело запроса содержит:
        """
        { "pin": "AAAA-AAAA" }
        """
    Then статус ответа равен 400
    And тело ответа содержит поле "error.code" со значением "invalid_pin"

  Scenario: Список bindings у member
    Given member имеет binding на "<machine_id>"
    When member отправляет GET-запрос на "/api/bindings"
    Then статус ответа равен 200
    And тело ответа содержит поле "bindings" типа "array"
    And bindings содержат machine_id "<machine_id>"

  Scenario: Owner revoke member — затем 403 на inference
    Given member имеет binding на "<machine_id>"
    And у member есть consumer API-ключ "<member_key>"
    When owner отзывает member с машины "<machine_id>"
    Then статус ответа равен 200
    When member отправляет POST-запрос на "/v1/machines/<machine_id>/chat/completions"
      And заголовок "Authorization" равен "Bearer <member_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hi"}],
          "stream": false
        }
        """
    Then статус ответа равен 403

  Scenario: Member self-remove binding
    Given member имеет binding на "<machine_id>"
    When member отправляет DELETE-запрос на "/api/bindings/<machine_id>"
    Then статус ответа равен 200
