@ui
Feature: API keys management (v2)
  Create / list / revoke consumer keys on /use?tab=keys.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен
    And пользователь аутентифицирован как "keyuser"

  Scenario: Empty keys list
    Given у пользователя нет consumer ключей
    When пользователь переходит на "/use?tab=keys"
    Then элемент с data-testid="keys-empty" видим
    And элемент с data-testid="btn-create-key" видим

  Scenario: Create key shows one-time banner
    When пользователь переходит на "/use?tab=keys"
    And пользователь кликает на data-testid="btn-create-key"
    Then элемент с data-testid="new-key-modal" видим
    And элемент с data-testid="new-key-value" видим
    And элемент с data-testid="btn-copy-new-key" видим
    And элемент с data-testid="btn-close-new-key-modal" видим

  Scenario: Key appears in list after create
    Given у пользователя нет consumer ключей
    When пользователь переходит на "/use?tab=keys"
    And пользователь кликает на data-testid="btn-create-key"
    And пользователь кликает на data-testid="btn-close-new-key-modal"
    Then элемент с data-testid="key-card" видим
    And элемент с data-testid="key-prefix" видим
    And элемент с data-testid="key-scope-label" содержит текст "for tools"

  Scenario: Revoke key
    Given у пользователя нет consumer ключей
    When пользователь переходит на "/use?tab=keys"
    And пользователь кликает на data-testid="btn-create-key"
    And пользователь кликает на data-testid="btn-close-new-key-modal"
    And пользователь подтверждает и кликает data-testid="btn-revoke-key"
    Then элемент с data-testid="keys-empty" видим
