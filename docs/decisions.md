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
**Decided.** No new spec is added without `parley probe` showing the real output shape.

Probed 2026-10-25 with the binaries installed at the time: codex 0.163.0 and cursor-agent
2026.10.19 matched their specs end to end (text, session id, resume) and are now verified.
Cursor needs the working directory trusted (`--trust` or an interactive run); parley does not
pass `--trust`, because trusting a tree is the user's call. pi 1.3.0 disagreed with the spec:
its JSON mode carries the answer in `message.content[]` (an array) and reports errors inside the
stream with exit 0. Rather than teach the adapter to walk arrays, the pi spec dropped to text
mode, where stderr and the exit code carry the failure. Supporting Pi's JSON mode is adapter
work, not a spec edit.

Probed 2026-10-27: hermes 0.21.4 (stream-json, session id in the `result` event, errors in
`result.error`) and omp 18.13.0. omp's JSON mode has pi's shape (`message.content[]`) and is
the only mode that reports a session id, so its spec is text mode with no continuity; parley
already sends the history in every prompt. Hermes has no read-only flag; `-t safe` is the only
toolset without writes, and it has no file reads, so the spec defaults to it and the README
shows the per-participant override.

## D14 — Trust model: configuration is code
**Decided.** parley runs the argv in each participant's spec (`bin`, `start`, `continue`) and
points it at that participant's `dir`. Whoever can write `~/.config/parley/config.json` can run
arbitrary commands as the user on the next `send`. That is the same trust boundary as a
Makefile, which is why the file is written `0600` and `setup` only adds participants that are
missing. It is also why read-only is a convenience, not a sandbox.

The channel assumes three things, and they are limitations, not bugs:

- **Prompt content is untrusted input for the recipient.** History, context and the peer's
  message are concatenated raw into the prompt. `read_only` limits what the recipient can
  change, not what it can be told to think.
- **`PARLEY_CHAIN` and `PARLEY_DEPTH` are cooperative.** They stop accidental recursion. A
  child that rewrites its own environment can bypass them.
- **`parley update` verifies sha256, not a signature.** `checksums.txt` ships with the release,
  so compromising the release is enough. If that is not acceptable, install through a package
  manager or pin a version.

## D15 — Concurrent turns to one participant are serialized
**Decided (issue #24).** Sessions are keyed by `(participant, directory)`. Two processes
sending to the same participant at once could each start a session, and the last writer won:
one of them lost continuity on the following turn. Now a turn takes a lease on that key
before it touches the session: one SQL statement grants it if it is free or its holder has
missed five heartbeats, the holder refreshes it while the turn runs, and a turn that finds it
held polls until it is free, its timeout runs out or it is cancelled. D7 holds: the lease
lives in the same SQLite file, there is no daemon and no queue process.

Waiting for the lease is inside the turn's timeout, so a send never takes longer than the
timeout the caller asked for. A turn that waited re-reads the conversation when it starts,
because the reply it was waiting for is part of what it must see. Measured with cursor-agent:
two concurrent sends took 13.9 s before and 20.8 s after, against 9.5 s for one: the second
now waits for the first instead of racing it. Harnesses with no session continuity are not
locked, since they have no session to lose.

## D16 — Read-only is the harness's flag, not a line in the prompt
**Decided, after a bug.** Every spec carried `read_only_args` and `model_args`, but argv was
built from `start`/`continue` alone: no turn ever got its read-only flag or its model. A
debate without `--write` rewrote files in the repo (issue #7), and `model` was silently
ignored (issue #6). `Render` now appends, in order, the permission flags, the model flags,
the participant's `args`, and the prompt. A spec without `read_only_args` is shown as
`read_only (not enforced …)` by `participants` rather than renamed: the flag keeps its name
because, with the fix, it does what it says for every built-in harness.

## D17 — A turn budget, not a token budget
**Decided.** `max_turns` caps a conversation; the check and the increment are one SQL
statement, so concurrent sends cannot both pass. A failed turn counts. Word limits and money
budgets were left out: truncating a reply corrupts the record, and only Claude and OpenCode
report a cost, so a money cap would be enforced for two harnesses and decorative for the rest.

`parley cancel` flags the running turns in the store; the owning process polls every second
and cancels its context, which kills the harness's process group. Polling the store instead of
signalling a pid works across processes without trusting a pid that may have been reused. It
cannot recall a request the provider already accepted, and the docs say so.
