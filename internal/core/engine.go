package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasvidela94/parley/internal/config"
	"github.com/lucasvidela94/parley/internal/harness"
	"github.com/lucasvidela94/parley/internal/store"
)

var (
	ErrLoop      = errors.New("agent cycle")
	ErrDepth     = errors.New("maximum depth reached")
	ErrTurnLimit = errors.New("turn limit reached")
	// ErrCancelled is the cause a turn carries when `parley cancel` stopped it.
	ErrCancelled = fmt.Errorf("%w by parley cancel", context.Canceled)
)

// cancelPoll is how often a running turn checks the store for a cancel request.
const cancelPoll = time.Second

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
	pollEvery  time.Duration
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

// View is a conversation and a window of its messages: Messages starts at
// index Offset of Total.
type View struct {
	Conversation store.Conversation `json:"conversation"`
	Messages     []store.Message    `json:"messages"`
	Offset       int                `json:"offset"`
	Total        int                `json:"total"`
	Failed       int                `json:"failed_turns,omitempty"` // whole conversation: turns spent that ended in an error
	CostUSD      float64            `json:"cost_usd,omitempty"`     // whole conversation, when the harness reports it
	Authors      []Author           `json:"participants"`           // whole conversation, in order of first turn
}

// Author is one voice in a transcript and the models it spoke with.
type Author struct {
	Name   string   `json:"name"`
	Models []string `json:"models,omitempty"`
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
	return &Engine{cfg: cfg, st: st, identity: id, depth: depth, chain: chain, newHarness: factory, pollEvery: cancelPoll}
}

func (e *Engine) Identity() string      { return e.identity }
func (e *Engine) Config() config.Config { return e.cfg }
func (e *Engine) Shutdown() error       { return e.st.Close() }

// Reload re-reads the config when its file was edited since it was loaded, so
// a long-lived MCP server does not keep serving a stale participant list. A
// broken edit is returned and the last good config stays in place. Identity and
// the store are fixed when the process starts and are not reloaded.
func (e *Engine) Reload() error {
	if !e.cfg.Changed() {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config changed but cannot be read, keeping the previous one: %w", err)
	}
	e.cfg = cfg
	return nil
}

// Models returns what the participant's harness prints when asked for its
// models, untouched: parley does not interpret model ids.
func (e *Engine) Models(ctx context.Context, participant string) (string, error) {
	spec, _, err := e.cfg.Spec(participant)
	if err != nil {
		return "", err
	}
	return harness.ListModels(ctx, spec, e.participantDir(participant))
}

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
		} else if s, _, err := e.cfg.Spec(n); err == nil && !s.EnforcesReadOnly() {
			// Say so instead of pretending: without read-only flags the only
			// restriction is a line in the prompt.
			perm = "read_only (not enforced: the spec has no read_only_args)"
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

// Open starts a conversation. It does not contact anyone yet. maxTurns > 0
// caps how many sends the conversation accepts.
func (e *Engine) Open(ctx context.Context, topic, contextStr string, maxTurns int) (View, error) {
	if strings.TrimSpace(topic) == "" {
		return View{}, errors.New("empty topic")
	}
	if maxTurns < 0 {
		return View{}, errors.New("max turns cannot be negative")
	}
	dir, _ := os.Getwd()
	now := time.Now().UTC()
	c := store.Conversation{ID: store.NewID("cnv"), Topic: topic, Context: contextStr, Status: "open", Dir: dir, MaxTurns: maxTurns, CreatedAt: now, UpdatedAt: now}
	if err := e.st.CreateConversation(ctx, c); err != nil {
		return View{}, err
	}
	return e.Read(ctx, c.ID)
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
		conv, err = e.openEphemeral(ctx, msg, req.Context)
		if err != nil {
			return SendResult{}, err
		}
	} else {
		conv, err = e.st.Conversation(ctx, req.ConversationID)
		if err != nil {
			return SendResult{}, err
		}
		if conv.Status == "closed" {
			return SendResult{}, fmt.Errorf("conversation %s is closed", conv.ID)
		}
	}

	history, err := e.st.Messages(ctx, conv.ID)
	if err != nil {
		return SendResult{}, err
	}
	// The limit is checked before anything is recorded, so a refused turn
	// leaves no question without an answer behind it.
	ok, err := e.st.SpendTurn(ctx, conv.ID)
	if err != nil {
		return SendResult{}, err
	}
	if !ok {
		return SendResult{Conversation: conv}, fmt.Errorf("%w: conversation %s allows %d turns", ErrTurnLimit, conv.ID, conv.MaxTurns)
	}
	// Record the question before calling the harness: a message that cannot be
	// stored leaves no transcript, so the exchange must not happen at all. The
	// turn already counted stays counted; failing closed is the safe side of a
	// budget.
	if err := e.st.AddMessage(ctx, store.Message{ConversationID: conv.ID, Author: e.identity, Kind: "message", Body: msg}); err != nil {
		return SendResult{}, fmt.Errorf("record message: %w", err)
	}
	model := e.cfg.Participants[req.To].Model

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
	rep, err := e.cancellable(ctx, conv.ID, req.To, func(ctx context.Context) (harness.Reply, error) {
		return e.turn(ctx, req.To, requestTimeout(req.TimeoutS, e.cfg.DefaultTimeoutS), req.Write, func(dir string, readOnly bool) string {
			in.dir = dir
			in.readOnly = readOnly
			return renderPrompt(in)
		})
	})
	if err != nil {
		// Recorded with a fresh context: a cancelled turn still owes the
		// transcript its error.
		recordErr := e.st.AddMessage(context.WithoutCancel(ctx), store.Message{ConversationID: conv.ID, Author: req.To, Kind: "error", Model: model, Body: err.Error()})
		if ephemeral {
			recordErr = errors.Join(recordErr, e.st.SetStatus(context.WithoutCancel(ctx), conv.ID, "closed"))
		}
		failed, _ := e.st.Conversation(context.WithoutCancel(ctx), conv.ID)
		// Both the turn failure and a failure to record it matter; one must not
		// mask the other.
		return SendResult{Conversation: failed}, errors.Join(err, recordErr)
	}
	reply := store.Message{ConversationID: conv.ID, Author: req.To, Kind: "message", Model: model, Body: rep.Text, CostUSD: rep.CostUSD}
	// The answer is paid for: a signal that lands after it arrives must not
	// keep it out of the transcript.
	ctx = context.WithoutCancel(ctx)
	if err := e.st.AddMessage(ctx, reply); err != nil {
		return SendResult{}, err
	}
	if ephemeral {
		// The reply is already persisted and returned; failing to flip the
		// status would hide a real answer, so this stays best-effort.
		_ = e.st.SetStatus(ctx, conv.ID, "closed")
	}
	conv, _ = e.st.Conversation(ctx, conv.ID)
	return SendResult{Conversation: conv, Reply: reply, CostUSD: rep.CostUSD}, nil
}

