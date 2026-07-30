@ui
Feature: Join with PIN (v2)
  /join redeem flow with privacy notice and human errors.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Logged out empty form
    Given пользователь не аутентифицирован
    When пользователь переходит на "/join"
    Then элемент с data-testid="join-title" содержит текст "Join with a code"
    And элемент с data-testid="join-pin-input" видим
    And элемент с data-testid="btn-signin-connect" содержит текст "Sign in to connect"
    And элемент с data-testid="privacy-notice" видим

  Scenario: Prefill PIN from query
    Given пользователь не аутентифицирован
    When пользователь переходит на "/join?pin=7K4Q-9M2P"
    Then элемент с data-testid="join-pin-input" имеет значение "7K4Q-9M2P"

  Scenario: Logged in form shows Connect
    Given пользователь аутентифицирован как "member1"
    When пользователь переходит на "/join"
    Then элемент с data-testid="btn-connect" содержит текст "Connect"

  Scenario: Successful redeem shows machine and Use link
    Given пользователь аутентифицирован как "owner1"
    And у пользователя есть provider токен и онлайн машина
    And owner создал invite PIN
    Given пользователь аутентифицирован как "member1"
    When пользователь переходит на "/join"
    And пользователь вводит PIN в join-форму
    And пользователь кликает на data-testid="btn-connect"
    Then элемент с data-testid="join-success" видим
    And элемент с data-testid="btn-go-use" имеет href, содержащий "/use?setup="

  Scenario: Invalid PIN shows human error
    Given пользователь аутентифицирован как "member1"
    When пользователь переходит на "/join"
    And пользователь вводит в data-testid="join-pin-input" текст "ZZZZ-ZZZZ"
    And пользователь кликает на data-testid="btn-connect"
    Then элемент с data-testid="join-error" видим
    And элемент с data-testid="join-error-title" содержит текст "Invalid code"
    And страница не содержит текст "code: invalid_pin"
