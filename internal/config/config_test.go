package config

import (
	"encoding/json"
	"slices"
	"testing"

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

func TestSpecFromHarness(t *testing.T) {
	c := Default()
	c.Participants["rev"] = Participant{Harness: "claude", Model: "opus"}
	s, _, err := c.Spec("rev")
	if err != nil {
		t.Fatal(err)
	}
	if s.Bin != "claude" || s.TextPath != "result" {
		t.Fatalf("%+v", s)
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

func TestSpecCustomOnly(t *testing.T) {
	c := Default()
	c.Participants["y"] = Participant{Spec: &harness.Spec{
		Bin: "otro", Start: []string{"go"}, Output: "text", TextPath: "",
	}}
	if _, _, err := c.Spec("y"); err != nil {
		t.Fatal(err)
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
