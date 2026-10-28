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

func TestSupportsContinuity(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want bool
	}{
		{"session by id and a continue argv", Spec{Session: "arg", Continue: []string{"--resume", "{session}"}}, true},
		{"session read from the reply", Spec{Session: "output:thread_id", Continue: []string{"resume"}}, true},
		{"no session", Spec{Session: "none", Continue: []string{"resume"}}, false},
		{"session unset", Spec{Continue: []string{"resume"}}, false},
		{"no continue argv", Spec{Session: "arg"}, false},
	}
	for _, c := range cases {
		if got := c.spec.SupportsContinuity(); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
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

func TestParseTextOutput(t *testing.T) {
	s := Spec{Output: "text"}
	rep, err := s.Parse("  ok\n\n")
	if err != nil || rep.Text != "ok" {
		t.Fatalf("text output is the trimmed stdout: %+v %v", rep, err)
	}
}

// A harness that exits 0 with nothing on stdout has not answered; recording
// that as a reply would let silence read as a peer's position.
func TestParseEmptyAnswerIsAnErrorInEveryOutputMode(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		out  string
	}{
		{"text", Spec{Output: "text"}, " \n"},
		{"json", Spec{Output: "json", TextPath: "result"}, `{"result":""}`},
		{"jsonl", Spec{Output: "jsonl", TextPath: "text"}, `{"type":"x"}`},
	}
	for _, c := range cases {
		if _, err := c.spec.Parse(c.out); err == nil || !strings.Contains(err.Error(), "no text") {
			t.Errorf("%s: an empty reply must be an error, got %v", c.name, err)
		}
	}
}

