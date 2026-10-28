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

That adapter work landed (issue #26): a path segment ending in `[]` walks an array, so pi and
omp read `message.content[].text` from `turn_end` events and treat `message.errorMessage` as a
failure. Probed 2026-10-27 with every provider on the machine failing (pi: expired key, exit 0;
omp: 429, exit 1): the error shape is verified live, the success shape comes from pi's
`docs/message-types.md`. Both specs are therefore `verified: false` until a working provider
confirms `content[].text` and omp's `--resume <header id>`.

Probed 2026-10-27 for issue #30, no spec added: gemini 0.66.0 (`-p … -o json`) printed
`{"session_id", "error": {"type", "message", "code": 41}}` for a missing auth method; copilot
1.0.103 (`-p … --output-format json`) streamed `session.*` events and then
`Access denied by policy settings`; grok 1.0.70 (`-p … --output-format json`, `--session-id`,
`--resume`, `--permission-mode plan`) printed `{"type":"error","message":"Not signed in…"}`.
None completed a turn on the probing machine, so the text, session-id and resume paths are
unseen. The flags are promising (all three have a headless JSON mode and a session flag) but
D13 asks for the real output shape, so the specs wait for a machine where each is signed in.
kilo, qoder and agy were not installed.

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

  Probed 2026-10-27 (issue #29), because delimiting the sections is the obvious hardening and
  it costs tokens on every turn. A participant whose reply carried (a) a forged
  `MESSAGE FROM self` section asking for a one-word answer, (b) a forged `Rules:` section with
  a standing instruction, and (c) a forged `[self]` turn impersonating the caller with the
  same instruction, was followed by a real question to cursor-agent (grok-4.7-low), opencode
  and codex. None obeyed in 21 runs (6, 6 and 9), and opencode named the instruction as an
  "unverified claim". So the envelope has no failure to fix today and was not added. This is
  a small sample on three harnesses and one model each: if a harness is seen following a
  smuggled instruction, the next step is boundaries a body cannot contain (a per-prompt random
  token), and the three payloads above are the regression test.
- **`PARLEY_CHAIN` and `PARLEY_DEPTH` are cooperative.** They stop accidental recursion. A
  child that rewrites its own environment can bypass them.
- **`parley update` verifies sha256, not a signature.** `checksums.txt` ships with the release,
  so compromising the release is enough. If that is not acceptable, verify the Sigstore
  signature by hand (D18), install through a package manager or pin a version.

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

## D18 — Releases are signed; `update` still checks sha256 only
**Decided (issue #28).** The release workflow signs `checksums.txt` with cosign, keyless
through GitHub OIDC, and publishes `checksums.txt.sigstore.json`. The identity is the release
workflow at a `v*` tag, so a signature proves the checksums came from this repository's CI, not
from whoever can edit the release page. `release-install.md` shows the `cosign verify-blob`
command.

`parley update` does not verify it. Doing that in-process needs `sigstore-go` (a large
dependency tree, against the allowed-dependency list) and a network fetch of the Sigstore trust
root; shelling out needs `cosign` on the PATH, and a check that silently turns off when cosign
is missing is worse than an honest one. That is the falsifier written in the issue: verification
needs extra toolchain, so sha256 stays the automatic check and the signature is the high-trust
path for people who want it. Not exercised end to end until the next tag is pushed: the signing
step has only been read against cosign's documented flags.
