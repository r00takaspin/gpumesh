@ui
Feature: Страница About
  Публичная страница `/about` с объяснением проекта, ASCII-диаграммой,
  FAQ и карточками для потребителей и доноров.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен
    And пользователь переходит на "/about"

  Scenario: Отображение заголовка и hero
    Then заголовок страницы содержит "About"
    And элемент с data-testid="about-hero" видим
    And элемент с data-testid="about-title" содержит текст "What is GPU Mesh?"

  Scenario: Отображение ASCII-диаграммы
    Then элемент с data-testid="about-diagram" видим
    And диаграмма содержит ASCII-графику с компонентами координатора и доноров

  Scenario: Отображение секции «In plain language»
    Then элемент с data-testid="about-plain-language" видим
    And секция содержит аналогию "community garden for GPUs"

  Scenario: Отображение секции «How it works»
    Then элемент с data-testid="about-how-it-works" видим
    And отображаются три шага: "Share", "Match", "Use"

  Scenario: Отображение карточек «Who is this for»
    Then элемент с data-testid="about-whos-for" видим
    And отображается карточка "Consumers"
    And отображается карточка "Donors"

  Scenario: Отображение ключевых фактов
    Then элемент с data-testid="about-key-facts" видим
    And список фактов включает: "Free", "Open source", "Privacy-first", "Community-driven", "Standard API"

  Scenario: Отображение FAQ
    Then элемент с data-testid="about-faq" видим
    And FAQ содержит раскрывающиеся вопросы и ответы
    And каждый вопрос представлен элементом <details> с <summary>

  Scenario: Раскрытие FAQ-вопроса
    Given FAQ-вопрос с data-testid="faq-item-0" закрыт
    When пользователь кликает на data-testid="faq-item-0"
    Then ответ на FAQ-вопрос видим

  Scenario: Навигация — ссылка на Home
    When пользователь кликает на ссылку с data-testid="nav-home"
    Then URL страницы равен "/"

  Scenario: Страница доступна без аутентификации
    Given пользователь не аутентифицирован
    Then кнопка "Sign in with GitHub" видима в навбаре
    And весь контент страницы видим

  Scenario: Отображение для авторизованного пользователя
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/about"
    Then элемент с data-testid="nav-username" содержит текст "testuser"
    And кнопка "Logout" видима