func TestParseReportsTheModelTheHarnessUsed(t *testing.T) {
	s, _ := Known("claude")
	rep, err := s.Parse(`{"result":"ok","modelUsage":{"claude-opus-5-5":{"inputTokens":1},"claude-haiku-5-5":{"inputTokens":1}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Model != "claude-haiku-5-5, claude-opus-5-5" {
		t.Fatalf("every model that worked on the turn, sorted: %q", rep.Model)
	}
	rep, err = s.Parse(`{"result":"ok"}`)
	if err != nil || rep.Model != "" {
		t.Fatalf("a harness that says nothing must not be guessed for: %q %v", rep.Model, err)
	}
}

// The samples are trimmed from real turns (cursor-agent 2026.10.19, codex 0.163,
// opencode 2.1, claude), not invented.
func TestParseUsageAsEachHarnessReportsIt(t *testing.T) {
	cases := []struct {
		name         string
		out          string
		in, out2     int
		cost         float64
		reportsUsage bool
	}{
		{"cursor", `{"type":"result","is_error":false,"result":"ok","session_id":"s","usage":{"inputTokens":9178,"outputTokens":134,"cacheReadTokens":6016}}`, 9178, 134, 0, true},
		{"codex", `{"type":"thread.started","thread_id":"t"}
{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":17213,"cached_input_tokens":0,"output_tokens":22}}`, 17213, 22, 0, true},
		{"opencode", `{"type":"step_start","sessionID":"s"}
{"type":"text","sessionID":"s","part":{"type":"text","text":"ok"}}
{"type":"step_finish","sessionID":"s","part":{"type":"step-finish","cost":0.5,"tokens":{"input":100,"output":10}}}
{"type":"step_finish","sessionID":"s","part":{"type":"step-finish","cost":0.25,"tokens":{"input":200,"output":20}}}`, 300, 30, 0.75, true},
		// Claude's input_tokens leaves the cache out (2 next to 17079 cached), so
		// it would understate the turn: no tokens, only the cost it reports.
		{"claude", `{"result":"ok","total_cost_usd":0.07,"usage":{"input_tokens":2,"output_tokens":32,"cache_creation_input_tokens":17079}}`, 0, 0, 0.07, false},
	}
	for _, c := range cases {
		s, _ := Known(c.name)
		rep, err := s.Parse(c.out)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if rep.InputTokens != c.in || rep.OutputTokens != c.out2 || rep.CostUSD != c.cost {
			t.Errorf("%s: tokens %d/%d cost %v, want %d/%d cost %v", c.name, rep.InputTokens, rep.OutputTokens, rep.CostUSD, c.in, c.out2, c.cost)
		}
	}
	// A spec with no usage paths reports nothing; zero means unknown, never a guess.
	rep, err := Spec{Output: "json", TextPath: "result"}.Parse(`{"result":"ok","usage":{"input_tokens":5}}`)
	if err != nil || rep.InputTokens != 0 || rep.OutputTokens != 0 {
		t.Fatalf("no paths, no usage: %+v %v", rep, err)
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
	// The event is the failure; its payload must reach the caller.
	if _, err := s.Parse(`{"type":"error","sessionID":"x","error":{"message":"boom"}}`); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the error event must surface with its message: %v", err)
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

func TestParsePiJSONWalksContentArrays(t *testing.T) {
	// pi and omp stream the whole turn: system/user messages carry no answer,
	// and an assistant message's content is an array of typed parts.
	pi, _ := Known("pi")
	omp, _ := Known("omp")
	for _, tc := range []struct {
		name, raw, text, session string
		spec                     Spec
		wantErr                  string
	}{
		{
			name: "pi answer is the text parts of the assistant turn",
			spec: pi,
			raw: `{"type":"session","id":"s1"}
{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"question"}]}}
{"type":"turn_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"po"},{"type":"toolCall","name":"read"},{"type":"text","text":"ng"}],"stopReason":"stop"}}`,
			text: "pong",
		},
		{
			name: "omp reports its session id in the header",
			spec: omp,
			raw: `{"type":"session","version":3,"id":"01a18052"}
{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"pong"}],"stopReason":"stop"}}`,
			text: "pong", session: "01a18052",
		},
		{
			name: "an error inside the stream is an error even with exit 0",
			spec: pi,
			raw: `{"type":"session","id":"s1"}
{"type":"turn_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"401: api_key_expired"}}`,
			wantErr: "api_key_expired",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := tc.spec.Parse(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rep.Text != tc.text || rep.SessionID != tc.session {
				t.Fatalf("%+v", rep)
			}
		})
	}
}

func TestPathWalksArrays(t *testing.T) {
	obj := map[string]any{"a": []any{
		map[string]any{"t": "x", "ok": false},
		map[string]any{"n": 1},
		map[string]any{"t": "y", "ok": true},
	}}
	if got := strAt(obj, "a[].t"); got != "xy" {
		t.Fatalf("a[].t = %q, want xy", got)
	}
	if b, found := boolAt(obj, "a[].ok"); !found || !b {
		t.Fatalf("a[].ok = %v,%v, want true when any element is", b, found)
	}
	if got := strAt(obj, "a[].missing"); got != "" {
		t.Fatalf("missing key through an array = %q, want empty", got)
	}
	if got := strAt(map[string]any{"a": "scalar"}, "a[].t"); got != "" {
		t.Fatalf("[] on a non-array = %q, want empty", got)
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

func TestListModelsRunsTheSpecCommand(t *testing.T) {
	spec := Spec{Bin: "sh", ModelsArgs: []string{"-c", "printf 'a\\nb\\n'"}}
	out, err := ListModels(context.Background(), spec, "")
	if err != nil || out != "a\nb" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := ListModels(context.Background(), Spec{Bin: "sh"}, ""); err == nil {
		t.Fatal("a spec with no models command must say so")
	}
	failing := Spec{Bin: "sh", ModelsArgs: []string{"-c", "echo no >&2; exit 2"}}
	if _, err := ListModels(context.Background(), failing, ""); err == nil || !strings.Contains(err.Error(), "no") {
		t.Fatalf("the command's stderr must reach the caller: %v", err)
	}
}

func TestFailureCarriesTheSpecHintForKnownStderr(t *testing.T) {
	spec := Spec{
		Bin: "sh", Start: []string{"-c", "echo 'Workspace Trust Required' >&2; exit 1"}, Output: "text",
		ErrorHints: map[string]string{"Workspace Trust Required": "trust the directory"},
	}
	_, err := NewCLI(spec).Start(context.Background(), "", Options{})
	if err == nil || !strings.Contains(err.Error(), "Workspace Trust Required") || !strings.Contains(err.Error(), "hint: trust the directory") {
		t.Fatalf("the stderr and the hint must both be in the error: %v", err)
	}
	spec.Start = []string{"-c", "echo other >&2; exit 1"}
	_, err = NewCLI(spec).Start(context.Background(), "", Options{})
	if err == nil || strings.Contains(err.Error(), "hint:") {
		t.Fatalf("an unrelated failure must not get the hint: %v", err)
	}
}

func TestCLICommandFailureSurfacesStderr(t *testing.T) {
	spec := Spec{Bin: "sh", Start: []string{"-c", "echo fallo >&2; exit 3"}, Output: "text"}
	_, err := NewCLI(spec).Start(context.Background(), "", Options{})
	if err == nil || !strings.Contains(err.Error(), "fallo") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("the exit status and the stderr must both reach the caller: %v", err)
	}
}
