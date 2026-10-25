package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasvidela94/parley/internal/config"
	"github.com/lucasvidela94/parley/internal/harness"
	"github.com/lucasvidela94/parley/internal/store"
)

var (
	ErrLoop  = errors.New("agent cycle")
	ErrDepth = errors.New("maximum depth reached")
)

// Engine is the whole bridge: participants, their sessions, and conversations.
// It knows nothing about debates, votes or decisions. Those are conventions the
// agents agree on over plain messages.
type Engine struct {
	cfg        config.Config
	st         *store.Store
	identity   string
	depth      int
	chain      []string
	newHarness func(config.Participant) harness.Harness
}

type SendRequest struct {
	ConversationID string // empty = ephemeral one-turn conversation
	To             string
	Message        string
	Context        string
	Write          bool
	TimeoutS       int
}

type SendResult struct {
	Conversation store.Conversation `json:"conversation"`
	Reply        store.Message      `json:"reply"`
	CostUSD      float64            `json:"cost_usd"`
}

type View struct {
	Conversation store.Conversation `json:"conversation"`
	Messages     []store.Message    `json:"messages"`
}

func New(cfg config.Config, st *store.Store) *Engine { return NewWith(cfg, st, nil) }

// NewWith lets a test supply the harness. nil means the generic CLI adapter.
func NewWith(cfg config.Config, st *store.Store, factory func(config.Participant) harness.Harness) *Engine {
	id := os.Getenv("PARLEY_IDENTITY")
	if id == "" {
		id = cfg.Identity
	}
	if id == "" {
		id = "self"
	}
	depth, _ := strconv.Atoi(os.Getenv("PARLEY_DEPTH"))
	var chain []string
	for _, c := range strings.Split(os.Getenv("PARLEY_CHAIN"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			chain = append(chain, c)
		}
	}
	return &Engine{cfg: cfg, st: st, identity: id, depth: depth, chain: chain, newHarness: factory}
}

func (e *Engine) Identity() string      { return e.identity }
func (e *Engine) Config() config.Config { return e.cfg }
func (e *Engine) Shutdown() error       { return e.st.Close() }

func (e *Engine) Participants() []map[string]string {
	names := make([]string, 0, len(e.cfg.Participants))
	for n := range e.cfg.Participants {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]map[string]string, 0, len(names))
	for _, n := range names {
		p := e.cfg.Participants[n]
		perm := "read_only"
		if !p.ReadOnly() {
			perm = "write"
		}
		h := p.Harness
		if h == "" {
			h = "custom"
		}
		out = append(out, map[string]string{
			"name": n, "harness": h, "model": p.Model, "permission": perm, "dir": p.Dir,
		})
	}
	return out
}

// Open starts a conversation. It does not contact anyone yet.
func (e *Engine) Open(topic, contextStr string) (View, error) {
	if strings.TrimSpace(topic) == "" {
		return View{}, errors.New("empty topic")
	}
	dir, _ := os.Getwd()
	now := time.Now().UTC()
	c := store.Conversation{ID: store.NewID("cnv"), Topic: topic, Context: contextStr, Status: "open", Dir: dir, CreatedAt: now, UpdatedAt: now}
	if err := e.st.CreateConversation(c); err != nil {
		return View{}, err
	}
	return e.Read(c.ID)
}

// Send delivers one message to one participant and waits for the reply. With no
// conversation id it creates an ephemeral one and closes it right after.
func (e *Engine) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return SendResult{}, errors.New("empty message")
	}
	if req.To == "" {
		return SendResult{}, errors.New("missing recipient")
	}
	if _, ok := e.cfg.Participants[req.To]; !ok {
		return SendResult{}, fmt.Errorf("unknown participant %q (available: %s)", req.To, strings.Join(e.names(), ", "))
	}
	if err := e.guard(req.To); err != nil {
		return SendResult{}, err
	}

	ephemeral := req.ConversationID == ""
	var conv store.Conversation
	var err error
	if ephemeral {
		conv, err = e.openEphemeral(msg, req.Context)
		if err != nil {
			return SendResult{}, err
		}
	} else {
		conv, err = e.st.Conversation(req.ConversationID)
		if err != nil {
			return SendResult{}, err
		}
		if conv.Status == "closed" {
			return SendResult{}, fmt.Errorf("conversation %s is closed", conv.ID)
		}
	}

	history, err := e.st.Messages(conv.ID)
	if err != nil {
		return SendResult{}, err
	}
	_ = e.st.AddMessage(store.Message{ConversationID: conv.ID, Author: e.identity, Kind: "message", Body: msg})

	in := promptInput{
		identity:     e.identity,
		name:         req.To,
		dir:          e.participantDir(req.To),
		readOnly:     e.readOnly(req.To, req.Write),
		message:      msg,
		context:      req.Context,
		history:      history,
		conversation: conv,
	}
	rep, err := e.turn(ctx, req.To, requestTimeout(req.TimeoutS, e.cfg.DefaultTimeoutS), req.Write, func(dir string, readOnly bool) string {
		in.dir = dir
		in.readOnly = readOnly
		return renderPrompt(in)
	})
	if err != nil {
		_ = e.st.AddMessage(store.Message{ConversationID: conv.ID, Author: req.To, Kind: "error", Body: err.Error()})
		if ephemeral {
			_ = e.st.SetStatus(conv.ID, "closed")
		}
		failed, _ := e.st.Conversation(conv.ID)
		return SendResult{Conversation: failed}, err
	}
	reply := store.Message{ConversationID: conv.ID, Author: req.To, Kind: "message", Body: rep.Text, CostUSD: rep.CostUSD}
	if err := e.st.AddMessage(reply); err != nil {
		return SendResult{}, err
	}
	if ephemeral {
		_ = e.st.SetStatus(conv.ID, "closed")
	}
	conv, _ = e.st.Conversation(conv.ID)
	return SendResult{Conversation: conv, Reply: reply, CostUSD: rep.CostUSD}, nil
}

