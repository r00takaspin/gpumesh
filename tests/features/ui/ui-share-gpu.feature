@ui
Feature: Страница донора Share GPU
  Страница `/share` с двумя состояниями: logged-out (публичный лендинг)
  и logged-in (дашборд донора: Setup, Agent Status, Stats).

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  # ===== Состояние logged-out =====

  Scenario: Отображение logged-out версии
    Given пользователь не аутентифицирован
    When пользователь переходит на "/share"
    Then элемент с data-testid="hero-share" видим
    And заголовок содержит "Share your GPU"
    And подзаголовок содержит "Run the agent, serve LLM requests"
    And элемент с data-testid="btn-signin" видим
    And отображается блок «How it works» с шагами:
      | шаг                          |
      | Install Ollama               |
      | Pull a model                 |
      | Run the agent                |

  Scenario: Кнопка входа с редиректом на /share
    Given пользователь не аутентифицирован
    When пользователь переходит на "/share"
    And пользователь кликает на data-testid="btn-signin"
    Then URL содержит "/auth/github"
    And URL содержит параметр "redirect" со значением "/share"

  # ===== Состояние logged-in =====

  Scenario: Отображение logged-in версии
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/share"
    Then навбар отображает "testuser"
    And ссылка с data-testid="nav-share" имеет класс "active"

  Scenario: Блок Setup отображается
    Given пользователь аутентифицирован
    When пользователь переходит на "/share"
    Then элемент с data-testid="share-setup" видим
    And блок содержит инструкцию по установке провайдера
    And блок содержит команду для запуска

  Scenario: Отображение донорского токена в Setup
    Given у пользователя есть донорский токен
    When пользователь переходит на "/share"
    Then элемент с data-testid="donor-token" содержит токен, начинающийся с "inf_"

  Scenario: Предупреждение при отсутствии донорского токена
    Given у пользователя нет донорского токена
    When пользователь переходит на "/share"
    Then элемент с data-testid="no-token-warning" видим
    And предупреждение предлагает создать токен

  Scenario: Создание донорского токена
    Given у пользователя нет донорского токена
    When пользователь кликает на data-testid="btn-create-donor-token"
    Then отображается модальное окно с data-testid="modal-donor-token"
    And модальное окно содержит новый токен

  Scenario: Закрытие модального окна с токеном
    Given открыто модальное окно с новым донорским токеном
    When пользователь кликает на data-testid="btn-close-modal"
    Then модальное окно закрыто

  Scenario: Отображение карточек агентов
    Given у пользователя есть подключённый агент донора
    When пользователь переходит на "/share"
    Then элемент с data-testid="share-models" видим
    And отображается карточка агента с данными:
      | поле         |
      | provider_id  |
      | models       |
      | description  |
      | hardware     |
      | uptime       |

  Scenario: Пустое состояние агентов
    Given у пользователя нет подключённых агентов
    When пользователь переходит на "/share"
    Then элемент с data-testid="share-models" видим
    And отображается сообщение об отсутствии агентов

  Scenario: Отображение статистики донора
    Given пользователь аутентифицирован
    When пользователь переходит на "/share"
    Then элемент с data-testid="donor-stats" видим
    And отображается "total_requests"
    And отображается "total_tokens"
    And отображается "total_uptime_seconds"
    And отображается бейдж донора

  Scenario: HTMX polling для Setup
    Given Setup блок загружен
    When проходит 5 секунд
    Then Setup блок обновляется

  Scenario: HTMX polling для Agent Status
    Given агенты отображаются
    When проходит 10 секунд
    Then карточки агентов обновляются

  Scenario: HTMX polling для Stats
    Given статистика отображается
    When проходит 60 секунд
    Then статистика обновляется

  Scenario: Активная ссылка в навбаре для /share
    Given пользователь аутентифицирован
    When пользователь переходит на "/share"
    Then ссылка с data-testid="nav-share" имеет класс "active"
