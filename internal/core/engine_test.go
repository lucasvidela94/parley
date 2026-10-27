package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucasvidela94/parley/internal/config"
	"github.com/lucasvidela94/parley/internal/harness"
	"github.com/lucasvidela94/parley/internal/store"
)

// fake replies from a script. "ERR:<msg>" fails the turn; "BLOCK" signals
// running and waits for the context to end, like a harness mid-turn.
type fake struct {
	name    string
	mu      sync.Mutex
	script  []string
	prompts []string
	opts    []harness.Options
	calls   int
	started int
	cont    int
	running chan struct{}
}

func (f *fake) Name() string       { return f.name }
func (f *fake) Spec() harness.Spec { return harness.Spec{Bin: f.name} }

func (f *fake) reply(ctx context.Context, prompt string, o harness.Options, cont bool) (harness.Reply, error) {
	f.mu.Lock()
	if cont {
		f.cont++
	} else {
		f.started++
	}
	f.prompts = append(f.prompts, prompt)
	f.opts = append(f.opts, o)
	i := f.calls
	f.calls++
	out := "ok"
	if i < len(f.script) {
		out = f.script[i]
	}
	f.mu.Unlock()
	switch {
	case out == "BLOCK":
		close(f.running)
		<-ctx.Done()
		return harness.Reply{}, context.Cause(ctx)
	case strings.HasPrefix(out, "ERR:"):
		return harness.Reply{}, errors.New(strings.TrimPrefix(out, "ERR:"))
	}
	return harness.Reply{Text: out}, nil
}

func (f *fake) Start(ctx context.Context, prompt string, o harness.Options) (harness.Reply, error) {
	return f.reply(ctx, prompt, o, false)
}

func (f *fake) Continue(ctx context.Context, prompt string, o harness.Options) (harness.Reply, error) {
	return f.reply(ctx, prompt, o, true)
}

