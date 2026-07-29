@ui
Feature: Вход через GitHub
  Страница `/login`, OAuth-редирект на GitHub, обработка callback
  и редирект на целевые страницы после аутентификации.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Отображение страницы входа
    When пользователь переходит на "/login"
    Then заголовок страницы содержит "Login"
    And элемент с data-testid="login-card" видим
    And отображается ASCII-логотип "GPU MESH"
    And отображается текст "One click. No password. No email."

  Scenario: Кнопка входа ведёт на GitHub OAuth
    When пользователь переходит на "/login"
    Then элемент с data-testid="btn-github-login" видим
    And кнопка содержит текст "Sign in with GitHub"
    And href кнопки содержит "/auth/github"

  Scenario: Редирект на /use сохраняется в параметре redirect
    When пользователь переходит на "/login"
    And пользователь кликает на data-testid="btn-github-login"
    Then URL содержит "/auth/github"
    And URL содержит параметр "redirect" со значением "/use"

  Scenario: Кнопка «Sign in with GitHub» на лендинге с редиректом
    Given пользователь не аутентифицирован
    When пользователь переходит на "/"
    And пользователь кликает на data-testid="btn-signin"
    Then URL содержит "/auth/github"
    And URL содержит параметр "redirect" со значением "/use"

  Scenario: Кнопка входа на странице /share с редиректом на /share
    Given пользователь не аутентифицирован
    When пользователь переходит на "/share"
    And пользователь кликает на data-testid="btn-signin"
    Then URL содержит "/auth/github"
    And URL содержит параметр "redirect" со значением "/share"

  Scenario: OAuth callback — успешная аутентификация
    Given GitHub OAuth настроен и доступен
    When пользователь проходит аутентификацию через GitHub
    Then пользователь перенаправляется на "/use"
    And сессионная cookie установлена
    And элемент с data-testid="nav-username" отображает GitHub-логин

  Scenario: OAuth callback с параметром redirect
    Given GitHub OAuth настроен
    And пользователь авторизуется с параметром "redirect=/models"
    When пользователь проходит аутентификацию через GitHub
    Then пользователь перенаправляется на "/models"

  Scenario: OAuth не настроен — информационное сообщение
    Given переменная GITHUB_CLIENT_ID не задана
    When пользователь переходит на "/login"
    Then кнопка входа через GitHub не видима
    And отображается текст "OAuth not configured"
    And отображается подсказка "Set GITHUB_CLIENT_ID"

  Scenario: Отображение информации «Why GitHub?»
    When пользователь переходит на "/login"
    Then элемент с data-testid="login-why-github" видим
    And секция объясняет почему используется GitHub для входа

  Scenario: Выход из системы
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/"
    And пользователь кликает на data-testid="btn-logout"
    Then пользователь перенаправляется на "/"
    And сессионная cookie удалена
    And элемент с data-testid="btn-signin" видим
    And элемент с data-testid="nav-username" не видим

  Scenario: Middleware редиректит неавторизованных на /login
    Given пользователь не аутентифицирован
    When пользователь переходит на "/api/keys"
    Then статус ответа равен 401
    And пользователь видит страницу входа или JSON-ошибку
