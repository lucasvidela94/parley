package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucasvidela94/parley/internal/harness"
)

func TestDefaultIsEmpty(t *testing.T) {
	c := Default()
	if len(c.Participants) != 0 {
		t.Fatalf("the default must not assume a harness: %v", c.Participants)
	}
	if c.DefaultTimeoutS == 0 || c.MaxDepth == 0 {
		t.Fatalf("%+v", c)
	}
}

func TestSpecFromHarnessIsTheKnownSpecUntouched(t *testing.T) {
	c := Default()
	// Model and args belong to the call, not to the spec.
	c.Participants["rev"] = Participant{Harness: "claude", Model: "opus", Args: []string{"--x"}}
	s, _, err := c.Spec("rev")
	if err != nil {
		t.Fatal(err)
	}
	if known, _ := harness.Known("claude"); !reflect.DeepEqual(s, known) {
		t.Fatalf("a participant with no inline spec must resolve to its harness spec:\ngot  %+v\nwant %+v", s, known)
	}
}

func TestSpecInlineOverrides(t *testing.T) {
	c := Default()
	c.Participants["x"] = Participant{
		Harness: "codex",
		Spec:    &harness.Spec{Bin: "mi-codex", TextPath: "output.text"},
	}
	s, _, err := c.Spec("x")
	if err != nil {
		t.Fatal(err)
	}
	if s.Bin != "mi-codex" || s.TextPath != "output.text" {
		t.Fatalf("%+v", s)
	}
	if s.Output != "jsonl" { // inherited from the known harness
		t.Fatalf("it lost the rest of the spec: %+v", s)
	}
}

func TestReadOnlyArgsPerParticipant(t *testing.T) {
	raw := `{"participants": {
  "hermes":        {"harness": "hermes"},
  "hermes-reader": {"harness": "hermes", "spec": {"read_only_args": ["-t", "file,web"]}},
  "hermes-open":   {"harness": "hermes", "spec": {"read_only_args": []}}
}}`
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		want     []string
		enforced bool
	}{
		{"hermes", []string{"-t", "safe"}, true},
		{"hermes-reader", []string{"-t", "file,web"}, true},
		{"hermes-open", []string{}, false},
	}
	for _, tc := range cases {
		s, _, err := c.Spec(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(s.ReadOnlyArgs, tc.want) || s.EnforcesReadOnly() != tc.enforced {
			t.Fatalf("%s: read_only_args %q enforced=%v", tc.name, s.ReadOnlyArgs, s.EnforcesReadOnly())
		}
		if s.Bin != "hermes" || s.TextFilterEquals != "result" {
			t.Fatalf("%s lost the rest of the hermes spec: %+v", tc.name, s)
		}
	}
}

func TestInlineSpecReplacesHintsAndListingOfTheHarness(t *testing.T) {
	c := Default()
	c.Participants["cur"] = Participant{Harness: "cursor", Spec: &harness.Spec{
		ErrorHints: map[string]string{"quota": "wait"},
		ErrorKinds: map[string]string{"quota": "quota"},
		ModelsArgs: []string{"list"},
	}}
	c.Participants["plain"] = Participant{Harness: "cursor"}
	s, _, err := c.Spec("cur")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.ErrorHints, map[string]string{"quota": "wait"}) ||
		!reflect.DeepEqual(s.ErrorKinds, map[string]string{"quota": "quota"}) ||
		!slices.Equal(s.ModelsArgs, []string{"list"}) {
		t.Fatalf("the participant's own hints and listing must replace the harness's: %+v", s)
	}
	plain, _, _ := c.Spec("plain")
	if len(plain.ErrorHints) == 0 || len(plain.ModelsArgs) == 0 {
		t.Fatalf("without an override the harness keeps its own: %+v", plain)
	}
}

func TestSpecCustomOnlyGetsDefaults(t *testing.T) {
	c := Default()
	c.Participants["y"] = Participant{Spec: &harness.Spec{Bin: "otro", Start: []string{"go"}}}
	s, _, err := c.Spec("y")
	if err != nil {
		t.Fatal(err)
	}
	if s.Bin != "otro" || s.Prompt != "stdin" || s.Output != "text" {
		t.Fatalf("an inline spec with no harness must get stdin/text defaults: %+v", s)
	}
	c.Participants["nobin"] = Participant{Spec: &harness.Spec{Start: []string{"go"}}}
	if _, _, err := c.Spec("nobin"); err == nil {
		t.Fatal("a spec with no binary cannot run")
	}
}

