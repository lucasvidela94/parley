package core

import (
	"fmt"
	"strings"

	"github.com/lucasvidela94/parley/internal/store"
)

type promptInput struct {
	identity     string
	name         string
	dir          string
	readOnly     bool
	message      string
	context      string
	history      []store.Message
	conversation store.Conversation
}

func renderPrompt(in promptInput) string {
	mode := "read-only: do not edit files or run commands that mutate state"
	if !in.readOnly {
		mode = "you may edit files in " + in.dir
	}
	var b strings.Builder
	fmt.Fprintf(&b, `You are connected through parley, a channel between different coding agent harnesses.
You are %q. %q is writing to you. Working directory: %s.
Mode: %s.

Rules:
- Answer the message. Do not invent: every technical claim must rest on something verifiable (file:line, command, tool output). What you cannot verify, state as an open question.
- If it asks you to do something, do it or explain why not. If it is a question, answer it.
- You do not have to close with a conclusion: if the other side wants to keep going, it will write again.
`, in.name, in.identity, in.dir, mode)

	if tr := historyText(in.history); tr != "" {
		b.WriteString("\nCONVERSATION SO FAR\n")
		b.WriteString(tr)
	}
	if c := strings.TrimSpace(in.context); c != "" {
		b.WriteString("\n\nCONTEXT\n")
		b.WriteString(c)
	}
	fmt.Fprintf(&b, "\n\nMESSAGE FROM %s\n%s\n", in.identity, strings.TrimSpace(in.message))
	return b.String()
}

func historyText(msgs []store.Message) string {
	var b strings.Builder
	for i := range msgs {
		m := &msgs[i]
		if m.Kind == "error" {
			continue
		}
		label := m.Author
		if m.Kind == "note" {
			label += " (note)"
		}
		fmt.Fprintf(&b, "\n[%s]\n%s\n", label, strings.TrimSpace(m.Body))
	}
	out := b.String()
	const limit = 16000
	if len(out) <= limit {
		return strings.TrimSpace(out)
	}
	return "… (truncated) …\n" + out[len(out)-(limit-20):]
}
