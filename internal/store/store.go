package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Conversation struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic"`
	Context   string    `json:"context,omitempty"`
	Status    string    `json:"status"` // open | closed
	Dir       string    `json:"dir,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Author         string    `json:"author"`
	Kind           string    `json:"kind"` // message | note | error
	Body           string    `json:"body"`
	CostUSD        float64   `json:"cost_usd,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS sessions (
  participant TEXT NOT NULL,
  dir         TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  PRIMARY KEY (participant, dir)
);
CREATE TABLE IF NOT EXISTS conversations (
  id         TEXT PRIMARY KEY,
  topic      TEXT NOT NULL,
  context    TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL,
  dir        TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  author          TEXT NOT NULL,
  kind            TEXT NOT NULL,
  body            TEXT NOT NULL,
  cost_usd        REAL NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_conversation ON messages(conversation_id, created_at);`)
	return err
}

func (s *Store) Session(ctx context.Context, participant, dir string) (string, bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT session_id FROM sessions WHERE participant=? AND dir=?`, participant, dir).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (s *Store) SetSession(ctx context.Context, participant, dir, id string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(participant, dir, session_id, updated_at) VALUES(?,?,?,?)
ON CONFLICT(participant, dir) DO UPDATE SET session_id=excluded.session_id, updated_at=excluded.updated_at`,
		participant, dir, id, now())
	return err
}

func (s *Store) ClearSession(ctx context.Context, participant, dir string) error {
	if dir == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE participant=?`, participant)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE participant=? AND dir=?`, participant, dir)
	return err
}

func (s *Store) CreateConversation(ctx context.Context, c Conversation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations(id,topic,context,status,dir,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)`, c.ID, c.Topic, c.Context, c.Status, c.Dir, ts(c.CreatedAt), ts(c.UpdatedAt))
	return err
}

func (s *Store) Conversation(ctx context.Context, id string) (Conversation, error) {
	var c Conversation
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,topic,context,status,dir,created_at,updated_at FROM conversations WHERE id=?`, id).
		Scan(&c.ID, &c.Topic, &c.Context, &c.Status, &c.Dir, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, fmt.Errorf("conversation not found: %s", id)
		}
		return c, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, nil
}

func (s *Store) AddMessage(ctx context.Context, m Message) error {
	if m.ID == "" {
		m.ID = NewID("msg")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,author,kind,body,cost_usd,created_at) VALUES(?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Author, m.Kind, m.Body, m.CostUSD, ts(m.CreatedAt))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, ts(time.Now().UTC()), m.ConversationID)
	}
	return err
}

func (s *Store) Messages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,author,kind,body,cost_usd,created_at FROM messages WHERE conversation_id=? ORDER BY created_at, rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		var m Message
		var created string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Author, &m.Kind, &m.Body, &m.CostUSD, &created); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE conversations SET status=?, updated_at=? WHERE id=?`, status, now(), id)
	return err
}

func (s *Store) ListConversations(ctx context.Context, dir string, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id,topic,status,created_at FROM conversations`
	args := []any{}
	if dir != "" {
		q += ` WHERE dir=?`
		args = append(args, dir)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var created string
		if err := rows.Scan(&c.ID, &c.Topic, &c.Status, &created); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, c)
	}
	return out, rows.Err()
}

func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func now() string { return ts(time.Now().UTC()) }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