func TestSpecErrors(t *testing.T) {
	c := Default()
	if _, _, err := c.Spec("nadie"); err == nil {
		t.Fatal("expected error for a missing participant")
	}
	c.Participants["z"] = Participant{Harness: "noexiste"}
	if _, _, err := c.Spec("z"); err == nil {
		t.Fatal("expected error for an unknown harness")
	}
	c.Participants["w"] = Participant{}
	if _, _, err := c.Spec("w"); err == nil {
		t.Fatal("expected error for an empty participant")
	}
}

func TestReadOnlyDefault(t *testing.T) {
	if !(Participant{}).ReadOnly() {
		t.Fatal("default must be read_only")
	}
	if (Participant{Permission: "write"}).ReadOnly() {
		t.Fatal("write must enable writes")
	}
}

func TestLoadRejectsAnUnknownKeyWithItsLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PARLEY_CONFIG", path)
	raw := `{
  "participants": {
    "claude": {"harness": "claude", "modle": "x"}
  }
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), `unknown field "modle"`) || !strings.Contains(err.Error(), ":3:") {
		t.Fatalf("a typo must name the line: %v", err)
	}
}

func TestLoadRejectsWhatWouldFailLater(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"bad permission", `{"participants":{"a":{"harness":"claude","permission":"rw"}}}`, "permission must be"},
		{"bad name", `{"participants":{"a,b":{"harness":"claude"}}}`, "cannot be empty"},
		{"self", `{"participants":{"self":{"harness":"claude"}}}`, "local identity"},
		{"unknown harness", `{"participants":{"a":{"harness":"nope"}}}`, "unknown harness"},
		{"nothing", `{"participants":{"a":{}}}`, "set a harness or an inline spec"},
		{"spec without bin", `{"participants":{"a":{"spec":{"start":["go"]}}}}`, `needs a "bin"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			t.Setenv("PARLEY_CONFIG", path)
			if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}

func TestLoadRejectsTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PARLEY_CONFIG", path)
	raw := "{\"participants\":{\"a\":{\"harness\":\"claude\"}}}\n{\"participants\":{}}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "unexpected data") {
		t.Fatalf("a second value must not be ignored: %v", err)
	}
}

func TestLoadLocatesAnUnknownKeyInsideNestedObjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PARLEY_CONFIG", path)
	raw := `{
  "participants": {
    "a": {
      "harness": "claude",
      "args": ["bogus"],
      "bogus": 1
    }
  }
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	// The array value "bogus" must not be mistaken for the key on line 6.
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), `unknown field "bogus"`) || !strings.Contains(err.Error(), ":6:") {
		t.Fatalf("got %v", err)
	}
}

func TestSaveKeepsASymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(`{"participants":{"a":{"harness":"claude"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks are unavailable:", err)
	}
	t.Setenv("PARLEY_CONFIG", link)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c.Participants["b"] = Participant{Harness: "opencode"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink must survive: %v %v", fi, err)
	}
	c, err = Load()
	if err != nil || len(c.Participants) != 2 {
		t.Fatalf("%+v %v", c.Participants, err)
	}
}

func TestSaveReplacesTheFilePrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PARLEY_CONFIG", path)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c.Participants["claude"] = Participant{Harness: "claude"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the config must stay private: %v %v", fi, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("a temporary file was left behind: %+v %v", entries, err)
	}
}

func TestChangedNoticesAnEditedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PARLEY_CONFIG", path)
	if err := os.WriteFile(path, []byte(`{"participants":{"a":{"harness":"claude"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Changed() {
		t.Fatal("a config nobody touched is not stale")
	}
	if err := os.WriteFile(path, []byte(`{"participants":{"b":{"harness":"codex"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if !c.Changed() {
		t.Fatal("an edited file must be reported as changed")
	}
}

func TestChangedIsFalseWithoutAFile(t *testing.T) {
	if Default().Changed() {
		t.Fatal("a config built in memory has no file to go stale")
	}
}
