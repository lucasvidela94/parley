package store

import (
	"context"
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

func TestUnknownConversation(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Conversation(context.Background(), "cnv_nope"); err == nil {
		t.Fatal("expected error")
	}
}
