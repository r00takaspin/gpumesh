@ui
Feature: Use machines dashboard (v2)
  /use shows owned and bound machines with per-machine base URLs — no community catalog.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Logged-out pitch
    Given пользователь не аутентифицирован
    When пользователь переходит на "/use"
    Then элемент с data-testid="use-title" содержит текст "Your machines"
    And элемент с data-testid="btn-signin-use" видим
    And элемент с data-testid="cta-enter-code" имеет href "/join"
    And элемент с data-testid="privacy-notice" видим

  Scenario: Empty machines state
    Given пользователь аутентифицирован как "lonely"
    And у пользователя нет машин
    When пользователь переходит на "/use"
    Then элемент с data-testid="machines-empty" содержит текст "No machines yet"
    And элемент с data-testid="privacy-notice" видим

  Scenario: Owned machine card with base URL
    Given пользователь аутентифицирован как "owner1"
    And у пользователя есть provider токен и онлайн машина
    When пользователь переходит на "/use"
    Then элемент с data-testid="machine-card" видим
    And элемент с data-testid="machine-name" видим
    And элемент с data-testid="btn-copy-base-url" видим
    And элемент с data-testid="machine-meta" содержит текст "owned by you"

  Scenario: Snippets use per-machine path
    Given пользователь аутентифицирован как "owner-snip"
    And у пользователя есть provider токен и онлайн машина
    When пользователь переходит на "/use"
    And пользователь кликает на data-testid="btn-toggle-snippets"
    Then элемент с data-testid="snippets-panel" видим
    And элемент с data-testid="snippet-code" содержит текст "/v1/machines/"

  Scenario: API Keys tab
    Given пользователь аутентифицирован как "owner1"
    When пользователь переходит на "/use?tab=keys"
    Then элемент с data-testid="use-title" содержит текст "API Keys"
    And элемент с data-testid="btn-create-key" видим

  Scenario: One-time key banner after new=1
    Given пользователь аутентифицирован как "newconsumer"
    And у пользователя нет consumer ключей
    When пользователь переходит на "/use?new=1"
    Then элемент с data-testid="onetime-key" видим
    And элемент с data-testid="onetime-key-value" видим

  Scenario: /dashboard redirects to /use
    When пользователь переходит на "/dashboard"
    Then URL страницы равен "/use"

  Scenario: No community Models tab
    Given пользователь аутентифицирован как "owner1"
    When пользователь переходит на "/use"
    Then элемент с data-testid="tab-models" не видим
    And страница не содержит текст "Donors online"
