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
open_conversation(topic="...", context="...")     # multi-turn
send_message(to="claude", conversation_id="cnv_…", message="...")
close_conversation(id="cnv_…", note="conclusion")
```

Or ask in plain language — the bundled skill knows the workflow:

> use the parley skill with claude, review this implementation, and let both of you decide
> what is best

From the shell:

```bash
parley send --to claude "does this break the API contract?"
parley open --topic "cache choice" --context "repo at /src/x"
parley send --to opencode --in cnv_123 "and with a restart?"
parley read cnv_123
parley close cnv_123 --note "in-memory plus a pending benchmark"
parley list
```

Without `--in` / `conversation_id` the conversation is ephemeral: created, answered, closed.

## Update

```bash
parley update --check   # what would change
parley update           # download, verify sha256, replace the binary
```

Updates come from GitHub releases. Builds made with `go install` at a tagged version can
self-update; `dev` builds refuse unless you pass `--force`.

## Supported harnesses

| Harness | Binary | Status |
|---|---|---|
| Claude Code | `claude` | verified |
| OpenCode | `opencode` | verified |
| Codex | `codex` | spec included, not verified end-to-end |
| Cursor | `cursor-agent` | spec included, not verified end-to-end |
| Pi | `pi` | spec included, not verified end-to-end |

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
| `model_args`, `read_only_args`, `write_args` | flags per mode |

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

```json
{
  "participants": {
    "reviewer": { "harness": "claude", "permission": "read_only" },
    "builder":  { "harness": "opencode" },
    "second":   { "harness": "opencode", "model": "<any id opencode accepts>" }
  }
}
```

`harness` is the CLI family. `model` is optional and always native to that harness: parley
passes it through untouched and never interprets it. Omit it to use the harness default.

## How session continuity works

Per participant and per working directory, parley stores one session id. The first turn uses
`start`, later turns use `continue` with that id (Claude: `--session-id` / `--resume`;
OpenCode: `--session`). If the session vanished, parley starts a fresh one instead of failing.
`parley session reset <name>` clears it.

## Guarantees

1. **Read-only by default.** A consulted agent cannot edit files unless `write=true` and its
   config allows it.
2. **A timeout is never agreement.** It returns an error and is recorded as an error message.
3. **No cycles.** `PARLEY_CHAIN` travels to the child process; a recipient already in the
   chain is rejected. `PARLEY_DEPTH` stops at three hops.
4. **Each agent owns its context.** Only the message and the explicit context cross the
   channel, never the private session history of the other side.

## Limitations

- OpenCode cannot act as an MCP server (client only); parley drives it through its CLI.
- Claude Code cannot be injected into an open TUI; parley continues its session by id.
- No daemon: there is no push into a sleeping session. If that is ever needed, it is a
  separate layer.
- The Codex, Cursor and Pi specs are untested end-to-end.

## Development

```bash
go test ./...
go vet ./...
```

Tests cover output parsing per format, the CLI adapter, the SQLite store, cycle and depth
guards, the MCP handshake and the updater, all with fake transports.

## Docs

- [`docs/decisions.md`](docs/decisions.md) — what was decided and why, including reversals.
- [`docs/prior-art.md`](docs/prior-art.md) — what already exists and what doesn't.
- [`docs/origin.md`](docs/contexto.md) — origin of the project.

## License

MIT — see [LICENSE](LICENSE).
