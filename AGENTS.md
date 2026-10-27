# AGENTS.md

Guidance for AI agents working in this repo. Keep it short: the linters and tests
already encode most of what matters.

## What this is

`parley` is a Go CLI + MCP server that carries messages between coding-agent harnesses
(Claude Code, OpenCode, Codex, Pi, Cursor). It is a channel: it does not decide, vote,
schedule turns or wake sessions.

## Commands

```bash
make check        # gofmt + vet + golangci-lint + tests (run before saying "done")
make race         # go test -race ./...
make vuln         # govulncheck ./...
make hooks        # install .githooks as the repo's core.hooksPath
make build        # ./parley
```

## Map

```
main.go               CLI: flags, usage, setup/probe/update (no business logic)
internal/mcp          JSON-RPC over stdio; tool schemas and dispatch
internal/core         conversations, prompt assembly, session selection, guards
internal/harness      one declarative adapter (Spec) + built-in harness specs
internal/config       config file, harness detection, spec resolution
internal/store        SQLite (WAL) for sessions, conversations, messages
internal/update       GitHub release download, sha256, extraction
internal/skills       embedded skill installed by `parley setup`
```

The seam is `harness.Harness` (one implementation, `CLI`, driven by a `Spec`). A new
harness is a `Spec` in `internal/harness/spec.go` or inline config — not a new package.

## Invariants

1. The core knows nothing about debates, votes or decisions. Conversations only:
   `open / send / read / close / list`.
2. Config names a `harness`, never a provider. `model` is opaque; omit it to use the
   harness default.
3. Read-only by default. A consulted agent edits files only with `write=true` and
   `permission: "write"`.
4. A timeout or an error is never agreement. Return the error; record it as an error
   message.
5. One participant never talks to itself in a chain: `PARLEY_CHAIN` / `PARLEY_DEPTH`
   guard recursion.
6. The repo is English-only: code, comments, strings, docs, commit messages.

## Prohibitions

- No `utils`, `common`, `helpers` packages. Put code where its caller is.
- No interface without two real implementations. `Harness` qualifies; a new one does not
  until it has a second adapter.
- No `panic`, no `log.Fatal`, no `os.Exit` outside `main.main`.
- No `init()`, no mutable globals, no package-level state.
- No printing from `internal/`: return strings and let `main`/`mcp` print them.
- No new dependency without a reason and a `depguard` update. Allowed today: stdlib and
  `modernc.org/sqlite`.
- No `time.Sleep` in tests, no network, no real harness binaries in unit tests.
- No mocking library: a fake in the test file is enough (see `internal/core/engine_test.go`).

## Errors and tests

- Wrap with `%w` at package boundaries; keep sentinel errors (`ErrLoop`, `ErrDepth`) for
  control flow.
- Tests are table-driven, use `t.TempDir()` and `t.Chdir()`, and assert behaviour through
  the public interface.
- Tests that touch env or cwd must not call `t.Parallel()`.
- Every bug fix gets a test that fails without the fix.

## Definition of done

`make check` green, plus `make race` when touching concurrency, store or core. If the
change adds an MCP tool or a CLI command, follow `skills/parley-dev/SKILL.md` and update
the README usage section.

A change a user can see (a flag, a tool, output text, a spec field, a fixed bug) adds an entry
under `## [Unreleased]` in `CHANGELOG.md`, with the issue number. To release, rename that section
to the version and date and add its compare link; the release workflow takes the notes from that
section and fails without them.

## Review

Read the whole diff before calling it done, even when the gates are green. Green checks
prove the properties that were asserted, not that the change is right. For anything
non-trivial, ask a second agent to review the diff without the context of how it was
written.
