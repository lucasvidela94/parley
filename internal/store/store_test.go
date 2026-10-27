package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSessions(t *testing.T) {
	s := openTemp(t)
	if _, ok, err := s.Session(context.Background(), "claude", "/x"); err != nil || ok {
		t.Fatalf("expected empty: %v %v", ok, err)
	}
	if err := s.SetSession(context.Background(), "claude", "/x", "abc"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Session(context.Background(), "claude", "/x")
	if err != nil || !ok || got != "abc" {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	if err := s.SetSession(context.Background(), "claude", "/x", "def"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Session(context.Background(), "claude", "/x")
	if got != "def" {
		t.Fatalf("did not update: %q", got)
	}
	if err := s.ClearSession(context.Background(), "claude", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Session(context.Background(), "claude", "/x"); ok {
		t.Fatal("did not clear")
	}
}

func TestConversationRoundtrip(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()
	c := Conversation{ID: NewID("cnv"), Topic: "t", Status: "open", Dir: "/repo", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateConversation(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMessage(context.Background(), Message{ConversationID: c.ID, Author: "claude", Kind: "message", Body: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMessage(context.Background(), Message{ConversationID: c.ID, Author: "opencode", Kind: "message", Body: "two"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Messages(context.Background(), c.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("%d %v", len(msgs), err)
	}
	if msgs[0].Body != "one" || msgs[1].Body != "two" {
		t.Fatalf("orden: %+v", msgs)
	}
	if err := s.SetStatus(context.Background(), c.ID, "closed"); err != nil {
		t.Fatal(err)
	}
	back, err := s.Conversation(context.Background(), c.ID)
	if err != nil || back.Status != "closed" {
		t.Fatalf("%+v %v", back, err)
	}
	list, err := s.ListConversations(context.Background(), "/repo", 10)
	if err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestMessageKeepsItsUsage(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now().UTC()
	c := Conversation{ID: NewID("cnv"), Topic: "t", Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMessage(ctx, Message{ConversationID: c.ID, Author: "a", Kind: "message", Body: "x", InputTokens: 120, OutputTokens: 7, CostUSD: 0.01}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMessage(ctx, Message{ConversationID: c.ID, Author: "b", Kind: "message", Body: "y"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Messages(ctx, c.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("%d %v", len(msgs), err)
	}
	if msgs[0].InputTokens != 120 || msgs[0].OutputTokens != 7 || msgs[0].CostUSD != 0.01 {
		t.Fatalf("usage lost on the way: %+v", msgs[0])
	}
	if msgs[1].InputTokens != 0 || msgs[1].OutputTokens != 0 {
		t.Fatalf("a message with no usage must stay at zero: %+v", msgs[1])
	}
}

func TestOpenMigratesAnOlderStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// The schema as the first release wrote it.
	if _, err := db.Exec(`
CREATE TABLE conversations (id TEXT PRIMARY KEY, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL, dir TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, author TEXT NOT NULL, kind TEXT NOT NULL,
  body TEXT NOT NULL, cost_usd REAL NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
INSERT INTO conversations VALUES ('cnv_old','t','','open','/r','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
INSERT INTO messages VALUES ('msg_old','cnv_old','self','message','hi',0,'2026-01-01T00:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	if ok, err := s.SpendTurn(ctx, "cnv_old"); err != nil || !ok {
		t.Fatalf("spend: %v %v", ok, err)
	}
	c, err := s.Conversation(ctx, "cnv_old")
	if err != nil || c.Turns != 1 || c.MaxTurns != 0 {
		t.Fatalf("%+v %v", c, err)
	}
	msgs, err := s.Messages(ctx, "cnv_old")
	if err != nil || len(msgs) != 1 || msgs[0].Body != "hi" {
		t.Fatalf("%+v %v", msgs, err)
	}
	// The columns added later must be writable on a store the first release made.
	if err := s.AddMessage(ctx, Message{ConversationID: "cnv_old", Author: "x", Kind: "message", Body: "new", InputTokens: 3, OutputTokens: 4}); err != nil {
		t.Fatalf("an older store must accept usage after migrating: %v", err)
	}
}

func TestSpendTurnStopsAtTheLimit(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now().UTC()
	c := Conversation{ID: NewID("cnv"), Topic: "t", Status: "open", MaxTurns: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false, false} {
		if ok, err := s.SpendTurn(ctx, c.ID); err != nil || ok != want {
			t.Fatalf("spend %d: %v %v", i, ok, err)
		}
	}
	if back, _ := s.Conversation(ctx, c.ID); back.Turns != 1 {
		t.Fatalf("a refused turn was counted: %d", back.Turns)
	}
}

func TestRequestCancelDropsTurnsOfDeadProcesses(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	live, err := s.StartTurn(ctx, "cnv_1", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.RequestCancel(ctx, "cnv_1", time.Now().Add(-time.Hour)); err != nil || len(got) != 1 || got[0].ID != live {
		t.Fatalf("live turn: %+v %v", got, err)
	}
	if stop, err := s.TurnCancelled(ctx, live); err != nil || !stop {
		t.Fatalf("flag not seen: %v %v", stop, err)
	}
	// Nobody polls a turn whose process died, so its heartbeat is old.
	if got, err := s.RequestCancel(ctx, "cnv_1", time.Now().Add(time.Hour)); err != nil || len(got) != 0 {
		t.Fatalf("a dead turn was reported as running: %+v %v", got, err)
	}
}

func TestUnknownConversation(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Conversation(context.Background(), "cnv_nope"); err == nil {
		t.Fatal("expected error")
	}
}
