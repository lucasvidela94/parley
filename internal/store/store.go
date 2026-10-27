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
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Conversation struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic"`
	Context   string    `json:"context,omitempty"`
	Status    string    `json:"status"` // open | closed
	Dir       string    `json:"dir,omitempty"`
	MaxTurns  int       `json:"max_turns,omitempty"` // 0 = no limit
	Turns     int       `json:"turns"`               // turns spent, failed ones included
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	Author         string  `json:"author"`
	Kind           string  `json:"kind"`            // message | note | error
	Model          string  `json:"model,omitempty"` // the participant's configured model, when set
	Body           string  `json:"body"`
	CostUSD        float64 `json:"cost_usd,omitempty"`
	// Tokens exactly as the harness reported them; 0 means it said nothing.
	// Harnesses count cache tokens differently, so they do not add up across
	// participants.
	InputTokens  int       `json:"input_tokens,omitempty"`
	OutputTokens int       `json:"output_tokens,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// RunningTurn is a harness call in progress, recorded so another process can
// ask for it to stop.
type RunningTurn struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Participant    string    `json:"participant"`
	StartedAt      time.Time `json:"started_at"`
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
CREATE INDEX IF NOT EXISTS messages_conversation ON messages(conversation_id, created_at);
CREATE TABLE IF NOT EXISTS running_turns (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  participant     TEXT NOT NULL,
  cancel          INTEGER NOT NULL DEFAULT 0,
  started_at      TEXT NOT NULL,
  seen_at         TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	// Columns added after the first release; CREATE TABLE IF NOT EXISTS does
	// not touch a table that already exists.
	for _, c := range []struct{ table, column, decl string }{
		{"conversations", "max_turns", "INTEGER NOT NULL DEFAULT 0"},
		{"conversations", "turns", "INTEGER NOT NULL DEFAULT 0"},
		{"messages", "model", "TEXT NOT NULL DEFAULT ''"},
		{"messages", "input_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"messages", "output_tokens", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumn(ctx, c.table, c.column, c.decl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) addColumn(ctx context.Context, table, column, decl string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// table, column and decl are constants from migrate, never input.
	_, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl)
	if err != nil && strings.Contains(err.Error(), "duplicate column name") {
		return nil // another process opening the same store migrated it first
	}
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
		// A participant's sessions under a model override are keyed
		// "<participant>\x1f<model>" and go with it.
		_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE participant=?1 OR substr(participant,1,length(?1)+1)=?1||char(31)`, participant)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE (participant=?1 OR substr(participant,1,length(?1)+1)=?1||char(31)) AND dir=?2`, participant, dir)
	return err
}

func (s *Store) CreateConversation(ctx context.Context, c Conversation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations(id,topic,context,status,dir,max_turns,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)`, c.ID, c.Topic, c.Context, c.Status, c.Dir, c.MaxTurns, ts(c.CreatedAt), ts(c.UpdatedAt))
	return err
}

func (s *Store) Conversation(ctx context.Context, id string) (Conversation, error) {
	var c Conversation
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,topic,context,status,dir,max_turns,turns,created_at,updated_at FROM conversations WHERE id=?`, id).
		Scan(&c.ID, &c.Topic, &c.Context, &c.Status, &c.Dir, &c.MaxTurns, &c.Turns, &created, &updated)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,author,kind,model,body,cost_usd,input_tokens,output_tokens,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Author, m.Kind, m.Model, m.Body, m.CostUSD, m.InputTokens, m.OutputTokens, ts(m.CreatedAt))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, ts(time.Now().UTC()), m.ConversationID)
	}
	return err
}

func (s *Store) Messages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,author,kind,model,body,cost_usd,input_tokens,output_tokens,created_at FROM messages WHERE conversation_id=? ORDER BY created_at, rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		var m Message
		var created string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Author, &m.Kind, &m.Model, &m.Body, &m.CostUSD, &m.InputTokens, &m.OutputTokens, &created); err != nil {
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

// SpendTurn counts one turn against the conversation's limit. It reports false,
// and counts nothing, when the limit is already reached. The check and the
// increment are one statement, so two concurrent sends cannot both slip past.
func (s *Store) SpendTurn(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE conversations SET turns=turns+1, updated_at=?
WHERE id=? AND (max_turns=0 OR turns<max_turns)`, now(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) StartTurn(ctx context.Context, conversationID, participant string) (string, error) {
	id := NewID("trn")
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO running_turns(id,conversation_id,participant,started_at,seen_at) VALUES(?,?,?,?,?)`,
		id, conversationID, participant, t, t)
	return id, err
}

func (s *Store) EndTurn(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM running_turns WHERE id=?`, id)
	return err
}

// TurnCancelled reports whether someone asked the turn to stop. Each call is
// also the turn's heartbeat: a row nobody polls belongs to a dead process.
func (s *Store) TurnCancelled(ctx context.Context, id string) (bool, error) {
	var c int
	err := s.db.QueryRowContext(ctx, `UPDATE running_turns SET seen_at=? WHERE id=? RETURNING cancel`, now(), id).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return c == 1, err
}

// RequestCancel flags every live turn of a conversation and returns them.
// Turns not seen since staleBefore were left by a process that died; they are
// dropped instead of reported as running.
func (s *Store) RequestCancel(ctx context.Context, conversationID string, staleBefore time.Time) ([]RunningTurn, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM running_turns WHERE conversation_id=? AND seen_at<?`, conversationID, ts(staleBefore)); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE running_turns SET cancel=1 WHERE conversation_id=?`, conversationID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,participant,started_at FROM running_turns WHERE conversation_id=? ORDER BY started_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RunningTurn
	for rows.Next() {
		var r RunningTurn
		var started string
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.Participant, &started); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunningTurns lists the live turns of a conversation without touching them.
// Turns not seen since staleBefore belong to a process that died and are left
// out.
func (s *Store) RunningTurns(ctx context.Context, conversationID string, staleBefore time.Time) ([]RunningTurn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,participant,started_at FROM running_turns WHERE conversation_id=? AND seen_at>=? ORDER BY started_at`, conversationID, ts(staleBefore))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RunningTurn
	for rows.Next() {
		var r RunningTurn
		var started string
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.Participant, &started); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		out = append(out, r)
	}
	return out, rows.Err()
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
