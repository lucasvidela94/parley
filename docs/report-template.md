Report: one template for a bug or a topic, in any tracker or channel. Copy as plain text.

Claim:
One sentence: what is wrong, or what is worth discussing.

Evidence:
Something another person can check without trusting the author: a command
anyone can run, a `file:line`, a quoted doc, a pasted output. Prose that only
restates the claim is not evidence.

Observed:
What the evidence shows, pasted, not summarized.

Expected:
What it should show, and where that expectation comes from (`file:line`, a
documented decision, an agreement in a conversation).

Check that would go red:
A test or command that fails today and passes once this is resolved. If there
is none (a design question, a discussion topic), say why.

Falsifier:
The result that would show the claim is wrong: the check passes today, the
behaviour is intended, the cause is elsewhere.

Raised by:
One of: a person (name or role); an agent (harness and model, for example
`opencode / deepseek-chat`); a conversation (where and when: a parley
conversation id, a thread, a meeting). Whoever verifies a report raised by an
agent should run a different model.
