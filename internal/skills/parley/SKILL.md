---
name: parley
description: Talk to another agent harness (Claude Code, OpenCode, Codex, Pi, Cursor) through the parley binary. Use it when the user says "ask claude", "consult opencode", "have the other one review this", "debate this decision", "let both decide", or when you want a second opinion with evidence from a different model.
---

# parley

parley is a channel. It does not decide, vote or schedule turns: it carries messages between
harnesses and stores the conversation. The conclusion is yours to write.

## Tools

MCP server `parley`: `participants`, `open_conversation`, `send_message`, `read_conversation`,
`close_conversation`, `list_conversations`.

If the MCP server is not configured, the same binary works from the shell:

```bash
parley participants
parley send --to claude "message"                    # ephemeral, one turn
parley send --to claude --in cnv_123 "follow-up"     # continue
parley open --topic "..." --context "..."
parley read cnv_123
parley close cnv_123 --note "..."
```

## Pattern A — direct question

One shot:

```
send_message(to="claude", message="does this compile error come from the change in store.go?",
             context="file: internal/store/store.go:120")
```

With no `conversation_id` the conversation is ephemeral: created, answered, closed.

## Pattern B — multi-turn conversation

When it needs back and forth:

1. `open_conversation(topic, context)` → `cnv_...`
2. `send_message(to=..., conversation_id=cnv_..., message=...)` as many times as needed. The
   recipient sees the previous turns each time.
3. `read_conversation(cnv_...)` if you need to re-read.
4. `close_conversation(cnv_..., note="...")` when done. Nobody writes after that.

## Pattern C — debate with a conclusion

The user says: *"use parley with claude, review the implementation, and let both of you
decide what is best"*.

1. **Write your position first.** Read the code and state your case with evidence
   (`file:line`, commands). Do not delegate the review before having your own.
2. `open_conversation(topic="...", context="<repo, scope, constraints>")`.
3. `send_message(conversation_id, to="claude", message=
   "My position: ... Evidence: ... Attack the weak points and tell me what you would change.")`
   — ask for criticism, not confirmation.
4. Read the reply. If an objection is valid, **concede** and say so. If not, rebut with new
   evidence, not by repeating your position: `send_message(conversation_id, ...)`.
5. One or two rounds is enough. More is ping-pong.
6. `close_conversation(conversation_id, note="conclusion in one or two sentences")`.
7. **Write the record yourself** with your file tools, at `docs/decisions/NNN-slug.md`:
   question, positions, open objections, conclusion, and what supports it. parley does not
   generate that file.

## Rules

- **A timeout or an error is never agreement.** If the other side did not answer, say so.
- **Read-only by default.** The recipient cannot edit files unless you pass `write=true` and
  its config allows it. If it needs to change something, have it return a diff.
- **Explicit context, not a dump.** Send paths, prior decisions and constraints; do not paste
  your whole session history.
- **Evidence or open question.** If a claim cannot be verified, do not present it as fact.
- If the user names an agent that is not in `participants`, show the list and ask which one.
  Never invent names.
- When reporting to the human: conclusion, remaining disagreements, and the file with the
  record.
