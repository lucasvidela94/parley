---
name: parley
description: Talk to another agent harness (Claude Code, OpenCode, Codex, Pi, Cursor) through parley. Use when the user says "ask claude", "consult opencode", "have the other one review this", "debate this", "let both decide", or when a second opinion from a different model would settle a question.
---

# parley

parley is a channel. It carries messages between harnesses and stores the conversation.
It does not decide, vote or schedule turns. The conclusion is yours to write.

## Tools

MCP server `parley`: `participants`, `open_conversation`, `send_message`,
`read_conversation`, `close_conversation`, `list_conversations`.

Same operations from a shell when the MCP server is not registered:

```bash
parley participants
parley send --to claude "message"                    # ephemeral, one turn
parley send --to claude --in cnv_123 "follow-up"     # continue
parley open --topic "..." --context "..."
parley close cnv_123 --note "..."
```

Start with `participants` when the user names an agent you have not used in this session.
If the name is not there, show the list and ask; never invent one.

## A single question

Call `send_message(to, message, context?)` and report the reply.

Done when the answer is in your context and you have passed it on, with the agent's name
and any open question it raised.

## A conversation across turns

1. `open_conversation(topic, context)` → `cnv_...`
2. `send_message(to, conversation_id, message)` once per turn. The recipient sees the
   earlier turns on every call.
3. `read_conversation(cnv_...)` when you need the transcript back.
4. `close_conversation(cnv_..., note)` at the end.

Done when the conversation is closed and the note states the outcome or the next step.

## A review or a debate that ends in a decision

This is the pattern for "review this with claude and let both of you decide".

1. **Form your own position first.** Read the code and write it with evidence
   (`file:line`, command output). A review you delegate before doing is a review you
   cannot judge.
2. `open_conversation(topic, context)` with the repo, the scope and the constraints.
3. `send_message(to, conversation_id, message)` with your position and an explicit ask:
   attack the weak points, say what you would change. Ask for criticism, not agreement.
4. Concede what is right, out loud. Rebut with new evidence, not by restating your
   position.
5. One or two rounds. More is ping-pong.
6. `close_conversation(conversation_id, note)` with the decision in one or two sentences.
7. Write the record yourself with your own file tools: question, positions, objections,
   decision, evidence. `docs/decisions/NNN-slug.md` in the project you are working on.

Done when the record exists and lists the objections that stayed open.

## Guardrails

- **A timeout or an error is not agreement.** Say the other side did not answer.
- **Read-only by default.** Pass `write=true` only when the change is the point, and say
  so when reporting.
- **Send pointers, not a dump.** Paths, prior decisions and constraints beat pasted
  history.
- **Evidence or open question.** A claim you cannot verify is not a finding.
- Report to the human: the conclusion, what is still disputed, and where the record lives.
