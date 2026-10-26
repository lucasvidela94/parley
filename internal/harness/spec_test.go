package harness

import (
	"context"
	"testing"
)

func TestRender(t *testing.T) {
	claude, _ := Known("claude")
	got := claude.Render(false, "hola", "uuid-1", "opus", "/tmp")
	want := []string{"-p", "--session-id", "uuid-1", "--output-format", "json"}
	if len(got) != len(want) {
		t.Fatalf("len: got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg %d: got %q want %q", i, got[i], want[i])
		}
	}

	open, _ := Known("opencode")
	got = open.Render(true, "hola", "ses_1", "", "/tmp")
	if got[0] != "run" || got[1] != "--session" || got[2] != "ses_1" {
		t.Fatalf("continue render: %v", got)
	}
	if got[len(got)-1] != "hola" {
		t.Fatalf("prompt not appended: %v", got)
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
