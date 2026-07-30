@ui
Feature: Login page (v2)

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Sign in card
    When пользователь переходит на "/login"
    Then элемент с data-testid="login-card" видим
    And элемент с data-testid="login-title" содержит текст "Sign in"
    And элемент с data-testid="btn-github-login" содержит текст "Sign in with GitHub"

  Scenario: Why GitHub
    When пользователь переходит на "/login"
    Then элемент с data-testid="login-why-github" видим

  Scenario: Redirect query preserved
    When пользователь переходит на "/login?redirect=/join"
    Then элемент с data-testid="btn-github-login" имеет href, содержащий "redirect="

  Scenario: Logout clears session
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/"
    And пользователь кликает на data-testid="btn-logout"
    Then элемент с data-testid="btn-signin" видим
