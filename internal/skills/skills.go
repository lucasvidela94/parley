package skills

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed parley/SKILL.md
var Parley string

// Install writes the skill into the skill directories of the harnesses found on
// this machine. A nil return means every write succeeded.
func Install() map[string]error {
	home, _ := os.UserHomeDir()
	dirs := map[string]string{
		"opencode": filepath.Join(home, ".config", "opencode", "skills"),
		"claude":   filepath.Join(home, ".claude", "skills"),
		"agents":   filepath.Join(home, ".agents", "skills"),
	}
	out := map[string]error{}
	for harness, base := range dirs {
		parent := filepath.Dir(base)
		if _, err := os.Stat(parent); err != nil {
			continue
		}
		target := filepath.Join(base, "parley")
		if err := os.MkdirAll(target, 0o755); err != nil {
			out[harness] = err
			continue
		}
		if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(Parley), 0o644); err != nil {
			out[harness] = err
		}
	}
	return out
}
