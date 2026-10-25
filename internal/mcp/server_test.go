package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasvidela94/parley/internal/config"
	"github.com/lucasvidela94/parley/internal/core"
	"github.com/lucasvidela94/parley/internal/harness"
	"github.com/lucasvidela94/parley/internal/store"
)

type fakeH struct{ answer string }

func (f *fakeH) Name() string       { return "fake" }
func (f *fakeH) Spec() harness.Spec { return harness.Spec{Bin: "fake"} }
func (f *fakeH) Start(ctx context.Context, p string, o harness.Options) (harness.Reply, error) {
	return harness.Reply{Text: f.answer}, nil
}
func (f *fakeH) Continue(ctx context.Context, p string, o harness.Options) (harness.Reply, error) {
	return harness.Reply{Text: f.answer}, nil
}

func newEngine(t *testing.T) *core.Engine {
	t.Helper()
	t.Chdir(t.TempDir())
	cfg := config.Default()
	cfg.Participants = map[string]config.Participant{"claude": {Harness: "claude"}, "opencode": {Harness: "opencode"}}
	cfg.Store = filepath.Join(t.TempDir(), "p.db")
	st, err := store.Open(cfg.Store)
	if err != nil {
		t.Fatal(err)
	}
	e := core.NewWith(cfg, st, func(config.Participant) harness.Harness { return &fakeH{answer: "answer"} })
	t.Cleanup(func() { e.Shutdown() })
	return e
}

func TestServe(t *testing.T) {
	e := newEngine(t)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"send_message","arguments":{"to":"claude","message":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"participants","arguments":{}}}`,
	}, "\n") + "\n"

	var out, logs bytes.Buffer
	if err := New(e, "test").Serve(context.Background(), strings.NewReader(in), &out, &logs); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses (notifications get no reply): %d\n%s", len(lines), out.String())
	}

	var init struct {
		Result struct {
			ProtocolVersion string                `json:"protocolVersion"`
			ServerInfo      struct{ Name string } `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	if init.Result.ProtocolVersion != "2025-06-18" || init.Result.ServerInfo.Name != "parley" {
		t.Fatalf("%+v", init.Result)
	}

	var list struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range list.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"participants", "open_conversation", "send_message", "read_conversation", "close_conversation"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}

	var call struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &call); err != nil {
		t.Fatal(err)
	}
	if call.Result.IsError || !strings.HasPrefix(call.Result.Content[0].Text, "answer") {
		t.Fatalf("%+v", call.Result)
	}

	var parts struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &parts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parts.Result.Content[0].Text, "(claude,") {
		t.Fatalf("participants does not show the harness: %s", parts.Result.Content[0].Text)
	}
}

func TestUnknownMethod(t *testing.T) {
	e := newEngine(t)
	var out, logs bytes.Buffer
	in := `{"jsonrpc":"2.0","id":1,"method":"nope"}` + "\n"
	if err := New(e, "test").Serve(context.Background(), strings.NewReader(in), &out, &logs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-32601") {
		t.Fatalf("%s", out.String())
	}
}