func (f *fake) lastOpts() harness.Options {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opts[len(f.opts)-1]
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
		byHarness[name] = &fake{name: name, script: script, running: make(chan struct{})}
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
	view, err := env.e.Open(context.Background(), "choose a cache", "repo at /tmp/x", 0)
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

func TestSendDoesNotSpendATurnWhenTheMessageCannotBeRecorded(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ok"}})
	db, err := sql.Open("sqlite", "file:"+env.e.cfg.StorePath()+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER reject_message BEFORE INSERT ON messages
BEGIN SELECT RAISE(ABORT, 'store is read-only'); END;`); err != nil {
		t.Fatal(err)
	}

	if _, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "q"}); err == nil || !strings.Contains(err.Error(), "store is read-only") {
		t.Fatalf("expected the store error to survive, got %v", err)
	}
	if calls := env.fakes["claude"].calls; calls != 0 {
		t.Fatalf("the harness ran %d time(s); a message that cannot be stored must not cost a turn", calls)
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
	if !env.fakes["claude"].lastOpts().ReadOnly {
		t.Fatal("the harness must be told to run read-only, not only the prompt")
	}
}

func TestTwoModelsOfOneHarnessShareAConversation(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"opencode": {"from deepseek", "from kimi"}})
	env.e.cfg.Participants["deepseek"] = config.Participant{Harness: "opencode", Model: "deepseek/v4"}
	env.e.cfg.Participants["kimi"] = config.Participant{Harness: "opencode", Model: "opencode-go/kimi-k3", Args: []string{"--variant", "high"}}
	ctx := context.Background()
	view, err := env.e.Open(ctx, "spec", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID

	if _, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "deepseek", Message: "position?"}); err != nil {
		t.Fatal(err)
	}
	if o := env.fakes["opencode"].lastOpts(); o.Model != "deepseek/v4" {
		t.Fatalf("deepseek ran with model %q", o.Model)
	}
	if _, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "kimi", Message: "and you?"}); err != nil {
		t.Fatal(err)
	}
	if o := env.fakes["opencode"].lastOpts(); o.Model != "opencode-go/kimi-k3" || strings.Join(o.Args, " ") != "--variant high" {
		t.Fatalf("kimi ran with %+v", o)
	}

	view, err = env.e.Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	md := view.Markdown()
	for _, want := range []string{"### deepseek · deepseek/v4", "### kimi · opencode-go/kimi-k3", "### opencode", "from kimi"} {
		if !strings.Contains(md, want) {
			t.Fatalf("transcript lacks %q:\n%s", want, md)
		}
	}
	if !strings.Contains(md, "Participants: opencode, deepseek (deepseek/v4), kimi (opencode-go/kimi-k3)") {
		t.Fatalf("header does not list who took part:\n%s", md)
	}
}

func TestMaxTurnsRefusesTheNextTurn(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"one", "two", "three"}})
	ctx := context.Background()
	view, err := env.e.Open(ctx, "budget", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID
	for range 2 {
		if _, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q3"})
	if !errors.Is(err, ErrTurnLimit) {
		t.Fatalf("expected ErrTurnLimit, got %v", err)
	}
	if calls := env.fakes["claude"].calls; calls != 2 {
		t.Fatalf("the harness ran %d times", calls)
	}
	view, err = env.e.Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 4 {
		t.Fatalf("a refused turn must leave no message behind: %d messages", view.Total)
	}
	if !strings.Contains(view.Markdown(), "turns: 2/2") {
		t.Fatalf("read should show the turns spent:\n%s", view.Markdown())
	}
}

func TestCancelStopsARunningTurn(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"BLOCK"}})
	env.e.pollEvery = time.Millisecond
	ctx := context.Background()
	view, err := env.e.Open(ctx, "slow", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID
	if _, err := env.e.Cancel(ctx, id); err == nil {
		t.Fatal("cancel with nothing running must say so")
	}

	done := make(chan error, 1)
	go func() {
		_, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q"})
		done <- err
	}()
	<-env.fakes["claude"].running
	turns, err := env.e.Cancel(ctx, id)
	if err != nil || len(turns) != 1 || turns[0].Participant != "claude" {
		t.Fatalf("cancel: %+v %v", turns, err)
	}
	if err := <-done; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
	view, err = env.e.Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if last := view.Messages[len(view.Messages)-1]; last.Kind != "error" {
		t.Fatalf("a cancelled turn must be recorded as an error, got %+v", last)
	}
	if _, err := env.e.Cancel(ctx, id); err == nil {
		t.Fatal("the finished turn is still registered as running")
	}
}

func TestCloseSummaryConfirmsTheClose(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"a long peer reply"}})
	ctx := context.Background()
	view, err := env.e.Open(ctx, "t", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID
	if _, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q"}); err != nil {
		t.Fatal(err)
	}
	view, err = env.e.Close(ctx, id, "decided X")
	if err != nil {
		t.Fatal(err)
	}
	got := view.CloseSummary()
	if !strings.HasPrefix(got, "closed "+id) || !strings.Contains(got, "note: decided X") || strings.Contains(got, "peer reply") {
		t.Fatalf("summary: %q", got)
	}
	list, err := env.e.List(ctx, "", 10)
	if err != nil || list[0].Status != "closed" {
		t.Fatalf("list disagrees with close: %+v %v", list, err)
	}
	if _, err := env.e.Close(ctx, id, ""); err == nil {
		t.Fatal("closing twice must fail")
	}
}

func TestPage(t *testing.T) {
	v := View{Total: 5}
	for _, b := range []string{"m0", "m1", "m2", "m3", "m4"} {
		v.Messages = append(v.Messages, store.Message{Body: b, Kind: "message"})
	}
	cases := []struct {
		offset, limit, tail int
		want                string
		wantOffset          int
	}{
		{0, 0, 0, "m0 m1 m2 m3 m4", 0},
		{0, 0, 2, "m3 m4", 3},
		{1, 2, 0, "m1 m2", 1},
		{4, 10, 0, "m4", 4},
		{9, 0, 0, "", 5},
		{0, 0, 9, "m0 m1 m2 m3 m4", 0},
		{0, 1, 3, "m2", 2},
	}
	for _, c := range cases {
		p, err := v.Page(c.offset, c.limit, c.tail)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range p.Messages {
			got = append(got, m.Body)
		}
		if strings.Join(got, " ") != c.want || p.Offset != c.wantOffset {
			t.Fatalf("%+v: got %q at %d", c, got, p.Offset)
		}
	}
	if _, err := v.Page(1, 0, 1); err == nil {
		t.Fatal("tail with offset must be rejected")
	}
	past, _ := v.Page(9, 0, 0)
	if md := past.Markdown(); !strings.Contains(md, "no messages in this range (5 in total)") {
		t.Fatalf("past the end:\n%s", md)
	}
}

func TestParticipantsSayWhenReadOnlyIsNotEnforced(t *testing.T) {
	env := newTestEngine(t, map[string][]string{})
	env.e.cfg.Participants["custom"] = config.Participant{Spec: &harness.Spec{Bin: "mytool", Start: []string{"run"}}}
	for _, p := range env.e.Participants() {
		enforced := !strings.Contains(p["permission"], "not enforced")
		if want := p["name"] != "custom"; enforced != want {
			t.Fatalf("%s: permission %q", p["name"], p["permission"])
		}
	}
}

func TestReloadPicksUpAnEditedConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PARLEY_CONFIG", path)
	write := func(body string, at time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	write(`{"store":"`+filepath.Join(t.TempDir(), "p.db")+`","participants":{"claude":{"harness":"claude"}}}`, now)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.StorePath())
	if err != nil {
		t.Fatal(err)
	}
	e := New(cfg, st)
	t.Cleanup(func() { e.Shutdown() })

	if err := e.Reload(); err != nil || len(e.Participants()) != 1 {
		t.Fatalf("untouched config: %v %v", err, e.Participants())
	}

	write(`{"participants":{"claude":{"harness":"claude"},"cursor-opus":{"harness":"cursor","model":"claude-opus-5-5-high"}}}`, now.Add(time.Hour))
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := e.Participants(); len(got) != 2 || got[1]["name"] != "cursor-opus" || got[1]["model"] != "claude-opus-5-5-high" {
		t.Fatalf("the edit was not picked up: %v", got)
	}

	write(`{ not json`, now.Add(2*time.Hour))
	if err := e.Reload(); err == nil {
		t.Fatal("a broken edit must be reported")
	}
	if len(e.Participants()) != 2 {
		t.Fatal("a broken edit must leave the last good config in place")
	}
}

func TestFailedTurnsAreCountedApartFromReplies(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ERR:boom", "fine"}})
	ctx := context.Background()
	view, err := env.e.Open(ctx, "flaky", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID
	if _, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q1"}); err == nil {
		t.Fatal("expected the harness error")
	}
	view, err = env.e.Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Failed != 1 {
		t.Fatalf("failed turns = %d", view.Failed)
	}
	if !strings.Contains(view.Markdown(), "turns: 1/3 (1 failed)") {
		t.Fatalf("the header must say a spent turn did not produce a reply:\n%s", view.Markdown())
	}
	if _, err := env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q2"}); err != nil {
		t.Fatal(err)
	}
	view, _ = env.e.Read(ctx, id)
	if !strings.Contains(view.Markdown(), "turns: 2/3 (1 failed)") {
		t.Fatalf("%s", view.Markdown())
	}
}

func TestErrorKeepsTheHarnessRemedyInTheTranscript(t *testing.T) {
	remedy := "Pass --trust, --yolo, or -f if you trust this directory"
	long := "Workspace Trust Required " + strings.Repeat("Cursor Agent can execute code. ", 12) + remedy
	env := newTestEngine(t, map[string][]string{"claude": {"ERR:" + long}})
	ctx := context.Background()
	view, err := env.e.Open(ctx, "trust", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Conversation.ID
	_, _ = env.e.Send(ctx, SendRequest{ConversationID: id, To: "claude", Message: "q"})
	view, _ = env.e.Read(ctx, id)
	if !strings.Contains(view.Markdown(), remedy) {
		t.Fatalf("the error was cut before the remedy:\n%s", view.Markdown())
	}
}

func TestPromptAsksToReadBeforeClaimingAndToListWhatWasNotRead(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"ok"}})
	if _, err := env.e.Send(context.Background(), SendRequest{To: "claude", Message: "review x"}); err != nil {
		t.Fatal(err)
	}
	p := env.fakes["claude"].lastPrompt()
	for _, want := range []string{"read it before", "files you did not read"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not say %q:\n%s", want, p)
		}
	}
}

func TestParticipantsSayBothSwitchesAreNeededToWrite(t *testing.T) {
	env := newTestEngine(t, nil)
	env.e.cfg.Participants["editor"] = config.Participant{Harness: "claude", Permission: "write"}
	perm := map[string]string{}
	for _, p := range env.e.Participants() {
		perm[p["name"]] = p["permission"]
	}
	if !strings.Contains(perm["claude"], `permission: "write"`) {
		t.Errorf("a read_only participant must say how to allow edits: %q", perm["claude"])
	}
	if !strings.Contains(perm["editor"], "write=true") {
		t.Errorf("a write participant must say the call needs write=true too: %q", perm["editor"])
	}
}

func TestSendSaysWhenWriteWasIgnored(t *testing.T) {
	env := newTestEngine(t, map[string][]string{"claude": {"a", "b"}})
	env.e.cfg.Participants["editor"] = config.Participant{Harness: "claude", Permission: "write"}
	ctx := context.Background()

	res, err := env.e.Send(ctx, SendRequest{To: "claude", Message: "edit it", Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Notice, `"permission": "write"`) {
		t.Fatalf("write=true on a read_only participant must be reported: %q", res.Notice)
	}

	res, err = env.e.Send(ctx, SendRequest{To: "editor", Message: "edit it", Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Notice != "" {
		t.Fatalf("nothing was ignored: %q", res.Notice)
	}
	res, _ = env.e.Send(ctx, SendRequest{To: "claude", Message: "just read"})
	if res.Notice != "" {
		t.Fatalf("no write was asked for: %q", res.Notice)
	}
}

func TestModelsAsksTheHarnessForItsOwnList(t *testing.T) {
	env := newTestEngine(t, nil)
	env.e.cfg.Participants["lister"] = config.Participant{Spec: &harness.Spec{Bin: "sh", ModelsArgs: []string{"-c", "printf 'm1\nm2'"}}}
	env.e.cfg.Participants["mute"] = config.Participant{Spec: &harness.Spec{Bin: "sh"}}
	out, err := env.e.Models(context.Background(), "lister")
	if err != nil || out != "m1\nm2" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := env.e.Models(context.Background(), "mute"); err == nil || !strings.Contains(err.Error(), "cannot list") {
		t.Fatalf("a harness with no listing command must say so: %v", err)
	}
	if _, err := env.e.Models(context.Background(), "ghost"); err == nil {
		t.Fatal("unknown participant")
	}
}
