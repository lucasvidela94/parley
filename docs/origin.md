# Origin

Brief that started the project (2026-10-25). Kept so the problem does not get re-derived
from scratch, and so the corrections stay visible.

## What was asked

> "A bridge between harnesses that allows bidirectional communication between different CLIs,
> e.g. opencode with model X and claude code with model Y."

Later:

> "I'm thinking of a Go binary that allows communication, debate and resolution of a given
> point. MCP bridge."

The distribution model comes from `pi-google-services`: one binary, no runtime dependencies,
MCP over JSON-RPC 2.0 on stdio, a `setup` command that writes the harness configuration.

## The correction that shaped the design

> "I can make a direct query from the CLI, it's the same. I don't see the need to wake it,
> because the CLI is never asleep."

Direction accepted: the bridge does not schedule turns or wake agents. The active agent asks
the other one and gets the reply inside its own tool call.

The nuance the design had to solve: an interactive process waiting at the prompt **is**
blocked. What survives is the **session**, and both CLIs can append a turn to it by id.

## The distinction that decides the project

| Approach | What it allows | Is this it? |
|---|---|---|
| Model gateway | send requests to different models | no |
| Harness adapter | run different CLIs behind one interface | no |
| **Session bridge** | independent sessions ask each other, keep conversations | yes |

## Second correction: keep the core agnostic

The first implementation hard-coded a "debate → vote → decision" workflow in the binary. That
made it a one-purpose product. The core now exposes conversations only (`open`, `send`,
`read`, `close`, `list`); debate and resolution are conventions used by the skill, and the
record is written by the agent with its own file tools.
