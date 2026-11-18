package core

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lucasvidela94/parley/internal/store"
)

type Usage struct {
	Since   *time.Time       `json:"since,omitempty"`
	Rows    []store.UsageRow `json:"rows"`
	CostUSD float64          `json:"cost_usd,omitempty"`
}

func (e *Engine) Usage(ctx context.Context, since time.Time) (Usage, error) {
	rows, err := e.st.Usage(ctx, since)
	if err != nil {
		return Usage{}, err
	}
	if rows == nil {
		rows = []store.UsageRow{}
	}
	u := Usage{Rows: rows}
	if !since.IsZero() {
		t := since.UTC()
		u.Since = &t
	}
	for i := range rows {
		u.CostUSD += rows[i].CostUSD
	}
	return u, nil
}

func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		const maxDays = int(math.MaxInt64 / int64(24*time.Hour))
		if days, err := strconv.Atoi(n); err == nil && days > 0 && days <= maxDays {
			return now.Add(-time.Duration(days) * 24 * time.Hour).UTC(), nil
		}
		if days, err := strconv.Atoi(n); err == nil && days > 0 {
			return time.Time{}, fmt.Errorf("since %q is too far back", s)
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(-d).UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("since %q is not a duration (24h, 7d) or a date (2026-11-01)", s)
}

func (u Usage) Markdown() string {
	var b strings.Builder
	header := "usage"
	if u.Since != nil {
		header += " since " + u.Since.Format(time.RFC3339)
	}
	fmt.Fprintf(&b, "## %s\n\n", header)
	if len(u.Rows) == 0 {
		b.WriteString("no messages in this window\n")
		return b.String()
	}
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "participant\tmodel\tsource\tmessages\tfailed\tcost\treported\ttokens in/out")
	var noCost int
	for _, r := range u.Rows {
		model := r.Model
		if model == "" {
			model = "(unknown)"
		}
		source := r.ModelSource
		if source == "" {
			source = "—"
		}
		cost := "—"
		if r.CostUSD > 0 {
			cost = fmt.Sprintf("$%.2f", r.CostUSD)
		} else {
			noCost++
		}
		reported := "—"
		if r.Messages > 0 {
			reported = fmt.Sprintf("cost %d/%d · tok %d/%d", r.CostReported, r.Messages, r.TokensReported, r.Messages)
		}
		tokens := "—"
		if r.InputTokens+r.OutputTokens+r.CacheReadTokens+r.CacheWriteTokens > 0 {
			tokens = fmt.Sprintf("%d/%d", r.InputTokens, r.OutputTokens)
			if r.CacheReadTokens+r.CacheWriteTokens > 0 {
				tokens += fmt.Sprintf(" (cache %d/%d)", r.CacheReadTokens, r.CacheWriteTokens)
			}
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\n", r.Participant, model, source, r.Messages, r.Failed, cost, reported, tokens)
	}
	_ = w.Flush()
	fmt.Fprintf(&b, "\ncost: $%.2f over %d rows; %d reported none\n", u.CostUSD, len(u.Rows), noCost)
	b.WriteString("tokens are per harness and do not add up across rows\n")
	b.WriteString("messages include questions written from this side: the store does not record which messages are replies\n")
	return b.String()
}
