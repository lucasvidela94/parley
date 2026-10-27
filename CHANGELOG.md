# Changelog

All notable changes to parley. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and versions follow [Semantic Versioning](https://semver.org/) (before 1.0, a minor bump may change
behaviour). Dates are UTC, as GitHub shows them. Issue numbers refer to
[lucasvidela94/parley](https://github.com/lucasvidela94/parley/issues).

## [Unreleased]

## [0.5.0] - 2026-10-27

### Added

- **Per-call model.** `send_message` and `parley send --model <id>` run one call with another
  model than the participant's (#18). The model is native to the participant's harness. A model
  different from the participant's gets its own harness session, because harnesses disagree on
  resuming one under another model (Claude keeps the old model, Codex warns); the conversation
  history still travels in the prompt, so no context is lost. `parley session reset <name>`
  clears those sessions too.
- **`models` MCP tool** with a `participant` argument: the same listing as `parley models`, for
  agents that need to pick a valid model (#19).
- **The transcript names the model that answered** when the participant has none configured
  (#20). New spec field `model_keys_path` reads it from the harness output. Only Claude reports
  it (`modelUsage`); Cursor, Codex and OpenCode do not, so their transcripts stay blank instead of
  guessing. A configured `model` always wins.
- **`read` shows a turn in progress**: `turn in progress: <participant>, since <time>` while a
  peer is answering (#21). Turns of a dead process are not listed.
- **Spec field `error_hints`**: advice appended to the error as `(hint: …)` when a failed command's
  stderr contains a given fragment (#17). Cursor uses it for `Workspace Trust Required`.

### Changed

- **`participants` says what writing needs** (#23). A `read_only` participant shows
  `(edits also need permission: "write" in the config)` and a `write` one shows
  `(a call must also pass write=true)`. Anything that matches the exact strings `read_only` or
  `write` in that output needs to match the prefix instead.
- **`send` reports an ignored `write=true`** (#23): when the participant is `read_only` in the config
  the result carries a notice (`(notice: …)` over MCP, `[notice: …]` on stderr, `notice` in
  `--json`). Before, the flag was dropped silently.
- **The peer prompt asks to read before claiming** (#22): read a relevant file before making a claim
  about it, and end with the files that were not read. Its effect on real consultations has not
  been measured.

### Known limitations

- Cursor cannot report workspace trust without running a model, so the problem is reported when the
  first turn fails, with the fix in the hint, not before it.
- `pi` and `omp` (text mode) accept an empty reply as an answer, unlike the JSON harnesses (#35).
- An override of the model on a harness that never reports its model is recorded as asked, not as
  verified.

## [0.4.0] - 2026-10-27

### Added

- **`parley models <participant>`** and the spec field `models_args`: run the harness's own model
  listing and print it untouched; parley never interprets model ids (#16). Built in for OpenCode
  (`models`), Cursor (`--list-models`) and pi (`--list-models`). The README now shows several
  Cursor models as separate participants.

### Changed

- **A running MCP server picks up an edited `config.json`** on the next call, so adding a participant
  no longer needs a reconnect (#13). A file that no longer parses is reported and the previous
  config stays in use. Identity and the store path are still read once at startup.
- **`read` counts failed turns**: `turns: 1/3 (1 failed)` (#15). A failed turn still counts toward
  `max_turns`: it may have spent credits, and refunding it would leave a retry loop on a broken
  harness unbounded.
- **Errors in the transcript keep up to 1400 characters** (they were cut at 300), so the remedy a
  harness prints at the end is no longer lost (#14).
- The Cursor spec notes explain the trust requirement: run `cursor-agent` once in the directory, or
  add `"args": ["--trust"]` to the participant (#14).

## [0.3.2] - 2026-10-27

### Added

- **Hermes** (`hermes`) as a built-in harness, verified against 0.21.4 with real turns: stream-json
  output, session continuity via `--resume`, errors from the `result` event. Read-only uses
  `-t safe`, the only toolset with no file writes and no terminal; it cannot read files either.
- **oh-my-pi** (`omp`) as a built-in harness, verified against 18.13.0: text mode, read-only via
  `--no-tools`. No session continuity: only its JSON mode reports a session id, and that mode puts
  the answer in an array the adapter cannot read. parley sends the history in every prompt anyway.
- `parley setup` detects both and shows how to register the MCP server in them by hand.

### Changed

- Read-only is a default, not a cage: every participant can override its harness's read-only flags in
  `config.json` (`"spec": {"read_only_args": [...]}`). `"read_only_args": []` removes the
  restriction, and `parley participants` then shows `not enforced`.

## [0.3.1] - 2026-10-27

### Fixed

- `parley update` replaced the binary but left the installed skill (`~/.claude`,
  `~/.config/opencode`, `~/.agents`) on the old text. It now runs the new binary with
  `parley setup --skills`, which installs only the skill and leaves config and MCP registrations
  alone.
- `parley setup --write` reported only failures when installing the skill; it now lists every harness
  it installed it for.

## [0.3.0] - 2026-10-27

### Fixed

- **Read-only is now enforced by the harness** (#7). Specs carried `read_only_args` and `model_args`,
  but they were never added to argv: a turn without `--write` ran with full permissions. Without
  `--write` the harness now starts in its read-only mode (`claude --permission-mode plan`,
  `opencode --agent plan`, `codex -c sandbox_mode="read-only"`, `cursor-agent --mode plan`,
  `pi --no-tools`). A custom spec without such flags shows as `read_only (not enforced …)` in
  `parley participants`.
- **`model` reaches the harness** (#6). Two participants on the same harness with different models are
  two voices in one conversation. New `args` field for extra argv.
- **`parley close <id> --note …` dropped the note**: flags after the id were not parsed. `close` now
  prints `closed <id>` and the note instead of the transcript (#9).

### Added

- `open --max-turns N` / `max_turns`: the send past the limit is refused (#8).
- `parley cancel <id>`: stops the turn running in a conversation and kills the harness with its child
  processes (#8). Ctrl-C, SIGTERM and SIGHUP do the same.
- `read --tail N`, `--offset N`, `--limit N` (CLI and MCP), by whole messages (#10).
- The transcript header lists every participant with its model, the turns spent and the cost when the
  harness reports it (#12).
- Docs: every config field, read-only per harness, concurrency, `max_depth` (#11).

### Known limitations

- Cancelling stops the local process, not the provider: a request already sent may finish and be
  billed. There are no token counts; only Claude reports cost.
- Not run against the real harnesses: the read-only flags were checked with `--help` and with a stub
  harness, not in a real turn.
- The MCP server handles one call at a time: an agent cannot cancel its own `send_message`; use
  `parley cancel` from a shell.
- Conversations created before this version show `turns: 0`. The store migrates itself on first
  open; no action needed.

## [0.2.1] - 2026-10-25

### Fixed

- `parley update` sends `GITHUB_TOKEN` or `GH_TOKEN` as a bearer token and turns a 403 or 429 into a
  message that names the fix. Unauthenticated calls are rate-limited per IP, which a shared address
  hits fast.

## [0.2.0] - 2026-10-25

### Added

- CI runs `golangci-lint` (strict config) and `govulncheck` on every push; Dependabot and a security
  policy (`SECURITY.md`).

### Security

- Archive extraction in `parley update` is bounded.
- Workflows run with least-privilege permissions; only the release job can write.

## [0.1.2] - 2026-10-25

### Fixed

- English strings in the participants list and the probe prompt.

## [0.1.1] - 2026-10-25

### Fixed

- Remaining non-English user-facing strings.

## [0.1.0] - 2026-10-25

First release.

### Added

- Declarative CLI adapter with built-in specs for Claude Code, OpenCode, Codex, Cursor and pi.
- Conversations with sessions that survive turns, stored in SQLite.
- Participants configured by harness, detected on `PATH` by `parley setup`.
- MCP server over stdio with six tools, and a CLI: `send`, `open`, `read`, `close`, `list`,
  `participants`, `session reset`, `harnesses`, `setup`, `probe`, `update`.
- A usage skill for agent-to-agent workflows, installed by `parley setup`.
- Self-update from GitHub releases with sha256 verification.

[Unreleased]: https://github.com/lucasvidela94/parley/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/lucasvidela94/parley/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/lucasvidela94/parley/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/lucasvidela94/parley/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/lucasvidela94/parley/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/lucasvidela94/parley/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/lucasvidela94/parley/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/lucasvidela94/parley/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/lucasvidela94/parley/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/lucasvidela94/parley/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/lucasvidela94/parley/commits/v0.1.0
