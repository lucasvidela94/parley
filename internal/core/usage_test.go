package core

import (
	"strings"
	"testing"
	"time"

	"github.com/lucasvidela94/parley/internal/store"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
		bad  bool
	}{
		{in: "", want: time.Time{}},
		{in: "24h", want: now.Add(-24 * time.Hour)},
		{in: "7d", want: now.Add(-7 * 24 * time.Hour)},
		{in: "2026-11-01", want: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{in: "yesterday", bad: true},
		{in: "0d", bad: true},
		{in: "-3d", bad: true},
		{in: "200000d", bad: true},
	}
	for _, c := range cases {
		got, err := ParseSince(c.in, now)
		if c.bad {
			if err == nil {
				t.Fatalf("%q: expected an error, got %v", c.in, got)
			}
			continue
		}
		if err != nil || !got.Equal(c.want) {
			t.Fatalf("%q: got %v (%v), want %v", c.in, got, err, c.want)
		}
	}
}

func TestUsageMarkdownKeepsTokensAndCostsApart(t *testing.T) {
	since := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	u := Usage{
		Since: &since,
		Rows: []store.UsageRow{
			{Participant: "claude", Model: "sonnet", ModelSource: "reported", Messages: 3, Failed: 1, CostUSD: 0.75, CostReported: 2,
				InputTokens: 2, OutputTokens: 32, CacheReadTokens: 9000, CacheWriteTokens: 17079, TokensReported: 3},
			{Participant: "opencode", Messages: 78},
		},
		CostUSD: 0.75,
	}
	md := u.Markdown()
	for _, want := range []string{
		"usage since 2026-11-11T00:00:00Z",
		"claude", "sonnet", "(unknown)", "reported",
		"$0.75", "cost 2/3", "tok 3/3",
		"2/32 (cache 9000/17079)",
		"1 reported none",
		"do not add up",
		"messages include questions",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
}