// Read returns a conversation and its messages.
func (e *Engine) Read(id string) (View, error) {
	conv, err := e.st.Conversation(id)
	if err != nil {
		return View{}, err
	}
	msgs, err := e.st.Messages(id)
	if err != nil {
		return View{}, err
	}
	return View{Conversation: conv, Messages: msgs}, nil
}

// Close ends a conversation, optionally with a note (the conclusion, the next
// step, whatever the agent decided to record).
func (e *Engine) Close(id, note string) (View, error) {
	conv, err := e.st.Conversation(id)
	if err != nil {
		return View{}, err
	}
	if conv.Status == "closed" {
		return View{}, fmt.Errorf("conversation %s was already closed", id)
	}
	if n := strings.TrimSpace(note); n != "" {
		_ = e.st.AddMessage(store.Message{ConversationID: id, Author: e.identity, Kind: "note", Body: n})
	}
	if err := e.st.SetStatus(id, "closed"); err != nil {
		return View{}, err
	}
	return e.Read(id)
}

func (e *Engine) List(dir string, limit int) ([]store.Conversation, error) {
	return e.st.ListConversations(dir, limit)
}

func (e *Engine) ResetSession(participant, dir string) error {
	if _, ok := e.cfg.Participants[participant]; !ok {
		return fmt.Errorf("unknown participant: %q", participant)
	}
	return e.st.ClearSession(participant, dir)
}

// --- internals -------------------------------------------------------------

func (e *Engine) openEphemeral(message, contextStr string) (store.Conversation, error) {
	dir, _ := os.Getwd()
	now := time.Now().UTC()
	conv := store.Conversation{
		ID: store.NewID("cnv"), Topic: firstLine(message), Context: contextStr,
		Status: "open", Dir: dir, CreatedAt: now, UpdatedAt: now,
	}
	return conv, e.st.CreateConversation(conv)
}

func (e *Engine) turn(ctx context.Context, name string, timeout time.Duration, write bool, build func(dir string, readOnly bool) string) (harness.Reply, error) {
	spec, p, err := e.cfg.Spec(name)
	if err != nil {
		return harness.Reply{}, err
	}
	dir := e.participantDir(name)
	var h harness.Harness
	if e.newHarness != nil {
		h = e.newHarness(p)
	} else {
		h = harness.NewCLI(spec)
	}
	readOnly := e.readOnly(name, write)
	opts := harness.Options{Model: p.Model, Dir: dir, ReadOnly: readOnly, Env: e.childEnv(name)}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sid, ok, err := e.st.Session(name, dir)
	if err != nil {
		return harness.Reply{}, err
	}
	if ok && spec.SupportsContinuity() {
		opts.SessionID = sid
		rep, err := h.Continue(ctx, build(dir, readOnly), opts)
		if err == nil {
			return rep, nil
		}
		if !missingSession(err) {
			return rep, err
		}
	}
	opts.SessionID = newUUID()
	rep, err := h.Start(ctx, build(dir, readOnly), opts)
	if err != nil {
		return rep, err
	}
	sid = rep.SessionID
	if sid == "" && spec.Session == "arg" {
		sid = opts.SessionID
	}
	if sid != "" {
		_ = e.st.SetSession(name, dir, sid)
	}
	return rep, nil
}

func (e *Engine) participantDir(name string) string {
	dir := e.cfg.Participants[name].Dir
	if dir == "" {
		dir = "."
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func (e *Engine) readOnly(name string, callerWantsWrite bool) bool {
	return e.cfg.Participants[name].ReadOnly() || !callerWantsWrite
}

func (e *Engine) childEnv(name string) []string {
	chain := append(append([]string{}, e.chain...), name)
	return []string{
		"PARLEY_DEPTH=" + strconv.Itoa(e.depth+1),
		"PARLEY_CHAIN=" + strings.Join(chain, ","),
		"PARLEY_IDENTITY=" + name,
	}
}

func (e *Engine) guard(name string) error {
	for _, c := range e.chain {
		if c == name {
			return fmt.Errorf("%w: %s already took part in this chain (%s)", ErrLoop, name, strings.Join(append(e.chain, name), " -> "))
		}
	}
	if e.depth >= e.cfg.MaxDepth {
		return fmt.Errorf("%w: depth %d", ErrDepth, e.depth)
	}
	return nil
}

func (e *Engine) names() []string {
	out := make([]string, 0, len(e.cfg.Participants))
	for n := range e.cfg.Participants {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func requestTimeout(s, def int) time.Duration {
	if s <= 0 {
		s = def
	}
	return time.Duration(s) * time.Second
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return strings.TrimSpace(s)
}

func missingSession(err error) bool {
	s := strings.ToLower(err.Error())
	for _, needle := range []string{"no conversation found", "session not found", "unknown session", "could not find session"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
