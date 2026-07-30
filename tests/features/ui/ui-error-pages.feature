@ui
Feature: Error pages (v2)

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: 404 page
    When пользователь переходит на "/test/error?code=404"
    Then элемент с data-testid="error-page" видим
    And элемент с data-testid="error-code" содержит текст "404"
    And элемент с data-testid="btn-go-home" имеет href "/"

  Scenario: 500 page
    When пользователь переходит на "/test/error?code=500"
    Then элемент с data-testid="error-code" содержит текст "500"

  Scenario: 503 page
    When пользователь переходит на "/test/error?code=503"
    Then элемент с data-testid="error-code" содержит текст "503"

  Scenario: Error pages have nav and footer
    When пользователь переходит на "/test/error?code=404"
    Then элемент с data-testid="nav-home" видим
    And элемент с data-testid="footer" видим
    And элемент с data-testid="footer-tagline" содержит текст "Share local models with friends"
