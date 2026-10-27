package harness

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	cases := []struct {
		name    string
		harness string
		cont    bool
		o       Options
		want    []string
	}{
		{
			name: "claude read-only with a model", harness: "claude",
			o: Options{SessionID: "uuid-1", Model: "opus", ReadOnly: true},
			want: []string{"-p", "--session-id", "uuid-1", "--output-format", "json",
				"--permission-mode", "plan", "--disallowedTools", "Write,Edit,MultiEdit,NotebookEdit",
				"--model", "opus"},
		},
		{
			name: "claude with write and no model", harness: "claude",
			o:    Options{SessionID: "uuid-1"},
			want: []string{"-p", "--session-id", "uuid-1", "--output-format", "json"},
		},
		{
			name: "opencode continue: flags before the prompt", harness: "opencode", cont: true,
			o: Options{SessionID: "ses_1", Model: "opencode-go/kimi-k3", ReadOnly: true, Args: []string{"--variant", "high"}},
			want: []string{"run", "--session", "ses_1", "--format", "json",
				"--agent", "plan", "--model", "opencode-go/kimi-k3", "--variant", "high", "hola"},
		},
		{
			name: "codex resume read-only uses the config override", harness: "codex", cont: true,
			o: Options{SessionID: "th_1", ReadOnly: true},
			want: []string{"exec", "resume", "th_1", "--json", "--skip-git-repo-check",
				"-c", `sandbox_mode="read-only"`},
		},
	}
	for _, c := range cases {
		s, _ := Known(c.harness)
		got := s.Render(c.cont, "hola", c.o)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Fatalf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestParseHermesStreamJSON(t *testing.T) {
	s, _ := Known("hermes")
	// Captured from hermes 0.21.4 with `parley probe`; init carries an empty
	// session id, the result event the real one.
	ok := `{"type": "system", "subtype": "init", "model": "", "session_id": "", "timestamp": 1}
{"type": "text", "text": "ok", "timestamp": 2}
{"type": "result", "session_id": "local-2a4a", "exit_code": 0, "text": "ok", "tokens": {"input": 14394}, "timestamp": 3}`
	rep, err := s.Parse(ok)
	if err != nil || rep.Text != "ok" || rep.SessionID != "local-2a4a" {
		t.Fatalf("%+v %v", rep, err)
	}
	failed := `{"type": "result", "session_id": "local-1", "exit_code": 1, "text": "", "error": "HTTP 400: Model is unavailable."}`
	if _, err := s.Parse(failed); err == nil || !strings.Contains(err.Error(), "Model is unavailable") {
		t.Fatalf("the error event must surface: %v", err)
	}
	got := s.Render(true, "hola", Options{SessionID: "local-1", ReadOnly: true, Model: "kimi"})
	want := []string{"chat", "-Q", "--format", "stream-json", "--resume", "local-1", "-q", "hola", "-t", "safe", "-m", "kimi"}
	if !slices.Equal(got, want) {
		t.Fatalf("render:\ngot  %q\nwant %q", got, want)
	}
}

func TestOmpTextModeWithoutContinuity(t *testing.T) {
	s, _ := Known("omp")
	if s.SupportsContinuity() {
		t.Fatal("omp has no session id in text mode")
	}
	got := s.Render(false, "hola", Options{ReadOnly: true})
	if want := []string{"-p", "--no-tools", "hola"}; !slices.Equal(got, want) {
		t.Fatalf("render: %q", got)
	}
}

func TestEveryBuiltinEnforcesReadOnly(t *testing.T) {
	for _, n := range Names() {
		s, _ := Known(n)
		if !s.EnforcesReadOnly() {
			t.Fatalf("%s has no read-only flags", n)
		}
	}
}

func TestPiSpecReadsTextOutput(t *testing.T) {
	s, _ := Known("pi")
	// pi's json mode hides the answer in message.content[] (an array) and
	// reports errors inside the stream with exit 0; text mode is what works.
	rep, err := s.Parse("ok\n")
	if err != nil || rep.Text != "ok" {
		t.Fatalf("text output: %+v %v", rep, err)
	}
	if !s.SupportsContinuity() {
		t.Fatal("pi should continue by session id")
	}
}

func TestParseClaudeJSON(t *testing.T) {
	s, _ := Known("claude")
	rep, err := s.Parse(`{"session_id":"abc","result":"ok","is_error":false,"total_cost_usd":0.12}`)
	if err != nil {
		t.Fatal(err)
	}
	// claude's session id comes from our own {session} arg, not from the reply.
	if rep.Text != "ok" || rep.CostUSD != 0.12 {
		t.Fatalf("%+v", rep)
	}
	if _, err := s.Parse(`{"is_error":true,"result":""}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseOpenCodeJSONL(t *testing.T) {
	s, _ := Known("opencode")
	raw := `{"type":"step_start","sessionID":"ses_9"}
{"type":"text","sessionID":"ses_9","part":{"type":"text","text":"ok"}}
{"type":"tool","sessionID":"ses_9","part":{"type":"tool","text":"ignorar"}}`
	rep, err := s.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Text != "ok" || rep.SessionID != "ses_9" {
		t.Fatalf("%+v", rep)
	}
}

func TestParseJSONLErrorEvent(t *testing.T) {
	s, _ := Known("opencode")
	if _, err := s.Parse(`{"type":"error","sessionID":"x","error":{"message":"boom"}}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFilteredJSONL(t *testing.T) {
	spec := Spec{
		Output: "jsonl", TextPath: "item.text",
		TextFilterPath: "item.type", TextFilterEquals: "agent_message",
		Session: "output:thread_id",
	}
	raw := `{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"type":"reasoning","text":"descartar"}}
{"type":"item.completed","item":{"type":"agent_message","text":"hola"}}`
	rep, err := spec.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Text != "hola" || rep.SessionID != "t1" {
		t.Fatalf("%+v", rep)
	}
}

func TestCLIStartsCommand(t *testing.T) {
	// /bin/sh as harness: stdout is the answer, text output mode.
	spec := Spec{Bin: "sh", Start: []string{"-c", "printf hola"}, Prompt: "stdin", Output: "text"}
	rep, err := NewCLI(spec).Start(context.Background(), "ignorado", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Text != "hola" {
		t.Fatalf("%q", rep.Text)
	}
}

func TestCLICommandFailureSurfacesStderr(t *testing.T) {
	spec := Spec{Bin: "sh", Start: []string{"-c", "echo fallo >&2; exit 3"}, Output: "text"}
	if _, err := NewCLI(spec).Start(context.Background(), "", Options{}); err == nil {
		t.Fatal("expected error")
	}
}
