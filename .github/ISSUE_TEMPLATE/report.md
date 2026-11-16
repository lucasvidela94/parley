---
name: Report
about: A bug or a topic, with evidence someone else can check. Same text as docs/report-template.md.
---
# Report

<!--
One template for any report, wherever it is raised: an issue tracker, a chat
channel, a ticket, a note from a meeting. Copy it as plain text. A report
without evidence someone else can check is a hypothesis: say so in the claim.
-->

## Claim

One sentence: what is wrong, or what is worth discussing.

## Evidence

Something another person can run or check without trusting the author: a
command on a clean checkout, a `file:line`, a quoted doc, a pasted output.
Prose that only restates the claim is not evidence.

## Observed

What the evidence shows, pasted, not summarized.

## Expected

What it should show, and where that expectation comes from (`file:line`, a
documented decision, an agreement in a conversation).

## Check that would go red

A test or command that fails today and passes once this is resolved. If there
is none (a design question, a discussion topic), say why.

## Falsifier

The result that would show the claim is wrong: the check passes today, the
behaviour is intended, the cause is elsewhere.

## Raised by

One of:

- a person: name or role;
- an agent: harness and model, for example `opencode / deepseek-chat`;
- a conversation: where and when, for example a parley conversation id, a
  thread, a meeting.

Whoever verifies a report raised by an agent should run a different model.
