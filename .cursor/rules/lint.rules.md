# Linter Rules

## 1. Ensure `golangci.yml` is present

- The repository **must** contain a `.golangci.yml` file that defines the linter configuration.

## 2. Run linter in CI

- The CI workflow should include a job that executes `golangci-lint run ./...`.

## 3. No lint errors in main branch

- The main branch should not contain any linting errors as per `golangci.yml` configuration.
