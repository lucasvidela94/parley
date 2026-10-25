package core

import (
	"fmt"
	"strings"
)

// Markdown renders a conversation for a model or a human to read.
func (v View) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", v.Conversation.Topic)
	fmt.Fprintf(&b, "Conversation: `%s` · status: %s\n", v.Conversation.ID, v.Conversation.Status)
	if c := strings.TrimSpace(v.Conversation.Context); c != "" {
		b.WriteString("\nContext: " + c + "\n")
	}
	for _, m := range v.Messages {
		switch m.Kind {
		case "note":
			fmt.Fprintf(&b, "\n> note from %s: %s\n", m.Author, strings.TrimSpace(m.Body))
		case "error":
			fmt.Fprintf(&b, "\n**[error from %s]** %s\n", m.Author, oneLine(m.Body, 300))
		default:
			fmt.Fprintf(&b, "\n### %s\n\n%s\n", m.Author, strings.TrimSpace(m.Body))
		}
	}
	return b.String()
}

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		s = s[:limit] + "…"
	}
	return s
}
