package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("PARLEY_TEST_CHILD") != "1" {
		os.Exit(m.Run())
	}
	if marker := os.Getenv("PARLEY_TEST_MARKER"); marker != "" && len(os.Args) > 1 && os.Args[1] == "session" {
		_ = os.WriteFile(marker, []byte("called"), 0o644)
	}
	if os.Getenv("PARLEY_TEST_FAIL") == "1" {
		os.Exit(1)
	}
	fmt.Print(os.Getenv("PARLEY_TEST_OUT"))
	os.Exit(0)
}

const sessionRecord = `{"info":{},"messages":[{"model":{"id":"old","providerID":"p0"}},{"model":{"id":"deepseek-v4.1-flash","providerID":"opencode-go"}},{}]}`

func TestSessionModelReadsTheLastModelThatIsThere(t *testing.T) {
	spec := Spec{
		Bin:                      os.Args[0],
		SessionModelArgs:         []string{"session", "export", "{session}"},
		SessionModelPath:         "messages[].model.id",
		SessionModelProviderPath: "messages[].model.providerID",
	}
	cases := []struct {
		name, out, want, fail string
	}{
		{name: "last one with a model wins, with its provider", out: sessionRecord, want: "opencode-go/deepseek-v4.1-flash"},
		{name: "a record with no model is unknown", out: `{"messages":[{},{}]}`, want: ""},
		{name: "not JSON is unknown", out: "usage: opencode session export <id>", want: ""},
		{name: "a failing command is unknown", out: sessionRecord, fail: "1", want: ""},
		{name: "empty stdout is unknown", out: "", want: ""},
	}
	for _, c := range cases {
		o := Options{Dir: t.TempDir(), Env: []string{"PARLEY_TEST_CHILD=1", "PARLEY_TEST_OUT=" + c.out, "PARLEY_TEST_FAIL=" + c.fail}}
		if got := NewCLI(spec).sessionModel(context.Background(), "ses_1", o); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	spec.SessionModelProviderPath = ""
	o := Options{Dir: t.TempDir(), Env: []string{"PARLEY_TEST_CHILD=1", "PARLEY_TEST_OUT=" + sessionRecord}}
	if got := NewCLI(spec).sessionModel(context.Background(), "ses_1", o); got != "deepseek-v4.1-flash" {
		t.Fatalf("no provider path: got %q", got)
	}
}

func TestSessionModelRunsNothingWithoutACommandOrASession(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "called")
	o := Options{Dir: t.TempDir(), Env: []string{"PARLEY_TEST_CHILD=1", "PARLEY_TEST_MARKER=" + marker}}
	if got := NewCLI(Spec{Bin: os.Args[0]}).sessionModel(context.Background(), "ses_1", o); got != "" {
		t.Fatalf("no command: got %q", got)
	}
	spec := Spec{Bin: os.Args[0], SessionModelArgs: []string{"session", "export", "{session}"}}
	if got := NewCLI(spec).sessionModel(context.Background(), "", Options{Dir: o.Dir, Env: o.Env}); got != "" {
		t.Fatalf("no session: got %q", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a process ran when it should not have: %v", err)
	}
}

func TestATurnReadsTheModelOnlyWhenItHasTo(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "called")
	spec := Spec{
		Bin:                      os.Args[0],
		Prompt:                   "arg",
		Session:                  "arg",
		Output:                   "text",
		SessionModelArgs:         []string{"session", "export", "{session}"},
		SessionModelPath:         "messages[].model.id",
		SessionModelProviderPath: "messages[].model.providerID",
		ModelArgs:                []string{"--model", "{model}"},
	}
	c := NewCLI(spec)
	env := []string{"PARLEY_TEST_CHILD=1", "PARLEY_TEST_MARKER=" + marker, "PARLEY_TEST_OUT=" + sessionRecord}

	rep, err := c.Start(context.Background(), "q", Options{SessionID: "ses_1", Dir: t.TempDir(), Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Model != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("the session record was not read: %q", rep.Model)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the command did not run: %v", err)
	}

	_ = os.Remove(marker)
	rep, err = c.Start(context.Background(), "q", Options{SessionID: "ses_1", Model: "pinned", Dir: t.TempDir(), Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Model != "" {
		t.Fatalf("a pinned call must not ask the harness what ran: %q", rep.Model)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a second process ran for a pinned call: %v", err)
	}
}
