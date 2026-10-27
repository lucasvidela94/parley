package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		if err := fs.Parse(flagsFirst(fs, c.args)); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if *note != c.note || *asJSON != c.asJSON || fs.Arg(0) != c.position {
			t.Fatalf("%v: note=%q json=%v args=%v", c.args, *note, *asJSON, strings.Join(fs.Args(), ","))
		}
	}
}
