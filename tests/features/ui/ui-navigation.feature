@ui
Feature: Navigation (v2)
  Home · Join · Use · Share · About — no Models catalog link.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario Outline: Navbar links
    Given пользователь не аутентифицирован
    When пользователь переходит на "<from>"
    And пользователь кликает на ссылку с data-testid="<nav_id>"
    Then URL страницы равен "<to>"

    Examples:
      | from   | nav_id    | to     |
      | /      | nav-home  | /      |
      | /      | nav-join  | /join  |
      | /      | nav-use   | /use   |
      | /      | nav-share | /share |
      | /      | nav-about | /about |
      | /join  | nav-home  | /      |
      | /use   | nav-join  | /join  |
      | /share | nav-about | /about |

  Scenario Outline: Active nav highlight
    When пользователь переходит на "<page>"
    Then ссылка с data-testid="<active_nav>" имеет класс "on"

    Examples:
      | page   | active_nav |
      | /      | nav-home   |
      | /join  | nav-join   |
      | /use   | nav-use    |
      | /share | nav-share  |
      | /about | nav-about  |

  Scenario: /dashboard redirects to /use
    When пользователь переходит на "/dashboard"
    Then URL страницы равен "/use"

  Scenario: /models redirects to /use
    When пользователь переходит на "/models"
    Then URL страницы равен "/use"

  Scenario: Logo goes home
    When пользователь переходит на "/about"
    And пользователь кликает на data-testid="logo"
    Then URL страницы равен "/"

  Scenario: Guest chrome
    Given пользователь не аутентифицирован
    When пользователь переходит на "/"
    Then элемент с data-testid="nav-home" видим
    And элемент с data-testid="nav-join" видим
    And элемент с data-testid="nav-use" видим
    And элемент с data-testid="nav-share" видим
    And элемент с data-testid="nav-about" видим
    And элемент с data-testid="nav-models" не видим
    And элемент с data-testid="btn-signin" видим
    And элемент с data-testid="nav-username" не видим
    And элемент с data-testid="btn-logout" не видим

  Scenario: Logged-in chrome
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/"
    Then элемент с data-testid="nav-username" содержит текст "testuser"
    And элемент с data-testid="btn-logout" видим
    And элемент с data-testid="btn-signin" не видим

  Scenario: Footer on main pages
    When пользователь переходит на "/"
    Then элемент с data-testid="footer" видим
    And элемент с data-testid="footer-license" содержит текст "MIT"
    When пользователь переходит на "/join"
    Then элемент с data-testid="footer" видим
    When пользователь переходит на "/use"
    Then элемент с data-testid="footer" видим
    When пользователь переходит на "/share"
    Then элемент с data-testid="footer" видим
    When пользователь переходит на "/about"
    Then элемент с data-testid="footer" видим
