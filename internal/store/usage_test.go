package store

import (
	"context"
	"testing"
	"time"
)

func TestUsageGroupsByParticipantAndModel(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	c := Conversation{ID: NewID("cnv"), Topic: "t", Status: "open", CreatedAt: time.Now().UTC()}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, m := range []Message{
		{Author: "claude", Kind: "message", Model: "opus", ModelSource: "configured", CostUSD: 0.5,
			InputTokens: 2, OutputTokens: 10, CacheReadTokens: 100, CacheWriteTokens: 200, CreatedAt: now},
		{Author: "claude", Kind: "message", Model: "opus", ModelSource: "configured", CostUSD: 0.25, CreatedAt: now},
		{Author: "claude", Kind: "error", Model: "opus", ModelSource: "configured", CreatedAt: now},
		{Author: "opencode", Kind: "message", CostUSD: 0.01, CreatedAt: now.Add(-48 * time.Hour)},
	} {
		m.ConversationID = c.ID
		if err := s.AddMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.Usage(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("since 24h: %+v", rows)
	}
	want := UsageRow{Participant: "claude", Model: "opus", ModelSource: "configured", Messages: 2, Failed: 1, CostUSD: 0.75, CostReported: 2,
		InputTokens: 2, OutputTokens: 10, CacheReadTokens: 100, CacheWriteTokens: 200, TokensReported: 1}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("row: %+v", rows)
	}

	all, err := s.Usage(ctx, time.Time{})
	if err != nil || len(all) != 2 {
		t.Fatalf("all time: %+v %v", all, err)
	}
	if all[0].Participant != "claude" || all[1].Participant != "opencode" {
		t.Fatalf("cost orders the rows: %+v", all)
	}
}
