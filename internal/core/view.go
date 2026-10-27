package core

import (
	"errors"
	"fmt"
	"strings"
)

// Page narrows the view to a window of whole messages, so a long transcript
// can be read in pieces without cutting a turn in half. tail > 0 keeps the last
// tail messages and excludes offset; otherwise the window starts at offset and
// holds at most limit messages (limit 0 = all the rest).
func (v View) Page(offset, limit, tail int) (View, error) {
	if offset < 0 || limit < 0 || tail < 0 {
		return v, errors.New("offset, limit and tail cannot be negative")
	}
	if tail > 0 && offset > 0 {
		return v, errors.New("use either tail or offset, not both")
	}
	start, end := offset, len(v.Messages)
	if tail > 0 {
		start = max(0, end-tail)
	}
	start = min(start, end)
	if limit > 0 {
		end = min(end, start+limit)
	}
	v.Offset += start
	v.Messages = v.Messages[start:end]
	return v, nil
}

// Markdown renders a conversation for a model or a human to read.
func (v View) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", v.Conversation.Topic)
	fmt.Fprintf(&b, "Conversation: `%s` · status: %s · %s", v.Conversation.ID, v.Conversation.Status, turns(v))
	if v.CostUSD > 0 {
		fmt.Fprintf(&b, " · cost: $%.4f", v.CostUSD)
	}
	b.WriteString("\n")
	if len(v.Authors) > 0 {
		names := make([]string, 0, len(v.Authors))
		for _, a := range v.Authors {
			if len(a.Models) > 0 {
				names = append(names, a.Name+" ("+strings.Join(a.Models, ", ")+")")
			} else {
				names = append(names, a.Name)
			}
		}
		b.WriteString("Participants: " + strings.Join(names, ", ") + "\n")
	}
	for _, r := range v.Running {
		fmt.Fprintf(&b, "\n_turn in progress: %s, since %s_\n", r.Participant, r.StartedAt.Format("15:04:05Z"))
	}
	if c := strings.TrimSpace(v.Conversation.Context); c != "" {
		b.WriteString("\nContext: " + c + "\n")
	}
	switch {
	case len(v.Messages) == 0 && v.Total > 0:
		fmt.Fprintf(&b, "\n_no messages in this range (%d in total)_\n", v.Total)
	case len(v.Messages) < v.Total:
		fmt.Fprintf(&b, "\n_messages %d–%d of %d_\n", v.Offset+1, v.Offset+len(v.Messages), v.Total)
	}
	for i := range v.Messages {
		m := &v.Messages[i]
		at := m.CreatedAt.Format("2006-01-02 15:04:05Z")
		switch m.Kind {
		case "note":
			fmt.Fprintf(&b, "\n> note from %s (%s): %s\n", m.Author, at, strings.TrimSpace(m.Body))
		case "error":
			fmt.Fprintf(&b, "\n**[error from %s, %s]** %s\n", m.Author, at, oneLine(m.Body, errorLimit))
		default:
			label := m.Author
			if m.Model != "" {
				label += " · " + m.Model
			}
			fmt.Fprintf(&b, "\n### %s\n_%s_\n\n%s\n", label, at, strings.TrimSpace(m.Body))
		}
	}
	return b.String()
}

// CloseSummary confirms a close: the id, the status and the note it recorded.
// The transcript is one `read` away; echoing it hid whether the close happened.
func (v View) CloseSummary() string {
	s := fmt.Sprintf("%s %s · %s", v.Conversation.Status, v.Conversation.ID, turns(v))
	if n := len(v.Messages); n > 0 && v.Messages[n-1].Kind == "note" {
		s += "\nnote: " + strings.TrimSpace(v.Messages[n-1].Body)
	}
	return s
}

// turns counts every turn that was spent, including the ones that ended in an
// error, and says how many of those there were.
func turns(v View) string {
	s := fmt.Sprintf("turns: %d", v.Conversation.Turns)
	if v.Conversation.MaxTurns > 0 {
		s = fmt.Sprintf("turns: %d/%d", v.Conversation.Turns, v.Conversation.MaxTurns)
	}
	if v.Failed > 0 {
		s += fmt.Sprintf(" (%d failed)", v.Failed)
	}
	return s
}

// errorLimit is the harness stderr tail (1200) plus its prefix: an error
// usually ends with the remedy, so cutting it short loses the useful part.
const errorLimit = 1400

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		s = s[:limit] + "…"
	}
	return s
}
