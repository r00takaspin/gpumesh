@ui
Feature: Каталог моделей
  Публичная страница `/models` с поиском, карточками моделей,
  бейджами доступности и HTMX-обновлением.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен
    And пользователь переходит на "/models"

  Scenario: Отображение заголовка страницы
    Then заголовок страницы содержит "Model Catalog"
    And элемент с data-testid="page-title" содержит текст "Model Catalog"

  Scenario: Отображение поисковой строки
    Then поле поиска с data-testid="model-search" видимо
    And placeholder поля поиска содержит "Search models"

  Scenario: Отображение карточек моделей с донорами
    Given в реестре есть онлайн-доноры с моделями
    When пользователь переходит на "/models"
    Then элемент с data-testid="model-list" содержит карточки моделей
    And каждая карточка модели содержит элемент с data-testid="model-name"
    And каждая карточка модели содержит количество доноров
    And каждая карточка модели содержит процент загрузки
    And каждая карточка модели содержит название вендора

  Scenario: Бейдж «available» для моделей с донорами
    Given в реестре есть модель "llama3.2:3b" с донорами онлайн
    When пользователь переходит на "/models"
    Then карточка модели "llama3.2:3b" содержит бейдж с data-testid="badge-available"
    And бейдж содержит текст "available"
    And бейдж имеет зелёный цвет

  Scenario: Бейдж «unavailable» для моделей без доноров
    Given в реестре есть модель "codellama:7b" без доноров онлайн
    When пользователь переходит на "/models"
    Then карточка модели "codellama:7b" содержит бейдж с data-testid="badge-unavailable"
    And бейдж содержит текст "unavailable"
    And бейдж имеет серый цвет

  Scenario: Пустое состояние каталога
    Given реестр доноров пуст
    When пользователь переходит на "/models"
    Then элемент с data-testid="model-list" содержит текст "No models available"

  Scenario: Фильтрация моделей через поиск
    Given в реестре есть модели "llama3.2:3b" и "codellama:7b"
    When пользователь вводит "llama" в поле с data-testid="model-search"
    Then видима только карточка модели, содержащая "llama"
    And карточка модели "codellama:7b" скрыта

  Scenario: Поиск несуществующей модели
    Given в реестре есть модель "llama3.2:3b"
    When пользователь вводит "nonexistent" в поле с data-testid="model-search"
    Then ни одна карточка модели не видима

  Scenario: Очистка поиска показывает все модели
    Given в реестре есть модели "llama3.2:3b" и "codellama:7b"
    And пользователь ввёл "llama" в поле поиска
    When пользователь очищает поле с data-testid="model-search"
    Then видимы все карточки моделей

  Scenario: HTMX polling обновляет список каждые 30 секунд
    Given в реестре есть модель "llama3.2:3b" с 2 донорами
    When проходит 30 секунд
    And появляется новый донор с моделью "llama3.2:3b"
    Then карточка модели "llama3.2:3b" показывает 3 донора

  Scenario: Навигация из каталога моделей
    When пользователь кликает на ссылку с data-testid="nav-use"
    Then URL страницы равен "/use"

  Scenario: Страница доступна без аутентификации
    Given пользователь не аутентифицирован
    When пользователь переходит на "/models"
    Then статус ответа равен 200
    And поисковая строка видима
    And кнопка "Sign in with GitHub" видима в навбаре
