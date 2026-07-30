@ui
Feature: Models catalog removed (v2)
  Public /models marketplace is gone; route redirects to /use.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: GET /models redirects to /use
    When пользователь переходит на "/models"
    Then URL страницы равен "/use"
