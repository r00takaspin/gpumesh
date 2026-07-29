@ui
Feature: Дашборд потребителя
  Страница `/use` для авторизованных пользователей: три таба
  (Overview, API Keys, Models), one-time key display, статистика.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен
    And пользователь аутентифицирован как "testuser"

  # ===== Состояние logged-out =====

  Scenario: Отображение logged-out версии
    Given пользователь не аутентифицирован
    When пользователь переходит на "/use"
    Then элемент с data-testid="hero-logged-out" видим
    And заголовок содержит "Free LLM inference"
    And элемент с data-testid="btn-signin" видим
    And отображается блок «How it works» с тремя шагами
    And отображается живая статистика (Models online, Donors online, Requests today)

  Scenario: Кнопка входа на logged-out странице
    Given пользователь не аутентифицирован
    When пользователь переходит на "/use"
    And пользователь кликает на data-testid="btn-signin"
    Then URL содержит "/auth/github"
    And URL содержит параметр "redirect" со значением "/use"

  # ===== One-time key display =====

  Scenario: Отображение API-ключа при первом входе
    Given пользователь впервые заходит после OAuth с параметром "?new=1"
    When пользователь переходит на "/use?new=1"
    Then элемент с data-testid="one-time-key-banner" видим
    And баннер содержит полный API-ключ, начинающийся с "inf_"
    And баннер содержит иконку предупреждения "⚠"
    And баннер содержит текст "Copy this key now — it won't be shown again."

  Scenario: Копирование ключа через кнопку Copy
    Given отображается one-time key баннер
    When пользователь кликает на data-testid="btn-copy-key"
    Then API-ключ скопирован в буфер обмена
    And отображается текст "Copied!" на 2 секунды

  Scenario: Скрытие one-time key баннера
    Given отображается one-time key баннер
    When пользователь кликает на data-testid="btn-dismiss-key"
    Then one-time key баннер скрыт

  Scenario: Отображение префикса ключа при повторном входе
    Given пользователь уже видел свой ключ ранее
    When пользователь переходит на "/use"
    Then отображается префикс ключа (первые 8 символов)
    And полный ключ не отображается
    And кнопка dismiss отсутствует

  # ===== Таб Overview =====

  Scenario: Отображение таба Overview по умолчанию
    When пользователь переходит на "/use"
    Then таб с data-testid="tab-overview" активен
    And элемент с data-testid="usage-stats" видим
    And отображаются три блока статистики: "Requests today", "Tokens today", "Models available"

  Scenario: Блок «Try it now» с curl-командой
    Given в реестре есть доступные модели
    When пользователь переходит на "/use"
    Then элемент с data-testid="try-it-now" видим
    And блок содержит curl-команду с подставленным API-ключом
    And кнопка копирования команды видима

  Scenario: Блок «Try it now» скрыт при отсутствии моделей
    Given реестр доноров пуст
    When пользователь переходит на "/use"
    Then элемент с data-testid="try-it-now" не видим

  # ===== Таб API Keys =====

  Scenario: Переключение на таб API Keys
    When пользователь переходит на "/use"
    And пользователь кликает на data-testid="tab-api-keys"
    Then таб с data-testid="tab-api-keys" активен
    And элемент с data-testid="api-keys-list" видим

  Scenario: Список ключей с карточками
    Given у пользователя есть API-ключи
    When пользователь переходит на "/use?tab=keys"
    Then каждая карточка ключа содержит префикс
    And каждая карточка ключа содержит дату создания
    And каждая карточка ключа содержит бейдж scope
    And каждая карточка ключа содержит кнопку "Revoke"

  Scenario: Пустое состояние списка ключей
    Given у пользователя нет API-ключей
    When пользователь переходит на "/use?tab=keys"
    Then отображается кнопка с data-testid="btn-create-first-key"

  Scenario: Создание нового ключа
    When пользователь переходит на "/use?tab=keys"
    And пользователь кликает на data-testid="btn-create-key"
    Then новый ключ появляется в списке
    And отображается полный ключ с предупреждением

  # ===== Таб Models =====

  Scenario: Переключение на таб Models
    When пользователь переходит на "/use"
    And пользователь кликает на data-testid="tab-models"
    Then таб с data-testid="tab-models" активен
    And элемент с data-testid="models-list" видим

  Scenario: Карточки моделей с донорами
    Given в реестре есть модель "llama3.2:3b" с донорами
    When пользователь переходит на "/use?tab=models"
    Then карточка модели "llama3.2:3b" содержит бейдж "available"
    And карточка содержит количество доноров
    And карточка содержит процент загрузки
    And карточка содержит название вендора

  Scenario: Раскрытие tool rows в карточке модели
    Given отображается карточка модели "llama3.2:3b"
    When пользователь кликает на заголовок карточки
    Then отображаются 7 строк с инструментами:
      | инструмент    |
      | Continue.dev  |
      | Aider         |
      | Cline         |
      | Open WebUI    |
      | curl          |
      | Python SDK    |
      | Oh My Pi      |

  Scenario: Раскрытие конкретного tool row
    Given карточка модели "llama3.2:3b" раскрыта
    When пользователь кликает на tool row "Continue.dev"
    Then отображается JSON-конфигурация для Continue.dev
    And кнопка копирования видима

  Scenario: Копирование конфигурации инструмента
    Given раскрыт tool row "curl"
    When пользователь кликает на кнопку копирования
    Then curl-команда скопирована в буфер обмена
    And отображается "Copied!" на 2 секунды

  Scenario: Пустое состояние таба Models
    Given реестр доноров пуст
    When пользователь переходит на "/use?tab=models"
    Then отображается текст "No models available"

  # ===== Редирект /dashboard =====

  Scenario: /dashboard редиректит на /use
    When пользователь переходит на "/dashboard"
    Then URL страницы равен "/use"
    And статус ответа исходного запроса равен 302

  # ===== Навигация =====

  Scenario: Активная ссылка в навбаре для /use
    When пользователь переходит на "/use"
    Then ссылка с data-testid="nav-use" имеет класс "active"
