# Prior art (verified 2026-10-25, refreshed 2026-10-27)

Checked against the GitHub API, not against second-hand summaries. Claims that turned out to
be wrong are listed at the end.

| Project | What it does | Transport | OpenCode ↔ Claude |
|---|---|---|---|
| [raysonmeng/agent-bridge](https://github.com/raysonmeng/agent-bridge) (407★, MIT) | Claude Code ↔ Codex | MCP channel ↔ daemon ↔ Codex app-server over WebSocket | no (OpenCode is on the roadmap) |
| [kevinArgueta96/open-agent-bridge](https://github.com/kevinArgueta96/open-agent-bridge) (5★, MIT) | Claude Code, OpenCode, Codex, Antigravity | MCP stdio + HTTP/WS registry + SQLite; injects with `session.prompt_async` | yes |
| [summationai/agent-exchange](https://github.com/summationai/agent-exchange) (7★, MIT, `ax`) | local broker, named identities, mailbox | private Unix socket + SQLite WAL; optional Tailscale | yes |
| [drewstone/cli-bridge](https://github.com/drewstone/cli-bridge) (3★, MIT) | CLIs as an OpenAI-compatible API | HTTP; `X-Session-Id` to resume | not applicable: gateway, not messaging |
| [Oryntai/AgentLink](https://github.com/Oryntai/AgentLink) (0★, MIT) | encrypted bridge between two agents | broker ↔ WebSocket relay (ngrok) | no (Codex/Claude) |
| [agentclientprotocol](https://github.com/agentclientprotocol/agent-client-protocol) (4.5k★, Apache-2.0) | editor ↔ agent protocol | JSON-RPC over stdio | no: it is not agent ↔ agent |
| [prassanna-ravishankar/repowire](https://github.com/prassanna-ravishankar/repowire) (269★, no license detected, v0.21.1) | named peers across repos, dashboard, scheduled check-ins | local daemon (Go) hooked into the live sessions; tmux for Claude Code; optional hosted relay | yes (Claude Code, Codex, OpenCode, Pi) |
| [aannoo/hcom](https://github.com/aannoo/hcom) (605★, MIT, v0.7.31) | message, watch and spawn agents across terminals and machines | live delivery into running sessions (mid-turn or waking an idle one); launched as `hcom claude` | yes (14 CLIs listed, including grok, hermes, pi, omp, gemini, copilot, kilo, qoder) |
| [mubashir1osmani/agent-mesh](https://github.com/mubashir1osmani/agent-mesh) (6★, MIT, v0.0.4) | MCP server: list peers, open or join a peer's session, send a prompt, read the reply | MCP over stdio, no daemon | yes (Claude Code, Codex, OpenCode, Gemini, Grok) |

### Excluded with reason (refresh of 2026-10-27)

Checked against the GitHub API on 2026-10-27. Not archived, but no push in the 30 days before
the check, so they are not a current reference:
`Hackerprod/Agent-Orchestrator` (1★, last push 2026-08-07, no release),
`instavm/murmur` (39★, 2026-07-17, no release),
`luisestebanveragomez/agents-mesh` (3★, 2026-08-19, v0.2.23),
`anthonylee991/agentmesh` (1★, 2026-03-10, v0.1.0).
`rithamnatani/agentmux` (0★, 2026-08-03, no release, no license) is also stale, and its
description ("orchestrating multiple AI coding CLIs as subagents") is orchestration, not
messaging between sessions.

Others in the same niche: `tofutools/tclaude` (daemon + tmux, 6 harnesses), `cordfuse/llmux`
(tmux `send-keys`), `clankercode/c2c` (no license), `dataforxyz/agent-intercom-opencode`
(AGPL), `Co-Messi/agent-peers-mcp` (lease + ack).

## What the ecosystem converged on

1. **Broker + durable mailbox** is the dominant architecture. Everyone puts a long-lived
   process between sessions and persists before acknowledging. Not everyone: `agent-mesh`
   is a stdio MCP server with no daemon that opens the peer's session and returns the reply,
   which is parley's spawn-sync choice. It is not unique, only uncommon.
2. **MCP is the tool surface, never the wake mechanism.** A stdio MCP server cannot open a turn
   in another session. Every project that wakes a peer adds a harness-specific path: Claude
   Code's inbox socket, Codex `turn/start`, an OpenCode plugin, tmux as fallback.
3. **Delivery semantics**: named identities, lease/ack, replay, and distinct states for
   *stored → accepted by the harness → read by the model*.
4. **Nobody solved the debate.** Everyone moves messages. None turns the exchange into a
   decision with state, objections and evidence — or, if they do, they hard-code one workflow.

## Corrections to the original brief

- `open-agent-bridge` does not support Pi; it supports Claude Code, OpenCode, Codex and
  Antigravity.
- "AgentLink / Agent P2P by Orynth" conflates two repos from different owners:
  `Oryntai/AgentLink` and `satorisz9/agent-p2p`.
- AgentLink is not direct P2P: it is broker ↔ encrypted relay.
- ACP does not connect two harnesses to each other; it needs a client in between.
- Claude Code has the same problem: its own agent teams use a mailbox plus tmux, and there are
  issues about teammates stuck at the prompt because nobody polls the mailbox.
