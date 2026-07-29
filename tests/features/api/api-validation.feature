@api
Feature: Валидация запросов — граничные случаи
  Проверка обработки некорректных, пустых и граничных входных данных
  на всех эндпоинтах API.

  Background:
    Given координатор запущен и доступен
    And существует валидный API-ключ "<valid_api_key>"
    And в реестре есть онлайн-донор с моделью "llama3.2:3b"

  # ===== /v1/chat/completions =====

  Scenario Outline: Невалидный JSON в теле запроса
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And заголовок "Content-Type" равен "application/json"
      And тело запроса содержит:
        """
        <body>
        """
    Then статус ответа равен 400

    Examples:
      | body                  |
      | {invalid json}        |
      | ""                    |
      | null                  |
      | "just a string"       |
      | 12345                 |

  Scenario Outline: Невалидные значения параметров
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "Hi"}],
          "<param>": <value>
        }
        """
    Then статус ответа равен 200 или 400

    Examples:
      | param         | value     |
      | temperature   | -1        |
      | temperature   | 3.5       |
      | temperature   | "hot"     |
      | max_tokens    | -10       |
      | max_tokens    | 0         |
      | max_tokens    | "many"    |
      | top_p         | -0.1      |
      | top_p         | 2.0       |
      | top_p         | "high"    |
      | stream        | "yes"     |
      | stream        | 1         |

  Scenario Outline: Пустые и граничные значения поля content
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "<content>"}],
          "stream": false
        }
        """
    Then статус ответа равен 200

    Examples:
      | content                                                |
      |                                                        |
      | Привет                                                 |
      | a                                                      |

  Scenario: Очень длинное сообщение
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "user", "content": "<very_long_text>"}],
          "stream": false
        }
        """
    Then статус ответа равен 200 или 413

  Scenario Outline: Невалидные значения role в messages
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "llama3.2:3b",
          "messages": [{"role": "<role>", "content": "Hello"}],
          "stream": false
        }
        """
    Then статус ответа равен 200 или 400

    Examples:
      | role      |
      | admin     |
      | moderator |
      | ""        |
      | bot       |

  Scenario: Пустое поле model
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "model": "",
          "messages": [{"role": "user", "content": "Hello"}]
        }
        """
    Then статус ответа равен 400 или 503

  Scenario: Отсутствует Content-Type заголовок
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And заголовок "Content-Type" отсутствует
      And тело запроса содержит валидный JSON
    Then статус ответа равен 200 или 400

  Scenario: Неверный Content-Type
    When пользователь отправляет POST-запрос на "/v1/chat/completions"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And заголовок "Content-Type" равен "text/plain"
      And тело запроса содержит валидный JSON
    Then статус ответа равен 200 или 415

  # ===== /api/report =====

  Scenario Outline: Невалидные поля в жалобе
    When пользователь отправляет POST-запрос на "/api/report"
      And заголовок "Authorization" равен "Bearer <valid_api_key>"
      And тело запроса содержит:
        """
        {
          "request_id": "req_123",
          "reason": <reason>
        }
        """
    Then статус ответа равен 400

    Examples:
      | reason     |
      | ""         |
      | null       |
      | 12345      |

  # ===== /api/keys =====

  Scenario: Создание ключа с пустым телом запроса
    Given пользователь аутентифицирован через GitHub OAuth
    When пользователь отправляет POST-запрос на "/api/keys"
      And тело запроса пустое
    Then статус ответа равен 201

  # ===== Общие =====

  Scenario: Запрос с очень длинным заголовком Authorization
    When пользователь отправляет GET-запрос на "/v1/models"
      And заголовок "Authorization" равен "Bearer <very_long_token_10kb>"
    Then статус ответа равен 401 или 414

  Scenario: Запрос несуществующего эндпоинта
    When пользователь отправляет GET-запрос на "/v1/nonexistent"
    Then статус ответа равен 404

  Scenario: Неподдерживаемый HTTP-метод
    When пользователь отправляет PUT-запрос на "/v1/models"
    Then статус ответа равен 405

  Scenario: Запрос с не-ASCII символами в пути
    When пользователь отправляет GET-запрос на "/v1/модели"
    Then статус ответа равен 400 или 404
