@ui
Feature: Share owner dashboard (v2)
  Progressive /share: no token → waiting → ready → create invite PIN modal.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Logged-out hero
    Given пользователь не аутентифицирован
    When пользователь переходит на "/share"
    Then элемент с data-testid="share-title" содержит текст "Share your local models"
    And элемент с data-testid="btn-signin-share" содержит текст "Sign in with GitHub"

  Scenario: Sign-in link targets /share redirect
    Given пользователь не аутентифицирован
    When пользователь переходит на "/share"
    Then элемент с data-testid="btn-signin-share" имеет href, содержащий "redirect=/share"

  Scenario: No provider token state
    Given пользователь аутентифицирован как "owner-notoken"
    And у пользователя нет provider токена
    When пользователь переходит на "/share"
    Then элемент с data-testid="share-panel" видим
    And элемент с data-testid="btn-generate-token" видим
    And элемент с data-testid="btn-generate-token" содержит текст "Generate provider token"

  Scenario: Generate provider token shows modal
    Given пользователь аутентифицирован как "owner-gentoken"
    And у пользователя нет provider токена
    When пользователь переходит на "/share"
    And пользователь кликает на data-testid="btn-generate-token"
    Then элемент с data-testid="modal-provider-token" видим
    And элемент с data-testid="provider-token-value" видим

  Scenario: Waiting for provider after token
    Given пользователь аутентифицирован как "owner-waiting"
    And у пользователя есть provider токен
    And у пользователя нет машин
    When пользователь переходит на "/share"
    Then элемент с data-testid="waiting-provider" содержит текст "Waiting for provider"
    And элемент с data-testid="btn-create-invite" неактивен
    And элемент с data-testid="provider-os-tabs" видим
    And элемент с data-testid="run-command" содержит текст "YOUR_PROVIDER_TOKEN"
    And элемент с data-testid="btn-revoke-provider-key" видим

  Scenario: Waiting setup has Windows install
    Given пользователь аутентифицирован как "owner-waiting-win"
    And у пользователя есть provider токен
    And у пользователя нет машин
    When пользователь переходит на "/share"
    And пользователь кликает на tab Windows в provider setup
    Then элемент с data-testid="provider-os-windows" видим
    And элемент с data-testid="install-windows" содержит текст "Invoke-WebRequest"
    And элемент с data-testid="run-command-windows" содержит текст "YOUR_PROVIDER_TOKEN"

  Scenario: Online ready can create invite
    Given пользователь аутентифицирован как "owner-online"
    And у пользователя есть provider токен и онлайн машина
    When пользователь переходит на "/share"
    Then элемент с data-testid="btn-create-invite" видим
    And элемент с data-testid="machine-strip" видим
    And элемент с data-testid="machine-status" содержит текст "Online"

  Scenario: Create invite shows PIN modal once
    Given пользователь аутентифицирован как "owner-invite"
    And у пользователя есть provider токен и онлайн машина
    When пользователь переходит на "/share"
    And пользователь кликает на data-testid="btn-create-invite"
    Then элемент с data-testid="modal-invite-pin" видим
    And элемент с data-testid="invite-pin" видим
    And элемент с data-testid="invite-join-link" видим
    And элемент с data-testid="btn-copy-pin" видим
    And элемент с data-testid="btn-copy-join-link" видим

  Scenario: Offline warning still allows invite
    Given пользователь аутентифицирован как "owner-offline"
    And у пользователя есть provider токен и офлайн машина
    When пользователь переходит на "/share"
    Then элемент с data-testid="offline-warning" видим
    And элемент с data-testid="btn-create-invite" видим

  Scenario: Active nav on /share
    When пользователь переходит на "/share"
    Then ссылка с data-testid="nav-share" имеет класс "on"

  Scenario: Setup has regenerate and revoke
    Given пользователь аутентифицирован как "owner-advanced"
    And у пользователя есть provider токен и онлайн машина
    When пользователь переходит на "/share"
    And пользователь открывает details data-testid="setup-collapse"
    Then элемент с data-testid="regen-warning" содержит текст "new machine URL"
    And элемент с data-testid="btn-regenerate-key" видим
    And элемент с data-testid="btn-revoke-provider-key" видим
    And элемент с data-testid="advanced-collapse" не видим
    And элемент с data-testid="run-command" содержит текст "YOUR_PROVIDER_TOKEN"
