package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lucasvidela94/parley/internal/harness"
)

type Participant struct {
	// Harness is the CLI family this participant runs: claude, opencode, codex,
	// cursor, pi, or a custom one with an inline Spec.
	Harness string `json:"harness"`
	// Model is optional and always native to that harness (an OpenCode
	// provider/model id, a Claude alias, whatever the CLI accepts). parley never
	// interprets it.
	Model string `json:"model,omitempty"`
	// Args is extra argv appended after the spec's flags, for anything the
	// spec does not cover (a variant, a profile, a provider flag).
	Args       []string      `json:"args,omitempty"`
	Dir        string        `json:"dir,omitempty"`
	Permission string        `json:"permission,omitempty"` // read_only (default) | write
	Spec       *harness.Spec `json:"spec,omitempty"`       // overrides the known harness spec
}

type Config struct {
	Identity        string                 `json:"identity,omitempty"`
	DefaultTimeoutS int                    `json:"default_timeout_s,omitempty"`
	MaxDepth        int                    `json:"max_depth,omitempty"`
	Store           string                 `json:"store,omitempty"`
	Participants    map[string]Participant `json:"participants"`
	path            string
	mtime           time.Time // of the file this config was read from
}

func Path() string {
	if p := os.Getenv("PARLEY_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "parley", "config.json")
}

func DefaultStore() string {
	if p := os.Getenv("PARLEY_DATA"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(dir, "parley", "parley.db")
}

// Default starts empty on purpose: which harnesses exist is a property of the
// machine, not of the binary. `parley setup` fills it from PATH.
func Default() Config {
	return Config{
		DefaultTimeoutS: 300,
		MaxDepth:        3,
		Participants:    map[string]Participant{},
	}
}

// Detect returns one participant per known harness whose binary is on PATH.
func Detect() map[string]Participant {
	out := map[string]Participant{}
	for _, name := range harness.Names() {
		s, ok := harness.Known(name)
		if !ok || s.Bin == "" {
			continue
		}
		if _, err := exec.LookPath(s.Bin); err == nil {
			out[name] = Participant{Harness: name}
		}
	}
	return out
}

// Load reads the config, creating an empty one on first run.
func Load() (Config, error) {
	path := Path()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		cfg.path = path
		if err := cfg.Save(); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg.path = path
	if fi, err := os.Stat(path); err == nil {
		cfg.mtime = fi.ModTime()
	}
	cfg.normalize()
	return cfg, nil
}

// Changed reports whether the file this config was read from has been edited
// since. A config that never came from a file is never stale.
func (c Config) Changed() bool {
	if c.path == "" || c.mtime.IsZero() {
		return false
	}
	fi, err := os.Stat(c.path)
	return err == nil && !fi.ModTime().Equal(c.mtime)
}

func (c *Config) normalize() {
	if c.DefaultTimeoutS <= 0 {
		c.DefaultTimeoutS = 300
	}
	if c.MaxDepth <= 0 {
		c.MaxDepth = 3
	}
	if c.Participants == nil {
		c.Participants = map[string]Participant{}
	}
}

func (c Config) Save() error {
	c.normalize()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(raw, '\n'), 0o600)
}

func (c Config) Path() string { return c.path }

func (c Config) StorePath() string {
	if c.Store != "" {
		return c.Store
	}
	return DefaultStore()
}

// Spec resolves a participant to the spec that drives it.
func (c Config) Spec(name string) (harness.Spec, Participant, error) {
	p, ok := c.Participants[name]
	if !ok {
		return harness.Spec{}, p, fmt.Errorf("unknown participant: %q (configure %s)", name, Path())
	}
	if p.Harness == "" && p.Spec == nil {
		return harness.Spec{}, p, fmt.Errorf("participant %q has neither harness nor spec", name)
	}
	var s harness.Spec
	if p.Harness != "" {
		base, ok := harness.Known(p.Harness)
		if !ok {
			return harness.Spec{}, p, fmt.Errorf("unknown harness: %q (known: %v)", p.Harness, harness.Names())
		}
		s = base
	}
	if p.Spec != nil {
		s = merge(s, *p.Spec)
	}
	if s.Bin == "" {
		return s, p, fmt.Errorf("participant %q has no binary", name)
	}
	if s.Prompt == "" {
		s.Prompt = "stdin"
	}
	if s.Output == "" {
		s.Output = "text"
	}
	return s, p, nil
}

func (p Participant) ReadOnly() bool { return p.Permission != "write" }

// merge lets an inline spec add flags without retyping the whole harness spec.
func merge(base, over harness.Spec) harness.Spec {
	if over.Bin != "" {
		base.Bin = over.Bin
	}
	if len(over.Start) > 0 {
		base.Start = over.Start
	}
	if len(over.Continue) > 0 {
		base.Continue = over.Continue
	}
	if over.Prompt != "" {
		base.Prompt = over.Prompt
	}
	if over.Session != "" {
		base.Session = over.Session
	}
	if over.Output != "" {
		base.Output = over.Output
	}
	if over.TextPath != "" {
		base.TextPath = over.TextPath
	}
	if over.TextFilterPath != "" {
		base.TextFilterPath = over.TextFilterPath
	}
	if over.TextFilterEquals != "" {
		base.TextFilterEquals = over.TextFilterEquals
	}
	if over.ErrorPath != "" {
		base.ErrorPath = over.ErrorPath
	}
	if over.ErrorEventType != "" {
		base.ErrorEventType = over.ErrorEventType
	}
	if over.ErrorBoolPath != "" {
		base.ErrorBoolPath = over.ErrorBoolPath
	}
	if over.CostPath != "" {
		base.CostPath = over.CostPath
	}
	if over.ModelKeysPath != "" {
		base.ModelKeysPath = over.ModelKeysPath
	}
	return mergeFlagArgs(base, over)
}

// mergeFlagArgs lets a participant replace the flag lists of its harness spec.
func mergeFlagArgs(base, over harness.Spec) harness.Spec {
	if over.ModelArgs != nil {
		base.ModelArgs = over.ModelArgs
	}
	if over.ModelsArgs != nil {
		base.ModelsArgs = over.ModelsArgs
	}
	if over.ReadOnlyArgs != nil {
		base.ReadOnlyArgs = over.ReadOnlyArgs
	}
	if over.WriteArgs != nil {
		base.WriteArgs = over.WriteArgs
	}
	return base
}
