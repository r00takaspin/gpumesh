@ui
Feature: Страницы ошибок
  Проверка отображения кастомных страниц ошибок: 404, 500, 503, 401.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Страница 404 — несуществующий URL
    When пользователь переходит на "/nonexistent-page-12345"
    Then статус ответа равен 404
    And элемент с data-testid="error-page" видим
    And элемент с data-testid="error-code" содержит "404"
    And заголовок или текст страницы содержит "Page not found"
    And элемент с data-testid="btn-go-home" видим
    And кнопка "Go home" ведёт на "/"

  Scenario: Кнопка «Go home» на странице 404 работает
    When пользователь переходит на "/nonexistent"
    And пользователь кликает на data-testid="btn-go-home"
    Then URL страницы равен "/"

  Scenario: Страница 500 — внутренняя ошибка сервера
    Given симулирована внутренняя ошибка сервера
    When пользователь переходит на страницу, вызывающую 500
    Then статус ответа равен 500
    And элемент с data-testid="error-page" видим
    And страница содержит текст "Something went wrong"
    And элемент с data-testid="btn-go-home" видим

  Scenario: Кнопка «Go home» на странице 500 работает
    Given симулирована внутренняя ошибка сервера
    When пользователь переходит на страницу, вызывающую 500
    And пользователь кликает на data-testid="btn-go-home"
    Then URL страницы равен "/"

  Scenario: Страница 503 — сервис недоступен
    Given координатор в состоянии обслуживания
    When пользователь переходит на "/"
    Then статус ответа равен 503
    And элемент с data-testid="error-page" видим
    And элемент с data-testid="btn-go-home" видим

  Scenario: Middleware 401 редиректит на /login
    Given пользователь не аутентифицирован
    When пользователь переходит на "/api/keys"
    Then статус ответа равен 401
    And пользователь не видит содержимое /api/keys

  Scenario: Навбар видим на страницах ошибок
    When пользователь переходит на "/nonexistent"
    Then статус ответа равен 404
    And элемент с data-testid="nav-home" видим
    And логотип с data-testid="logo" видим

  Scenario: Футер видим на страницах ошибок
    When пользователь переходит на "/nonexistent"
    Then статус ответа равен 404
    And элемент с data-testid="footer" видим

  Scenario: Статическая страница 503 без координатора
    Given координатор остановлен
    When пользователь переходит на "/"
    Then отображается страница ошибки или ошибка соединения
