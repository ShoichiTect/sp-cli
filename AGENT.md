# AGENT.md

## Scope

- `README.md` is the source of truth for user-facing install, usage, and development commands.
- This file only records repo-specific rules for agents and maintainers.

## Path Rules

- Do not use machine-specific absolute paths in docs, scripts, or notes.
- Always reference repo files with relative paths such as `./main.go` and `./pkg/cli/cli.go`.

## Binary Rules

- The installed binary name is `sp-cli`.
- Do not keep a repo-local built binary like `./sp-cli` checked in or lying around after ad hoc builds.
- If a one-off `go build .` is used, remove the generated `./sp-cli` afterward.
