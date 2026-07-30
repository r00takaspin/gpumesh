@ui
Feature: About page (v2)
  Invite-first about + FAQ.

  Background:
    Given пользователь открывает браузер
    And координатор запущен и доступен

  Scenario: Hero copy
    When пользователь переходит на "/about"
    Then элемент с data-testid="about-title" содержит текст "About GPU Mesh"
    And элемент с data-testid="about-lead" содержит текст "PIN"

  Scenario: Owner and Member cards
    When пользователь переходит на "/about"
    Then элемент с data-testid="about-owner" содержит текст "Owner"
    And элемент с data-testid="about-member" содержит текст "Member"

  Scenario: Privacy banner
    When пользователь переходит на "/about"
    Then элемент с data-testid="about-privacy" содержит текст "prompts go through the GPU Mesh server"

  Scenario: FAQ present
    When пользователь переходит на "/about"
    Then элемент с data-testid="about-faq" видим
    And элемент с data-testid="about-faq" содержит текст "Tailscale"

  Scenario: No community garden copy
    When пользователь переходит на "/about"
    Then страница не содержит текст "community garden"
    And страница не содержит текст "Powered by community"
