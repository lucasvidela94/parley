package main

import (
	_ "embed"
	"os"
	"path/filepath"
)

// SKILL.md is the usage skill. It lives at the repository root so it can be read
// and reviewed like any other document, and it is embedded so `parley setup`
// installs the exact same text.
//
//go:embed SKILL.md
var skillMarkdown []byte

// installSkillFiles writes the skill into the skill directory of every harness
// present on this machine. Every harness it touched is in the result: nil for
// installed, the error otherwise.
func installSkillFiles() map[string]error {
	home, _ := os.UserHomeDir()
	dirs := map[string]string{
		"opencode": filepath.Join(home, ".config", "opencode", "skills"),
		"claude":   filepath.Join(home, ".claude", "skills"),
		"agents":   filepath.Join(home, ".agents", "skills"),
	}
	out := map[string]error{}
	for harness, base := range dirs {
		if _, err := os.Stat(filepath.Dir(base)); err != nil {
			continue
		}
		target := filepath.Join(base, "parley")
		if err := os.MkdirAll(target, 0o750); err != nil {
			out[harness] = err
			continue
		}
		out[harness] = os.WriteFile(filepath.Join(target, "SKILL.md"), skillMarkdown, 0o600)
	}
	return out
}
