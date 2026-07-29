@ui
Feature: Навигация и маршрутизация
  Проверка всех переходов между страницами, активных состояний ссылок,
  редиректов и структуры навбара.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario Outline: Переход по ссылкам навбара (неавторизованный)
    Given пользователь не аутентифицирован
    When пользователь переходит на "<from>"
    And пользователь кликает на ссылку с data-testid="<nav_id>"
    Then URL страницы равен "<to>"

    Examples:
      | from      | nav_id     | to       |
      | /         | nav-home   | /        |
      | /         | nav-use    | /use     |
      | /         | nav-share  | /share   |
      | /         | nav-models | /models  |
      | /         | nav-about  | /about   |
      | /models   | nav-home   | /        |
      | /models   | nav-use    | /use     |
      | /models   | nav-share  | /share   |
      | /models   | nav-about  | /about   |
      | /about    | nav-home   | /        |
      | /about    | nav-use    | /use     |
      | /about    | nav-share  | /share   |
      | /about    | nav-models | /models  |

  Scenario Outline: Активная подсветка ссылок навбара
    When пользователь переходит на "<page>"
    Then ссылка с data-testid="<active_nav>" имеет класс "active"

    Examples:
      | page     | active_nav  |
      | /        | nav-home    |
      | /use     | nav-use     |
      | /share   | nav-share   |
      | /models  | nav-models  |
      | /about   | nav-about   |

  Scenario: Редирект /dashboard на /use
    When пользователь переходит на "/dashboard"
    Then URL страницы равен "/use"

  Scenario: Логотип всегда ведёт на "/"
    When пользователь переходит на "/models"
    And пользователь кликает на data-testid="logo"
    Then URL страницы равен "/"

    When пользователь переходит на "/about"
    And пользователь кликает на data-testid="logo"
    Then URL страницы равен "/"

  Scenario: Навбар для неавторизованного пользователя
    Given пользователь не аутентифицирован
    When пользователь переходит на "/"
    Then элемент с data-testid="nav-home" видим
    And элемент с data-testid="nav-use" видим
    And элемент с data-testid="nav-share" видим
    And элемент с data-testid="nav-models" видим
    And элемент с data-testid="nav-about" видим
    And элемент с data-testid="btn-signin" видим
    And элемент с data-testid="nav-username" не видим
    And элемент с data-testid="btn-logout" не видим

  Scenario: Навбар для авторизованного пользователя
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/"
    Then элемент с data-testid="nav-username" содержит текст "testuser"
    And элемент с data-testid="btn-logout" видим
    And элемент с data-testid="btn-signin" не видим

  Scenario: Навбар содержит все 5 ссылок на любой странице
    When пользователь переходит на "/models"
    Then все ссылки навбара (Home, Use Models, Share GPU, Models, About) видимы

  Scenario: Футер присутствует на всех страницах
    When пользователь переходит на "/"
    Then элемент с data-testid="footer" видим

    When пользователь переходит на "/use"
    Then элемент с data-testid="footer" видим

    When пользователь переходит на "/share"
    Then элемент с data-testid="footer" видим

    When пользователь переходит на "/models"
    Then элемент с data-testid="footer" видим

    When пользователь переходит на "/about"
    Then элемент с data-testid="footer" видим

  Scenario: Футер содержит ссылку на GitHub
    When пользователь переходит на "/"
    Then элемент с data-testid="footer-github-link" имеет href, содержащий "github.com/r00takaspin/gpumesh"

  Scenario: Страницы открываются по прямым ссылкам (не SPA)
    When пользователь напрямую переходит по URL "/models"
    Then статус ответа равен 200
    And страница содержит навбар
    And страница содержит контент каталога моделей

    When пользователь напрямую переходит по URL "/about"
    Then статус ответа равен 200
    And страница содержит навбар
    And страница содержит контент страницы About

  Scenario: Параметр ?tab в /use переключает табы
    When пользователь переходит на "/use?tab=keys"
    Then таб "API Keys" активен

    When пользователь переходит на "/use?tab=models"
    Then таб "Models" активен

    When пользователь переходит на "/use?tab=overview"
    Then таб "Overview" активен
