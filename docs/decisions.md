# Decisions

Each decision with its state and reason. Reversals stay written: the reason matters more than
the decision.

## D1 — Scope: channel, not runtime
**Decided.** parley carries messages and persists conversations. It does not schedule turns,
wake sessions or fan out automatically.
Reason: the ecosystem already solved the channel (see `prior-art.md`), and CLIs do not accept
injection into a TUI; the only thing that justifies the project is the quality of the exchange.

## D2 — Bridge-owned sessions, not the human's TUI
**Decided.** Each participant has a session that can be continued by id, separate from the one
the human is watching. The human's session is untouched.

## D3 — Context: one persisted session per participant
**Decided.** Keyed by `(participant, directory)`. `parley session reset` starts from zero.
Verified: Claude recalls a fact between turns (`--session-id` / `--resume`) and OpenCode does
too (`--session`).

## D4 — Read-only by default
**Decided.** `write=true` must be explicit and the participant must allow it. A consulted agent
that needs a change returns a diff instead of editing the tree.

## D5 — No decision semantics in the core
**Decided (reverses the initial design).** The first design had `debate_open`, `debate_round`,
`debate_resolve`, ACCEPT/OBJECT votes and a `decisions` table. All removed; the core is
`open / send / read / close / list`.
Reason: it coupled the binary to a single usage pattern. Debate is a convention between
agents, not a transport state machine; it lives in the skill, and the record in
`docs/decisions/*.md` is written by the agent with its own tools.

## D6 — Synchronous delivery with a timeout
**Decided.** `send_message` waits for the reply. Timeout or error is returned as an error and
recorded; it is never interpreted as acceptance.

## D7 — No daemon
**Decided.** Each harness spawns its own MCP process; all of them write the same SQLite
database with WAL and `busy_timeout`. A daemon only earns its place if push into sleeping
sessions appears.

## D8 — One generic adapter, specs as data
**Decided.** A single `CLI` adapter interprets a `Spec` (bin, start, continue, prompt, session,
output, paths). Adding a harness does not touch Go.
Reason: two hard-coded adapters showed that 90% was identical; the remaining 10% (flags, output
format, where the session id comes from) fits in a struct.

## D9 — Name: `parley`
**Decided.** Short, pronounceable, means what it does: two sides talk to settle something. Skill,
binary and MCP prefix share the name.

## D10 — Cycle guards through the environment
**Decided.** `PARLEY_CHAIN` and `PARLEY_DEPTH` travel to the child process. A recipient already
in the chain is rejected before spending tokens.

## D11 — No provider in the binary
**Decided.** Configuration names a `harness`, never a provider. `model` is an opaque string
passed to the CLI; when absent, each harness uses its default. `config.Default()` ships no
participants: `setup` detects binaries on `PATH` and adds what is missing without overwriting.
Reason: the final test used two models from one provider, but the binary must not know that
provider exists.

## D12 — Distribution: releases plus self-update
**Decided.** Multi-platform binaries on GitHub Releases, `parley update` downloads and verifies
them. No npm wrapper: it is a Go binary, and npm would add Node plus a postinstall download for
no gain. `go install` covers Go users.

## D13 — New harnesses need a probe
**Open.** No new spec is added without `parley probe` showing the real output shape. The
`codex`, `cursor` and `pi` specs are deliberately marked unverified.
