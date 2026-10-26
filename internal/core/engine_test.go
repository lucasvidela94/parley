package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lucasvidela94/parley/internal/config"
	"github.com/lucasvidela94/parley/internal/harness"
	"github.com/lucasvidela94/parley/internal/store"
)

type fake struct {
	name    string
	mu      sync.Mutex
	script  []string
	prompts []string
	calls   int
	started int
	cont    int
}

func (f *fake) Name() string       { return f.name }
func (f *fake) Spec() harness.Spec { return harness.Spec{Bin: f.name} }

func (f *fake) reply(prompt string, cont bool) (harness.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cont {
		f.cont++
	} else {
		f.started++
	}
	f.prompts = append(f.prompts, prompt)
	i := f.calls
	f.calls++
	if i >= len(f.script) {
		return harness.Reply{Text: "ok"}, nil
	}
	out := f.script[i]
	if strings.HasPrefix(out, "ERR:") {
		return harness.Reply{}, errors.New(strings.TrimPrefix(out, "ERR:"))
	}
	return harness.Reply{Text: out}, nil
}

func (f *fake) Start(ctx context.Context, prompt string, o harness.Options) (harness.Reply, error) {
	return f.reply(prompt, false)
}

func (f *fake) Continue(ctx context.Context, prompt string, o harness.Options) (harness.Reply, error) {
	return f.reply(prompt, true)
}

func (f *fake) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.prompts) == 0 {
		return ""
	}
	return f.prompts[len(f.prompts)-1]
}

type testEnv struct {
	e       *Engine
	fakes   map[string]*fake
	scripts map[string][]string
}

func newTestEngine(t *testing.T, scripts map[string][]string) *testEnv {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("PARLEY_IDENTITY", "opencode")
	t.Setenv("PARLEY_CHAIN", "")
	t.Setenv("PARLEY_DEPTH", "")

	cfg := config.Default()
	cfg.Participants = map[string]config.Participant{
		"claude":   {Harness: "claude"},
		"opencode": {Harness: "opencode"},
	}
	cfg.Store = filepath.Join(t.TempDir(), "p.db")
	cfg.DefaultTimeoutS = 30
	st, err := store.Open(cfg.Store)
	if err != nil {
		t.Fatal(err)
	}
	byHarness := map[string]*fake{}
	for name, script := range scripts {
		byHarness[name] = &fake{name: name, script: script}
	}
	e := NewWith(cfg, st, func(p config.Participant) harness.Harness {
		return byHarness[p.Harness]
	})
	t.Cleanup(func() { e.Shutdown() })
	return &testEnv{e: e, fakes: byHarness, scripts: scripts}
}

func TestSendEphemeral(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"hi"}})
	res, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "are you there?"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply.Body != "hi" {
		t.Fatalf("%q", res.Reply.Body)
	}
	if res.Conversation.Status != "closed" {
		t.Fatalf("ephemeral should be closed: %s", res.Conversation.Status)
	}
	view, err := env.e.Read(context.Background(), res.Conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Messages) != 2 {
		t.Fatalf("messages: %d", len(view.Messages))
	}
}

func TestConversationFlow(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"first", "second"}})
	view, err := env.e.Open(context.Background(), "choose a cache", "repo at /tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID

	if _, err := env.e.Send(context.Background(), SendRequest{ConversationID: id, To: "claude", Message: "Redis or in-memory?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.e.Send(context.Background(), SendRequest{ConversationID: id, To: "claude", Message: "and at 10k rps?"}); err != nil {
		t.Fatal(err)
	}
	// the second turn must see the first one
	if p := env.fakes["claude"].lastPrompt(); !strings.Contains(p, "Redis or in-memory?") || !strings.Contains(p, "first") {
		t.Fatalf("recipient did not see the history:\n%s", p)
	}
	view, err = env.e.Close(context.Background(), id, "keep memory, benchmark pending")
	if err != nil {
		t.Fatal(err)
	}
	if view.Conversation.Status != "closed" {
		t.Fatalf("estado: %s", view.Conversation.Status)
	}
	if _, err := env.e.Send(context.Background(), SendRequest{ConversationID: id, To: "claude", Message: "again"}); err == nil {
		t.Fatal("must not accept messages after closing")
	}
}

func TestSendErrorIsRecordedAndReturned(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ERR:timeout"}})
	res, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "q"})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, rerr := env.e.Read(context.Background(), res.Conversation.ID); rerr != nil {
		t.Fatal(rerr)
	}
}

func TestLoopGuard(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ok"}})
	env.e.chain = []string{"opencode", "claude"}
	_, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "q"})
	if !errors.Is(err, ErrLoop) {
		t.Fatalf("expected ErrLoop: %v", err)
	}
}

func TestDepthGuard(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ok"}})
	env.e.depth = env.e.cfg.MaxDepth
	_, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "q"})
	if !errors.Is(err, ErrDepth) {
		t.Fatalf("expected ErrDepth: %v", err)
	}
}

func TestSendRetriesFreshSession(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ERR:No conversation found with session ID abc", "fresh answer"}})
	dir, _ := os.Getwd()
	env.e.cfg.Participants["claude"] = config.Participant{Harness: "claude", Dir: dir}
	if err := env.e.st.SetSession(context.Background(), "claude", dir, "abc"); err != nil {
		t.Fatal(err)
	}
	res, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply.Body != "fresh answer" {
		t.Fatalf("%q", res.Reply.Body)
	}
	if f := env.fakes["claude"]; f.cont != 1 || f.started != 1 {
		t.Fatalf("cont=%d started=%d", f.cont, f.started)
	}
	sid, _, _ := env.e.st.Session(context.Background(), "claude", dir)
	if sid == "abc" {
		t.Fatal("did not store the new session")
	}
}

func TestParticipantsExposeHarness(t *testing.T) {
	env := newTestEngine(t, map[string][]string{})
	for _, p := range env.e.Participants() {
		if p["harness"] == "" || p["name"] == "" {
			t.Fatalf("incomplete participant: %+v", p)
		}
	}
}

func TestUnknownParticipant(t *testing.T) {
	env := newTestEngine(t, map[string][]string{})
	if _, err := env.e.Send(context.Background(), SendRequest{To: "nadie", Message: "q"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadOnlyDefault(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ok"}})
	if _, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "q"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.fakes["claude"].lastPrompt(), "read-only") {
		t.Fatal("the prompt should declare read-only")
	}
}
