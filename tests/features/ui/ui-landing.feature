@ui
Feature: Landing page (v2)
  Public home `/` with invite-first hero, demo PIN, how-it-works — no community stats.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Hero invite narrative
    When пользователь переходит на "/"
    Then заголовок страницы содержит "GPU Mesh"
    And элемент с data-testid="hero-title" содержит текст "Share your local models with friends"
    And элемент с data-testid="hero-lead" видим
    And элемент с data-testid="cta-create-invite" содержит текст "Create invite"
    And элемент с data-testid="cta-create-invite" имеет href "/share"
    And элемент с data-testid="cta-enter-code" содержит текст "Enter a code"
    And элемент с data-testid="cta-enter-code" имеет href "/join"

  Scenario: Example invite card is not a live code
    When пользователь переходит на "/"
    Then элемент с data-testid="example-invite" видим
    And элемент с data-testid="demo-pin" содержит текст "7K4Q-9M2P"
    And элемент с data-testid="cta-example-invite" имеет href "/share"

  Scenario: How it works steps
    When пользователь переходит на "/"
    Then элемент с data-testid="how-it-works" видим
    And элемент с data-testid="step-1" содержит текст "Run the agent"
    And элемент с data-testid="step-2" содержит текст "Share a PIN"
    And элемент с data-testid="step-3" содержит текст "They use your URL"

  Scenario: What you get section
    When пользователь переходит на "/"
    Then элемент с data-testid="what-you-get" содержит текст "Friends only"

  Scenario: No community proof stats
    When пользователь переходит на "/"
    Then элемент с data-testid="live-stats" не видим
    And элемент с data-testid="top-models" не видим
    And элемент с data-testid="stat-donors-online" не видим

  Scenario: Nav Use and Share
    When пользователь переходит на "/"
    And пользователь кликает на ссылку с data-testid="nav-use"
    Then URL страницы равен "/use"
    When пользователь переходит на "/"
    And пользователь кликает на ссылку с data-testid="nav-share"
    Then URL страницы равен "/share"

  Scenario: Logo goes home
    When пользователь переходит на "/about"
    And пользователь кликает на логотип с data-testid="logo"
    Then URL страницы равен "/"

  Scenario: Footer tagline
    When пользователь переходит на "/"
    Then элемент с data-testid="footer" видим
    And футер содержит ссылку на GitHub-репозиторий
    And элемент с data-testid="footer-tagline" содержит текст "Share local models with friends"
    And элемент с data-testid="footer-license" содержит текст "MIT"

  Scenario: Sign in for guests
    Given пользователь не аутентифицирован
    When пользователь переходит на "/"
    Then элемент с data-testid="btn-signin" видим
    And элемент с data-testid="btn-signin" содержит текст "Sign in with GitHub"

  Scenario: Username when logged in
    Given пользователь аутентифицирован как "testuser"
    When пользователь переходит на "/"
    Then элемент с data-testid="nav-username" содержит текст "testuser"
    And элемент с data-testid="btn-logout" видим
