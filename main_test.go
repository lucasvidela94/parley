package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lucasvidela94/parley/internal/config"
)

func TestSetupSkillsOnlyRefreshesTheSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, "parley-config.json")
	t.Setenv("PARLEY_CONFIG", cfgPath)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := cmdSetup([]string{"--skills"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "parley", "SKILL.md"))
	if err != nil || !bytes.Equal(got, skillMarkdown) {
		t.Fatalf("skill not installed: %v", err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("--skills must not touch the config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("a harness that is not installed got a skill directory")
	}
}

func TestInstallSkillFilesReportsSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o750); err != nil {
		t.Fatal(err)
	}
	res := installSkillFiles()
	if err, ok := res["claude"]; !ok || err != nil {
		t.Fatalf("claude should be reported installed: %+v", res)
	}
	if _, ok := res["opencode"]; ok {
		t.Fatalf("opencode is absent and must not be reported: %+v", res)
	}
}

func TestSendNamesTheFieldThatIsMissing(t *testing.T) {
	if err := cmdSend(nil); err == nil || err.Error() != "missing --to" {
		t.Fatalf("a send with no recipient: %v", err)
	}
	// The message is read from stdin when argv carries none; use an empty pipe
	// so the test cannot block on the terminal.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = old
		_ = r.Close()
	}()
	if err := cmdSend([]string{"--to", "claude"}); err == nil || err.Error() != "empty message (--to claude)" {
		t.Fatalf("an empty message must name the recipient: %v", err)
	}
}

func TestFlagsFirst(t *testing.T) {
	cases := []struct {
		args     []string
		note     string
		asJSON   bool
		position string
	}{
		{[]string{"cnv_1", "--note", "decided"}, "decided", false, "cnv_1"},
		{[]string{"--note", "decided", "cnv_1"}, "decided", false, "cnv_1"},
		{[]string{"cnv_1", "--json", "--note=x y"}, "x y", true, "cnv_1"},
		{[]string{"cnv_1"}, "", false, "cnv_1"},
		{[]string{"--", "-cnv"}, "", false, "-cnv"},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("close", flag.ContinueOnError)
		note := fs.String("note", "", "")
		asJSON := fs.Bool("json", false, "")
		ordered, err := flagsFirst(fs, c.args)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if err := fs.Parse(ordered); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if *note != c.note || *asJSON != c.asJSON || fs.Arg(0) != c.position {
			t.Fatalf("%v: note=%q json=%v args=%v", c.args, *note, *asJSON, strings.Join(fs.Args(), ","))
		}
	}
}

func TestFlagsFirstRefusesAValueFlagWithNoValue(t *testing.T) {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.String("note", "", "")
	// The "--" sentinel must not become the flag's value.
	if _, err := flagsFirst(fs, []string{"cnv_1", "--note"}); err == nil || !strings.Contains(err.Error(), "needs an argument") {
		t.Fatalf("%v", err)
	}
}

func participantEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PARLEY_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("PARLEY_DATA", filepath.Join(home, "p.db"))
	return home
}

func TestParticipantAdd(t *testing.T) {
	participantEnv(t)
	if err := cmdParticipant([]string{"add", "rev", "--harness", "opencode", "--model", "deepseek/x", "--arg", "--variant", "--arg", "high"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Participants["rev"]
	if !ok || p.Harness != "opencode" || p.Model != "deepseek/x" || !slices.Equal(p.Args, []string{"--variant", "high"}) {
		t.Fatalf("%+v", p)
	}
	if err := cmdParticipant([]string{"add", "rev", "--harness", "claude"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a duplicate must be refused: %v", err)
	}
	if err := cmdParticipant([]string{"add", "bad", "--harness", "nope"}); err == nil || !strings.Contains(err.Error(), "unknown harness") {
		t.Fatalf("an unknown harness must be refused: %v", err)
	}
}

func TestParticipantSet(t *testing.T) {
	home := participantEnv(t)
	if err := cmdParticipant([]string{"add", "rev", "--harness", "opencode", "--arg", "--variant"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdParticipant([]string{"set", "rev", "--model", "other/y", "--no-args"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.Participants["rev"]; p.Model != "other/y" || len(p.Args) != 0 {
		t.Fatalf("%+v", p)
	}

	specPath := filepath.Join(home, "spec.json")
	if err := os.WriteFile(specPath, []byte(`{"bin":"sh","start":["-c","echo hi"],"prompt":"arg","output":"text"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdParticipant([]string{"add", "custom", "--harness", "opencode", "--spec", specPath}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if spec, _, err := cfg.Spec("custom"); err != nil || spec.Bin != "sh" {
		t.Fatalf("%+v %v", spec, err)
	}
	if err := cmdParticipant([]string{"set", "custom", "--no-spec"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if spec, _, err := cfg.Spec("custom"); err != nil || spec.Bin != "opencode" {
		t.Fatalf("the inline spec must be gone and the harness kept: %+v %v", spec, err)
	}
	if err := cmdParticipant([]string{"set", "rev"}); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("a set with no flag must say so: %v", err)
	}
}

func TestParticipantSetKeepsWhatItDoesNotMention(t *testing.T) {
	participantEnv(t)
	if err := cmdParticipant([]string{"add", "rev", "--harness", "opencode", "--dir", "/tmp/x", "--permission", "write", "--model", "a/b", "--arg", "--variant"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdParticipant([]string{"set", "rev", "--model", ""}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Participants["rev"]
	if p.Model != "" || p.Harness != "opencode" || p.Dir != "/tmp/x" || p.Permission != "write" || !slices.Equal(p.Args, []string{"--variant"}) {
		t.Fatalf("%+v", p)
	}
}

func TestParticipantRefusesAFlagWithoutValue(t *testing.T) {
	participantEnv(t)
	for _, args := range [][]string{
		{"add", "rev", "--harness"},
		{"add", "rev", "--harness", "claude", "--arg"},
	} {
		if err := cmdParticipant(args); err == nil || !strings.Contains(err.Error(), "needs an argument") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Participants) != 0 {
		t.Fatalf("a refused flag must not write a participant: %+v", cfg.Participants)
	}
}

func TestParticipantRemove(t *testing.T) {
	participantEnv(t)
	if err := cmdParticipant([]string{"add", "rev", "--harness", "claude"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdParticipant([]string{"rm", "rev"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Participants["rev"]; ok {
		t.Fatal("rm left the participant behind")
	}
	if err := cmdParticipant([]string{"rm", "rev"}); err == nil {
		t.Fatal("removing an unknown participant must fail")
	}
}
