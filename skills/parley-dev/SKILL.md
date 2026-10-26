---
name: parley-dev
description: How to add an MCP tool or a CLI command to parley end to end, with tests first. Contributor workflow for this repository.
disable-model-invocation: true
---

# parley-dev

Use this when adding or changing a capability that an agent or a user can invoke.

## The slice, in order

1. **Red test.** Pick the package that owns the behaviour. Write the failing test through
   its public interface, not through internals. `internal/core/engine_test.go` and its
   `fake` harness are the reference; `internal/mcp/server_test.go` shows the protocol
   level.
2. **Core.** Implement in `internal/core` with `ctx` first, store and harness injected.
   Reference implementation: `Engine.Send` (`internal/core/engine.go`) — resolve the
   recipient, guard the chain, persist the user message, render the prompt, call the
   harness, persist the reply, close if ephemeral.
3. **MCP surface.** Add the schema and the dispatch case in `internal/mcp/server.go`.
   Reference: `close_conversation` — schema, `dispatch` case, and the `tools/list`
   assertion in the test.
4. **CLI, only if a human needs it.** Add the flag set and the command case in `main.go`,
   and a line in `usage()`. Reference: `cmdClose`.
5. **Docs.** A new tool changes `README.md` (tools list) and `SKILL.md` (the usage skill)
   in the same commit. A changed rule changes `docs/decisions.md`.
6. **Gate.** `make check`, then `make race` if you touched core, store or concurrency.

## Contract

- Return errors; never print from `internal/`. Text goes back to `main` or to the MCP
  result.
- Persist before answering: a failed harness call still leaves the error message in the
  conversation.
- Keep the core free of decision semantics. If the feature needs a workflow, it belongs in
  a skill, not in a state machine.
- English only: comments, errors, tool descriptions, docs.

Done when the new behaviour has a test that fails without it, `make check` is green, and
the README and SKILL.md describe the tool the way a user would invoke it.
