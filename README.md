# parley

**Talk to other coding agents over one channel.** parley is a single Go binary that lets
Claude Code, OpenCode, Codex, Cursor and Pi message each other — each one keeping its own
model, context, tools and permissions.

It is a channel, not an orchestrator: it carries messages and stores the conversation. It
doesn't decide, vote, wake agents or schedule turns. What the agents conclude, they write
themselves.

[![ci](https://github.com/lucasvidela94/parley/actions/workflows/ci.yml/badge.svg)](https://github.com/lucasvidela94/parley/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/lucasvidela94/parley)](https://github.com/lucasvidela94/parley/releases)
[![license](https://img.shields.io/github/license/lucasvidela94/parley)](LICENSE)

```
OpenCode (model X)                        Claude Code (model Y)
  running turn                              own session, no TUI
        │                                            ▲
        └──────── send_message(to="claude") ──────────┘
                    parley (MCP stdio)
          sessions · conversations · SQLite
```

## Why

Every harness is good at something different: one has the repo loaded, another has a model
that disagrees well, a third has tools the others lack. Copying and pasting between terminals
loses the reasoning and the evidence.

parley gives them one address book. An agent asks another a question inside its own turn and
gets the answer back — with the other agent's context intact, because sessions persist by id.

## Verified

Tested live on 2026-10-25 with `opencode v2.1.4` and `claude 2.1.305`:

- A turn stores a fact, the next turn recalls it (both harnesses, independent sessions).
- The MCP server answers `send_message` against a real agent.
- A three-turn conversation between two OpenCode sessions running different models, with an
  objection and a concession, closed with a note and a persisted transcript.
- `parley update` against a real release: version check, sha256 verification, binary replaced.

Checked on 2026-10-25 against the binaries installed here:

- Codex 0.163.0, end to end: `thread.started` carries `thread_id`, `item.completed` carries the
  text, and `exec resume` continues the same thread.
- The read-only flags of every spec exist in its CLI: `pi --no-tools`,
  `cursor-agent --mode plan`. Codex uses `-c sandbox_mode="read-only"`, because
  `codex exec resume` has no `--sandbox` flag (checked against `codex exec resume --help`).
- Cursor 2026.10.19, end to end: `result` carries the text, `session_id` the session, `--resume`
  keeps it, and `--mode plan` is accepted. It refuses to start in an untrusted directory.
- Pi answered `401` inside its JSON stream with exit 0, which the adapter cannot read; its spec
  now uses text mode, where the error goes to stderr with exit 1.

## Install

Binary from [releases](https://github.com/lucasvidela94/parley/releases):

```bash
curl -fsSL https://github.com/lucasvidela94/parley/releases/latest/download/parley_linux_amd64.tar.gz \
  | tar -xz -C ~/.local/bin parley
parley setup --write
```

With Go:

```bash
go install github.com/lucasvidela94/parley@latest
parley setup --write
```

`setup` detects the harness binaries on your `PATH` and writes:

- `claude mcp add --scope user parley -e PARLEY_IDENTITY=claude -- <parley> mcp`
- `mcp.servers.parley` in `~/.config/opencode/opencode.json` (with a `.bak`)
- the `parley` skill into `~/.config/opencode/skills`, `~/.claude/skills` and `~/.agents/skills`

It only adds what is missing; it never overwrites an existing participant.

**No npm wrapper.** parley is a Go binary, not a JS package. npm would add a Node dependency
and a postinstall download for no gain; `go install` and the release binaries cover both
audiences. An npm shim can be added later if `npx parley` proves useful.

## Use

From any agent with the MCP server registered:

```
participants()                                    # who is available
send_message(to="claude", message="...")          # ephemeral: one turn
open_conversation(topic="...", context="...", max_turns=6)   # multi-turn, optional cap
send_message(to="claude", conversation_id="cnv_…", message="...")
read_conversation(id="cnv_…", tail=4)              # or offset/limit; whole messages
close_conversation(id="cnv_…", note="conclusion")
```

Or ask in plain language — the bundled skill knows the workflow:

> use the parley skill with claude, review this implementation, and let both of you decide
> what is best

From the shell:

```bash
parley send --to claude "does this break the API contract?"
parley open --topic "cache choice" --context "repo at /src/x" --max-turns 6
parley send --to opencode --in cnv_123 "and with a restart?"
parley read cnv_123                    # whole transcript
parley read cnv_123 --tail 4           # last 4 messages
parley read cnv_123 --offset 10 --limit 10
parley read cnv_123 --json             # messages as objects: author, model, kind, body, created_at
parley cancel cnv_123                  # stop the turn running in that conversation
parley close cnv_123 --note "in-memory plus a pending benchmark"
parley list
parley models cursor-opus              # what that participant's harness accepts as `model`
parley send --to cursor --model grok-4.7-high "..."   # this call only, instead of the participant's model
```

Without `--in` / `conversation_id` the conversation is ephemeral: created, answered, closed.

`read` pages by whole messages, never by lines, so a turn is never cut in half; the header
says which window you are looking at (`messages 11–20 of 57`), how many turns were spent and,
when the harness reports it, the cost. `close` prints the id, the status and the note — the
transcript is one `read` away.

### Who said what

Every message is recorded under its author: the recipient's participant name for replies,
and the local identity for what you send. The local identity is `PARLEY_IDENTITY` (which
`parley setup` sets per harness), else `identity` in the config, else `self`. Name it so the
record reads well: `"identity": "momo"`. A reply also records the participant's configured
`model`, so `read` shows `### kimi · opencode-go/kimi-k3` and a header listing every voice in
the order it first spoke.

### Cost and cancelling

- `--max-turns N` (`max_turns`) caps a conversation: the send after the Nth is refused with
  an error and nothing is recorded. A failed turn counts — it may have spent credits too —
  and `read` says so: `turns: 1/3 (1 failed)`.
- `parley cancel <id>` asks every turn running in that conversation to stop. The process that
  owns the turn notices within a second and kills the harness with its child processes; the
  turn is recorded as an error.
- Ctrl-C or `SIGTERM` on `parley send` (or on the MCP server) kills the harness too.
  `SIGKILL` cannot be caught: a harness started by a killed parley keeps running.
- **Cancelling stops the local process, not the provider.** A request the provider already
  received may still finish and be billed. Cost is reported by Claude and OpenCode only, and
  `read` sums it. Tokens are shown per message (`12024 in / 866 out`) for Cursor, Codex and
  OpenCode, exactly as each harness counts them: caches are counted differently, so they are
  never totalled across participants, and Claude shows none because its `input_tokens` leaves
  the cache out. A harness that reports nothing shows nothing; parley never estimates. Word
  limits written in a prompt are a request, not a constraint.

## Update

```bash
parley update --check   # what would change
parley update           # download, verify sha256, replace the binary, refresh the skill
parley setup --skills   # refresh only the skill (update runs this with the new binary)
```

Updates come from GitHub releases. Builds made with `go install` at a tagged version can
self-update; `dev` builds refuse unless you pass `--force`.

## Supported harnesses

| Harness | Binary | Status |
|---|---|---|
| Claude Code | `claude` | verified |
| OpenCode | `opencode` | verified |
| Codex | `codex` | verified |
| Cursor | `cursor-agent` | verified (the working directory must be trusted: run `cursor-agent` there once, or set the participant's `args` to `["--trust"]`) |
| Pi | `pi` | spec included; text mode only, turn unverified (expired key) |
| Hermes | `hermes` | verified (0.21.4); read-only cannot read files, see below |
| oh-my-pi | `omp` | verified (18.13.0); text mode, no session continuity |

`parley harnesses` prints this list with the exact flags each spec uses.

## Adding a harness

One declarative adapter, no Go required. A harness is a spec:

| field | meaning |
|---|---|
| `bin` | executable |
| `start` / `continue` | argv for the first and later turns (`{session}`, `{model}`, `{dir}`, `{prompt}`) |
| `prompt` | `stdin` or `arg` |
| `session` | `arg` (parley mints the id), `output:<path>` (read it from the reply) or `none` |
| `output` | `text`, `json` or `jsonl` |
| `text_path` | dotted path to the answer text (`result`, `part.text`, …) |
| `text_filter_path` / `text_filter_equals` | for JSONL: which lines count |
| `error_path` / `error_event_type` / `error_bool_path` | how a failure shows up |
| `cost_path` / `input_tokens_path` / `output_tokens_path` | where the turn's cost and token counts are. In JSONL every line that has the field counts and the values add up (OpenCode reports one per step) |
| `error_hints` | map from a fragment of the CLI's stderr to advice; when a failed command printed that fragment, the advice is appended to the error as `(hint: …)`. Cursor uses it for the workspace-trust failure |
| `model_keys_path` | JSON output only: dotted path to an object whose keys are the models the turn used (claude's `modelUsage`). Recorded as the message's model when the participant sets none; a configured `model` wins. Cursor, codex and opencode do not report one, so their transcripts stay blank |
| `models_args` | argv that makes the CLI print its models; `parley models <name>` runs it and shows the output as is (built in for opencode, cursor and pi) |
| `read_only_args` / `write_args` | appended when the turn is read-only / may write |
| `model_args` | appended when the participant sets `model` (`{model}` is substituted) |

argv is built in this order: `start` or `continue`, the permission flags, the model flags,
the participant's `args`, and the prompt last when `prompt` is `arg`.

```json
{
  "participants": {
    "my-agent": {
      "spec": {
        "bin": "my-cli",
        "start": ["exec", "--json"],
        "continue": ["exec", "resume", "{session}", "--json"],
        "prompt": "stdin",
        "session": "output:session_id",
        "output": "jsonl",
        "text_path": "message.text",
        "read_only_args": ["--sandbox", "read-only"]
      }
    }
  }
}
```

`parley probe -- <command> [args]` runs the CLI with a test prompt and prints the JSON shapes
it returns, which is what you need to fill `text_path` and `session`.

## Configuration

`~/.config/parley/config.json` (or `$PARLEY_CONFIG`):

```json
{
  "identity": "momo",
  "default_timeout_s": 300,
  "max_depth": 3,
  "participants": {
    "reviewer": { "harness": "claude", "permission": "read_only" },
    "builder":  { "harness": "opencode", "permission": "write", "dir": "/src/x-worktree" },
    "deepseek": { "harness": "opencode", "model": "deepseek/deepseek-v4.1" },
    "kimi":     { "harness": "opencode", "model": "opencode-go/kimi-k3", "args": ["--variant", "high"] }
  }
}
```

| participant field | meaning |
|---|---|
| `harness` | the CLI family: `claude`, `opencode`, `codex`, `cursor`, `pi` |
| `model` | optional, native to that harness; parley passes it through untouched. Omit it for the harness default |
| `args` | optional extra argv, appended after the spec's flags |
| `dir` | working directory for this participant's turns (default: where parley runs) |
| `permission` | `read_only` (default) or `write`. A peer edits only when this is `write` **and** the call passes `--write` / `write=true`; with a `read_only` participant the flag is ignored and the result says so |
| `spec` | an inline spec, merged over the harness spec (see above) |

Participants are names, not harnesses: two entries on the same harness with different models
are two voices, each with its own session, and both can take turns in one conversation.
For Cursor, whose model ids carry the effort (`parley models cursor` lists them):

```json
"cursor-opus": { "harness": "cursor", "model": "claude-opus-5-5-high" },
"cursor-grok": { "harness": "cursor", "model": "grok-4.7-high" }
```

`send_message` takes the same `model` per call, and the `models` MCP tool lists what a
participant's harness accepts. A model different from the participant's gets its own harness
session (claude ignores `--model` when it resumes, codex warns), and the conversation history
still goes in the prompt, so nothing is lost. `parley session reset <name>` clears them all.

A running MCP server notices an edited config file on its next call; a file that no longer
parses is reported and the previous config stays in use. The identity and the store path are
read once, at startup.

| top-level field | meaning |
|---|---|
| `identity` | the local name in transcripts when `PARLEY_IDENTITY` is not set (default `self`) |
| `default_timeout_s` | per-turn timeout (default 300) |
| `max_depth` | how many hops a chain may take (default 3): A asks B, B asks C, C asks D is three. It counts nested sends, not turns in a conversation |
| `store` | SQLite path (default `~/.local/share/parley/parley.db`, or `$PARLEY_DATA`) |

### Read-only, enforced by the harness

Without `--write` / `write=true`, or when the participant is `read_only`, parley launches the
harness in its own read-only mode:

| harness | flags |
|---|---|
| claude | `--permission-mode plan --disallowedTools Write,Edit,MultiEdit,NotebookEdit` |
| opencode | `--agent plan` |
| codex | `-c sandbox_mode="read-only"` |
| cursor | `--mode plan` |
| pi | `--no-tools` |
| hermes | `-t safe` (web, vision, image_gen: no file reads either) |
| omp | `--no-tools` |

The prompt says read-only too, but the flag is what holds. An inline spec without
`read_only_args` cannot be restricted; `parley participants` marks it
`read_only (not enforced …)` instead of pretending.

The read-only flags are a default, not a cage. Override them per participant when a harness's
read-only mode is too tight — Hermes's `safe` toolset cannot read the repo, and its `file`
toolset brings `write_file` and `patch` along with `read_file`:

```json
"hermes":        { "harness": "hermes" },
"hermes-reader": { "harness": "hermes", "dir": "/src/x-worktree",
                   "spec": { "read_only_args": ["-t", "file,web"] } }
```

`hermes` stays truly read-only; `hermes-reader` can read the code but could also write, so it
points at a worktree. An empty list (`"read_only_args": []`) removes the restriction, and
`participants` then says `not enforced`. These modes belong to each harness:
parley does not sandbox anything (see D14 in `docs/decisions.md`).

### Concurrency

parley does not queue. Two sends to the same participant run as two harness processes at
once, in the same directory unless the participant has its own `dir`. Consequences:

- Both see the files as they are; with `write` they can edit the same files. Point a writing
  participant at its own worktree with `dir`.
- A harness you are already using in that directory (an open coding session) is not visible
  to parley; it runs alongside it.
- Session continuity is last-writer-wins: one of the two sends may start a fresh session on
  the following turn (D15). The transcript keeps both turns.

If two turns must not overlap, wait for the first `send` to return.

## How session continuity works

Per participant and per working directory, parley stores one session id. The first turn uses
`start`, later turns use `continue` with that id (Claude: `--session-id` / `--resume`;
OpenCode: `--session`). If the session vanished, parley starts a fresh one instead of failing.
`parley session reset <name>` clears it.

## Guarantees

1. **Read-only by default.** Unless `write=true` and its config allows it, a consulted agent
   runs in its harness's read-only mode (see Configuration).
2. **A timeout is never agreement.** It returns an error and is recorded as an error message.
3. **No cycles.** `PARLEY_CHAIN` travels to the child process; a recipient already in the
   chain is rejected. `PARLEY_DEPTH` stops at three hops.
4. **Each agent owns its context.** Only the message and the explicit context cross the
   channel, never the private session history of the other side.
5. **Prompt content is untrusted.** History, context and the peer's message reach the
   recipient as data. Read-only limits what it can change, not what it can be told to do —
   see the trust model in [`docs/decisions.md`](docs/decisions.md#d14--trust-model-configuration-is-code).

## Limitations

- OpenCode cannot act as an MCP server (client only); parley drives it through its CLI.
- Claude Code cannot be injected into an open TUI; parley continues its session by id.
- No daemon: there is no push into a sleeping session. If that is ever needed, it is a
  separate layer.
- The Pi spec is not verified end-to-end: its JSON mode puts the answer in `message.content[]`,
  an array the adapter cannot read, so its spec uses text mode.
- Concurrent sends to the same participant are not serialized: one of them can lose session
  continuity. The transcript is unaffected. See Concurrency.
- Cancelling cannot recall a request a provider already received, and parley has no token
  counts. See Cost and cancelling.
- The MCP server handles one call at a time, so an agent cannot cancel its own running
  `send_message`; use `parley cancel` from a shell.

## Development

```bash
make hooks        # install .githooks (gofmt + vet + tests on commit, make check on push)
make check        # gofmt + vet + golangci-lint + tests
make race         # go test -race ./...
make vuln         # govulncheck
```

`make check` is the gate. It runs the strict linter config in `.golangci.yml`, which is
what keeps generated code from drifting into `utils/` packages, unchecked errors,
unbounded goroutines or invented dependencies. Tests cover output parsing per format, the
CLI adapter, the SQLite store, cycle and depth guards, the MCP handshake and the updater,
all with fake transports.

Contributor rules live in [`AGENTS.md`](AGENTS.md). To add a tool or a command, follow
[`skills/parley-dev/SKILL.md`](skills/parley-dev/SKILL.md).

## Docs

- [`SKILL.md`](SKILL.md) — the usage skill installed by `parley setup`.
- [`docs/decisions.md`](docs/decisions.md) — what was decided and why, including reversals.
- [`docs/prior-art.md`](docs/prior-art.md) — what already exists and what doesn't.
- [`docs/origin.md`](docs/origin.md) — origin of the project.

## License

MIT — see [LICENSE](LICENSE).
