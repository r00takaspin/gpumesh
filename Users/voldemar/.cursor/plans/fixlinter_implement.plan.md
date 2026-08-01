---
name: FixLinter
overview: Address all golangci-lint errors in main.go and related utilities. Ensure no unused vars, duplicate flags, and correct env handling.

todos:
  - id: "1"
    content: Remove duplicate errcheck flag definition in cmd/provider/main.go (lines 3 and 6). Keep only one instance and adjust documentation accordingly.
    status: in_progress
  - id: "2"
    content: Remove duplicate errcheck flag definition in flag variable definition block (lines 3 and 6). Keep a single definition.
    status: in_progress
  - id: "3"
    content: Remove any unused flag variables: confirm that ollamaFlag, maxConcFlag, etc. are used. If any remain unused, delete their definitions.
    status: in_progress
  - id: "4"
    content: Update applyDefaults logic: ensure it uses cfg.MaxConcurrent correctly; if default is 1, set accordingly and remove any stale logic.
    status: in_progress
  - id: "5"
    content: Update applyEnv to use correct parameter names; ensure it only sets values if env vars present and does not override explicitly set flags.
    status: in_progress
  - id: "6"
    content: Add comments to explain why errcheck is removed and how flag precedence works.
    status: in_progress

tasks:
  - "Implement todo 1"
  - "Implement todo 2"
  - "Implement todo 3"
  - "Implement todo 4"
  - "Implement todo 5"
  - "Implement todo 6"

action: "Apply changes to cmd/provider/main.go and related files, ensure correct flag definitions and env handling, remove duplicates.
---