// Read returns a conversation and its messages.
func (e *Engine) Read(ctx context.Context, id string) (View, error) {
	conv, err := e.st.Conversation(ctx, id)
	if err != nil {
		return View{}, err
	}
	msgs, err := e.st.Messages(ctx, id)
	if err != nil {
		return View{}, err
	}
	v := View{Conversation: conv, Messages: msgs, Total: len(msgs)}
	seen := map[string]int{}
	for j := range msgs {
		m := &msgs[j]
		v.CostUSD += m.CostUSD
		if m.Kind == "error" {
			v.Failed++
		}
		i, ok := seen[m.Author]
		if !ok {
			i = len(v.Authors)
			seen[m.Author] = i
			v.Authors = append(v.Authors, Author{Name: m.Author})
		}
		if m.Model != "" && !slices.Contains(v.Authors[i].Models, m.Model) {
			v.Authors[i].Models = append(v.Authors[i].Models, m.Model)
		}
	}
	return v, nil
}

// Close ends a conversation, optionally with a note (the conclusion, the next
// step, whatever the agent decided to record).
func (e *Engine) Close(ctx context.Context, id, note string) (View, error) {
	conv, err := e.st.Conversation(ctx, id)
	if err != nil {
		return View{}, err
	}
	if conv.Status == "closed" {
		return View{}, fmt.Errorf("conversation %s was already closed", id)
	}
	if n := strings.TrimSpace(note); n != "" {
		if err := e.st.AddMessage(ctx, store.Message{ConversationID: id, Author: e.identity, Kind: "note", Body: n}); err != nil {
			return View{}, err
		}
	}
	if err := e.st.SetStatus(ctx, id, "closed"); err != nil {
		return View{}, err
	}
	return e.Read(ctx, id)
}

// Cancel asks every turn running in the conversation to stop. The process that
// owns each turn notices within a poll interval and kills the harness. It
// cannot recall a request the provider already received.
func (e *Engine) Cancel(ctx context.Context, id string) ([]store.RunningTurn, error) {
	if _, err := e.st.Conversation(ctx, id); err != nil {
		return nil, err
	}
	// A live turn heartbeats every poll; five missed polls means its process died.
	turns, err := e.st.RequestCancel(ctx, id, time.Now().Add(-5*e.pollEvery))
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, fmt.Errorf("conversation %s has no turn in progress", id)
	}
	return turns, nil
}

func (e *Engine) List(ctx context.Context, dir string, limit int) ([]store.Conversation, error) {
	return e.st.ListConversations(ctx, dir, limit)
}

func (e *Engine) ResetSession(ctx context.Context, participant, dir string) error {
	if _, ok := e.cfg.Participants[participant]; !ok {
		return fmt.Errorf("unknown participant: %q", participant)
	}
	return e.st.ClearSession(ctx, participant, dir)
}

// --- internals -------------------------------------------------------------

func (e *Engine) openEphemeral(ctx context.Context, message, contextStr string) (store.Conversation, error) {
	dir, _ := os.Getwd()
	now := time.Now().UTC()
	conv := store.Conversation{
		ID: store.NewID("cnv"), Topic: firstLine(message), Context: contextStr,
		Status: "open", Dir: dir, CreatedAt: now, UpdatedAt: now,
	}
	return conv, e.st.CreateConversation(ctx, conv)
}

// cancellable registers the turn so `parley cancel` can find it, and runs fn
// with a context that a cancel request ends.
func (e *Engine) cancellable(ctx context.Context, convID, name string, fn func(context.Context) (harness.Reply, error)) (harness.Reply, error) {
	turnID, err := e.st.StartTurn(ctx, convID, name)
	if err != nil {
		return harness.Reply{}, err
	}
	defer func() { _ = e.st.EndTurn(context.WithoutCancel(ctx), turnID) }()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(e.pollEvery)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if stop, _ := e.st.TurnCancelled(ctx, turnID); stop {
					cancel(ErrCancelled)
					return
				}
			}
		}
	}()
	return fn(ctx)
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
	opts := harness.Options{Model: p.Model, Dir: dir, ReadOnly: readOnly, Env: e.childEnv(name), Args: p.Args}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sid, ok, err := e.st.Session(ctx, name, dir)
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
		_ = e.st.SetSession(ctx, name, dir, sid)
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
