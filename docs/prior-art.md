# Prior art (verified 2026-10-25)

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

Others in the same niche: `tofutools/tclaude` (daemon + tmux, 6 harnesses), `cordfuse/llmux`
(tmux `send-keys`), `clankercode/c2c` (no license), `dataforxyz/agent-intercom-opencode`
(AGPL), `Co-Messi/agent-peers-mcp` (lease + ack).

## What the ecosystem converged on

1. **Broker + durable mailbox** is the dominant architecture. Everyone puts a long-lived
   process between sessions and persists before acknowledging.
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
