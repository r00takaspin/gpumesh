@ui
Feature: Лендинг страница
  Публичная главная страница `/` с hero-секцией, живой статистикой,
  блоком «How it works» и топ-моделями.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Отображение hero-секции
    When пользователь переходит на "/"
    Then заголовок страницы содержит "GPU Mesh"
    And элемент с data-testid="hero-logo" отображает ASCII-логотип "GPU MESH"
    And элемент с data-testid="hero-title" содержит текст "Free LLM inference"
    And элемент с data-testid="hero-subtitle" видим
    And элемент с data-testid="hero-cta-use" содержит текст "Use Models"
    And элемент с data-testid="hero-cta-use" имеет href "/use"
    And элемент с data-testid="hero-cta-share" содержит текст "Share GPU"
    And элемент с data-testid="hero-cta-share" имеет href "/share"

  Scenario: Отображение живой статистики
    When пользователь переходит на "/"
    Then элемент с data-testid="live-stats" видим
    And элемент с data-testid="stat-models-online" отображает число
    And элемент с data-testid="stat-donors-online" отображает число
    And элемент с data-testid="stat-requests-today" отображает число

  Scenario: Отображение блока «How it works»
    When пользователь переходит на "/"
    Then элемент с data-testid="how-it-works" видим
    And отображаются три шага: "Share", "Match", "Use"
    And каждый шаг содержит нумерованный кружок (1, 2, 3)

  Scenario: Отображение топ-моделей при наличии доноров
    Given в реестре есть онлайн-доноры с моделями
    When пользователь переходит на "/"
    Then секция с data-testid="top-models" видима
    And отображается до 5 карточек моделей
    And каждая карточка модели содержит название модели
    And каждая карточка модели содержит количество доноров
    And каждая карточка модели содержит бейдж "live"

  Scenario: Пустое состояние топ-моделей
    Given реестр доноров пуст
    When пользователь переходит на "/"
    Then секция с data-testid="top-models" видима
    And отображается текст "No models online"
    And присутствует ссылка "Browse all models" на "/models"

  Scenario: Навигация — ссылка «Use Models» ведёт на /use
    When пользователь переходит на "/"
    And пользователь кликает на ссылку с data-testid="nav-use"
    Then URL страницы равен "/use"

  Scenario: Навигация — ссылка «Share GPU» ведёт на /share
    When пользователь переходит на "/"
    And пользователь кликает на ссылку с data-testid="nav-share"
    Then URL страницы равен "/share"

  Scenario: Навигация — логотип ведёт на "/"
    When пользователь переходит на "/models"
    And пользователь кликает на логотип с data-testid="logo"
    Then URL страницы равен "/"

  Scenario: Отображение футера
    When пользователь переходит на "/"
    Then элемент с data-testid="footer" видим
    And футер содержит ссылку на GitHub-репозиторий
    And футер содержит текст "Powered by community"
    And футер содержит текст "MIT"

  Scenario: Кнопка «Sign in with GitHub» для неавторизованных
    Given пользователь не аутентифицирован
    When пользователь переходит на "/"
    Then элемент с data-testid="btn-signin" видим
    And элемент с data-testid="btn-signin" содержит текст "Sign in with GitHub"

  Scenario: Отображение имени пользователя для авторизованных
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/"
    Then элемент с data-testid="nav-username" видим
    And элемент с data-testid="nav-username" содержит текст "testuser"
    And элемент с data-testid="btn-logout" видим
