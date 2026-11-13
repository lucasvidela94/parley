package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

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
	cfg, err := decode(raw)
	if err != nil {
		return Config{}, fmt.Errorf("%s:%w", path, err)
	}
	cfg.path = path
	if fi, err := os.Stat(path); err == nil {
		cfg.mtime = fi.ModTime()
	}
	cfg.normalize()
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// decode reads the file strictly: an unknown key is a typo, and dropping it
// silently used to leave a participant running with a default it never asked
// for. The error carries a line and column, so the file can be fixed in place.
func decode(raw []byte) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, locatedError(raw, dec, err)
	}
	// Decode reads one value and stops; a second one would be ignored, which is
	// how a manual edit leaves half a config in place.
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			line, col := lineCol(raw, dec.InputOffset())
			return Config{}, fmt.Errorf("%d:%d: unexpected data after the config object", line, col)
		}
		return Config{}, locatedError(raw, dec, err)
	}
	return cfg, nil
}

// locatedError adds the line and column the stdlib leaves out for an unknown
// field, and passes through the offsets it does report.
func locatedError(raw []byte, dec *json.Decoder, err error) error {
	off := dec.InputOffset()
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		off = syntax.Offset
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		off = typeErr.Offset
	}
	line, col := lineCol(raw, off)
	const unknown = "json: unknown field "
	if strings.HasPrefix(err.Error(), unknown) {
		// No offset in the error: a token walk finds the key. If the same name
		// also appears earlier as a valid key, the line is the best guess; the
		// field name in the message is exact.
		field := strings.Trim(strings.TrimPrefix(err.Error(), unknown), `"`)
		if l, c, ok := locateKey(raw, field); ok {
			line, col = l, c
		}
	}
	return fmt.Errorf("%d:%d: %w", line, col, err)
}

// lineCol turns a byte offset into a 1-based line and column.
func lineCol(raw []byte, off int64) (int, int) {
	line, col := 1, 1
	for i := 0; i < len(raw) && int64(i) < off; {
		r, size := utf8.DecodeRune(raw[i:])
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += size
	}
	return line, col
}

// locateKey walks the document to find the position of an object key, so an
// unknown-field error names the typo's line instead of the object's last brace.
func locateKey(raw []byte, field string) (int, int, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var stack []bool // true for an object, false for an array
	expectKey := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, false
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				stack = append(stack, true)
				expectKey = true
			case '[':
				stack = append(stack, false)
				expectKey = false
			case '}', ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				expectKey = len(stack) > 0 && stack[len(stack)-1]
			}
			continue
		}
		if len(stack) == 0 || !stack[len(stack)-1] {
			continue
		}
		if expectKey {
			if key, ok := tok.(string); ok && key == field {
				line, col := lineCol(raw, dec.InputOffset())
				return line, col, true
			}
			expectKey = false
			continue
		}
		expectKey = true // the value ended; the next token is the next key
	}
}

// validate rejects a config that would fail later, at the first send: a typo in
// a harness name, a permission nobody checks, a participant with neither.
func (c Config) validate() error {
	for name, p := range c.Participants {
		if name == "" || strings.TrimSpace(name) != name || strings.ContainsRune(name, ',') || strings.ContainsAny(name, "\x00\n\r\t") {
			return fmt.Errorf("participant %q: a name cannot be empty, start or end with a space, or contain a comma, newline or tab (the chain is comma-separated)", name)
		}
		if name == "self" {
			return errors.New(`participant "self": that is the local identity; pick another name`)
		}
		if p.Permission != "" && p.Permission != "read_only" && p.Permission != "write" {
			return fmt.Errorf("participant %q: permission must be \"read_only\" or \"write\", got %q", name, p.Permission)
		}
		if p.Harness == "" && p.Spec == nil {
			return fmt.Errorf("participant %q: set a harness or an inline spec", name)
		}
		if p.Harness == "" && p.Spec != nil && p.Spec.Bin == "" {
			return fmt.Errorf("participant %q: an inline spec needs a \"bin\"", name)
		}
		if p.Harness != "" {
			if _, ok := harness.Known(p.Harness); !ok {
				return fmt.Errorf("participant %q: unknown harness %q (known: %s)", name, p.Harness, strings.Join(harness.Names(), ", "))
			}
		}
	}
	return nil
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
	if err := c.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write through a symlink instead of replacing the link with a file: a
	// dotfiles config often points at a repo.
	target := c.path
	if resolved, err := filepath.EvalSymlinks(c.path); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	// Write beside the target and rename: a crash or a full disk leaves the old
	// config in place instead of a truncated one every harness fails to read.
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
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
	if over.InputUsagePath != "" {
		base.InputUsagePath = over.InputUsagePath
	}
	if over.OutputUsagePath != "" {
		base.OutputUsagePath = over.OutputUsagePath
	}
	if over.ErrorHints != nil {
		base.ErrorHints = over.ErrorHints
	}
	if over.ErrorKinds != nil {
		base.ErrorKinds = over.ErrorKinds
	}
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
