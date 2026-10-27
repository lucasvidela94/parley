package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Reply is what a harness returns for one turn.
type Reply struct {
	Text      string
	SessionID string
	CostUSD   float64
	Raw       string
}

// Options are per-call knobs. SessionID is only used when the spec takes the
// session as an argument. Args is extra argv from the participant's config.
type Options struct {
	Model     string
	Dir       string
	ReadOnly  bool
	SessionID string
	Env       []string
	Args      []string
}

// Spec describes how to drive one CLI agent. Everything that varies between
// harnesses lives here; the adapter code is shared.
type Spec struct {
	Bin      string   `json:"bin"`
	Start    []string `json:"start"`
	Continue []string `json:"continue,omitempty"`

	// Prompt is "arg" (appended to argv) or "stdin".
	Prompt string `json:"prompt"`
	// Session is "arg" (we mint an id and substitute {session}), "output:<path>"
	// (read it from the reply) or "none" (no continuity).
	Session string `json:"session"`

	// Output is "text", "json" or "jsonl".
	Output string `json:"output"`
	// TextPath is a dotted path inside the object that holds the answer.
	TextPath string `json:"text_path,omitempty"`
	// TextFilterPath/TextFilterEquals keep only lines whose field matches
	// (jsonl). E.g. type == "text".
	TextFilterPath   string `json:"text_filter_path,omitempty"`
	TextFilterEquals string `json:"text_filter_equals,omitempty"`
	// ErrorPath holds a non-empty error message; ErrorEventType marks a whole
	// line as a failure; ErrorBoolPath holds a true value on failure.
	ErrorPath      string `json:"error_path,omitempty"`
	ErrorEventType string `json:"error_event_type,omitempty"`
	ErrorBoolPath  string `json:"error_bool_path,omitempty"`
	CostPath       string `json:"cost_path,omitempty"`

	ModelArgs    []string `json:"model_args,omitempty"`
	ReadOnlyArgs []string `json:"read_only_args,omitempty"`
	WriteArgs    []string `json:"write_args,omitempty"`

	Verified bool   `json:"verified,omitempty"`
	Notes    string `json:"notes,omitempty"`
}

// EnforcesReadOnly reports whether the spec has a flag that stops the harness
// from editing. Without one, read-only is only a line in the prompt.
func (s Spec) EnforcesReadOnly() bool { return len(s.ReadOnlyArgs) > 0 }

// SupportsContinuity reports whether the spec can add a turn to an existing
// session.
func (s Spec) SupportsContinuity() bool {
	return s.Session != "" && s.Session != "none" && len(s.Continue) > 0
}

// Known returns a copy of a built-in harness spec.
func Known(name string) (Spec, bool) {
	s, ok := profiles[name]
	return s, ok
}

// Names lists the built-in harness names, in a stable order.
func Names() []string {
	return []string{"claude", "opencode", "codex", "cursor", "pi", "hermes", "omp"}
}

var profiles = map[string]Spec{
	"claude": {
		Bin: "claude",
		// Verified against claude 2.1.305: -p --output-format json returns
		// {result, session_id, is_error, total_cost_usd}.
		Start:         []string{"-p", "--session-id", "{session}", "--output-format", "json"},
		Continue:      []string{"-p", "--resume", "{session}", "--output-format", "json"},
		Prompt:        "stdin",
		Session:       "arg",
		Output:        "json",
		TextPath:      "result",
		ErrorPath:     "error",
		ErrorBoolPath: "is_error",
		CostPath:      "total_cost_usd",
		ModelArgs:     []string{"--model", "{model}"},
		ReadOnlyArgs:  []string{"--permission-mode", "plan", "--disallowedTools", "Write,Edit,MultiEdit,NotebookEdit"},
		Verified:      true,
		Notes:         "session id must be a UUID; parley mints one",
	},
	"opencode": {
		Bin: "opencode",
		// Verified against opencode 2.1.4: --format json emits NDJSON events
		// carrying sessionID and parts.
		Start:            []string{"run", "--format", "json"},
		Continue:         []string{"run", "--session", "{session}", "--format", "json"},
		Prompt:           "arg",
		Session:          "output:sessionID",
		Output:           "jsonl",
		TextPath:         "part.text",
		TextFilterPath:   "type",
		TextFilterEquals: "text",
		ErrorEventType:   "error",
		ModelArgs:        []string{"--model", "{model}"},
		ReadOnlyArgs:     []string{"--agent", "plan"},
		Verified:         true,
		Notes:            "no MCP server mode; expose through the CLI or the v2 HTTP API",
	},
	"codex": {
		Bin:              "codex",
		Start:            []string{"exec", "--json", "--skip-git-repo-check"},
		Continue:         []string{"exec", "resume", "{session}", "--json", "--skip-git-repo-check"},
		Prompt:           "stdin",
		Session:          "output:thread_id",
		Output:           "jsonl",
		TextPath:         "item.text",
		TextFilterPath:   "item.type",
		TextFilterEquals: "agent_message",
		ErrorEventType:   "turn.failed",
		ErrorPath:        "error.message",
		ModelArgs:        []string{"-m", "{model}"},
		// `exec resume` has no --sandbox flag; the config override works on
		// both `exec` and `exec resume`.
		ReadOnlyArgs: []string{"-c", `sandbox_mode="read-only"`},
		Verified:     true,
		Notes:        "verified against codex-cli 0.163.0: thread.started carries thread_id, item.completed/agent_message the text, and exec resume keeps the thread; the turn.failed event was not forced",
	},
	"cursor": {
		Bin:           "cursor-agent",
		Start:         []string{"-p", "--output-format", "json"},
		Continue:      []string{"-p", "--output-format", "json", "--resume", "{session}"},
		Prompt:        "arg",
		Session:       "output:session_id",
		Output:        "json",
		TextPath:      "result",
		ErrorBoolPath: "is_error",
		ModelArgs:     []string{"--model", "{model}"},
		ReadOnlyArgs:  []string{"--mode", "plan"},
		Verified:      true,
		Notes:         "verified against 2026.10.19-6bbf2c4: result carries the text, session_id the session, --resume keeps it, --mode plan is accepted; the working directory must be trusted (`--trust` or an interactive run) or the CLI refuses to start, and parley does not pass --trust for you",
	},
	"pi": {
		Bin: "pi",
		// --mode text is pi's default; its json mode puts the answer in
		// message.content[] (an array) and reports errors inside the stream
		// with exit 0, neither of which the adapter can read.
		Start:        []string{"-p", "--session-id", "{session}"},
		Continue:     []string{"-p", "--session-id", "{session}"},
		Prompt:       "arg",
		Session:      "arg",
		Output:       "text",
		ModelArgs:    []string{"--model", "{model}"},
		ReadOnlyArgs: []string{"--no-tools"},
		Verified:     false,
		Notes:        "flags verified against pi 1.3.0 (--session-id creates the session, --no-tools disables tools); the turn was blocked by an expired provider key, so text output is inferred from exit 1 with the error on stderr",
	},
	"hermes": {
		Bin: "hermes",
		// The prompt sits right after -q; flags parley appends land after it.
		Start:            []string{"chat", "-Q", "--format", "stream-json", "-q", "{prompt}"},
		Continue:         []string{"chat", "-Q", "--format", "stream-json", "--resume", "{session}", "-q", "{prompt}"},
		Prompt:           "arg",
		Session:          "output:session_id",
		Output:           "jsonl",
		TextPath:         "text",
		TextFilterPath:   "type",
		TextFilterEquals: "result",
		ErrorPath:        "error",
		ModelArgs:        []string{"-m", "{model}"},
		// The safe toolset (web, vision, image_gen) is the only one without
		// file writes or a terminal. It also has no file reads: the file
		// toolset bundles read_file with write_file and patch.
		ReadOnlyArgs: []string{"-t", "safe"},
		Verified:     true,
		Notes:        "verified against hermes 0.21.4: stream-json result event carries text, session_id and error, --resume keeps the conversation; read-only (-t safe) cannot read files, so override read_only_args per participant if a reviewer must see the repo",
	},
	"omp": {
		Bin: "omp",
		// Like pi, omp's json mode puts the answer in message.content[] (an
		// array) and only json mode reports the session id, so the spec uses
		// text mode without continuity; parley sends the history every turn.
		Start:        []string{"-p"},
		Prompt:       "arg",
		Session:      "none",
		Output:       "text",
		ModelArgs:    []string{"--model", "{model}"},
		ReadOnlyArgs: []string{"--no-tools"},
		Verified:     true,
		Notes:        "verified against omp 18.13.0: -p prints the answer as text, a failure exits 1 with the error on stderr, --no-tools disables tools; no session continuity",
	},
}

// Render builds argv: the start or continue command (cont), then the
// permission flags (ReadOnlyArgs or WriteArgs), the model flags when a model is
// set, the participant's extra args, and the prompt last when the spec puts it
// on argv.
func (s Spec) Render(cont bool, prompt string, o Options) []string {
	src := s.Start
	if cont {
		src = s.Continue
	}
	src = append([]string{}, src...)
	if o.ReadOnly {
		src = append(src, s.ReadOnlyArgs...)
	} else {
		src = append(src, s.WriteArgs...)
	}
	if o.Model != "" {
		src = append(src, s.ModelArgs...)
	}
	src = append(src, o.Args...)

	out := make([]string, 0, len(src)+1)
	sawPrompt := false
	for _, a := range src {
		if strings.Contains(a, "{prompt}") {
			sawPrompt = true
		}
		a = strings.ReplaceAll(a, "{session}", o.SessionID)
		a = strings.ReplaceAll(a, "{model}", o.Model)
		a = strings.ReplaceAll(a, "{dir}", o.Dir)
		a = strings.ReplaceAll(a, "{prompt}", prompt)
		out = append(out, a)
	}
	if s.Prompt == "arg" && !sawPrompt {
		out = append(out, prompt)
	}
	return out
}

// Parse turns raw stdout into a Reply.
func (s Spec) Parse(stdout string) (Reply, error) {
	rep := Reply{Raw: stdout}
	switch s.Output {
	case "text":
		rep.Text = strings.TrimSpace(stdout)
		return rep, nil
	case "json":
		return s.parseJSON(stdout)
	case "jsonl":
		return s.parseJSONL(stdout)
	default:
		return rep, fmt.Errorf("unknown output: %q", s.Output)
	}
}

func (s Spec) parseJSON(stdout string) (Reply, error) {
	rep := Reply{Raw: stdout}
	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &obj); err != nil {
		return rep, fmt.Errorf("reply is not JSON: %w", err)
	}
	if err := s.checkError(obj); err != nil {
		return rep, err
	}
	rep.Text = strings.TrimSpace(strAt(obj, s.TextPath))
	rep.SessionID = s.sessionFrom(obj)
	if s.CostPath != "" {
		rep.CostUSD, _ = floatAt(obj, s.CostPath)
	}
	if rep.Text == "" {
		return rep, fmt.Errorf("reply has no text (%s empty)", s.TextPath)
	}
	return rep, nil
}

func (s Spec) parseJSONL(stdout string) (Reply, error) {
	rep := Reply{Raw: stdout}
	var parts []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if s.ErrorEventType != "" && strAt(obj, "type") == s.ErrorEventType {
			return rep, fmt.Errorf("event %s: %s", s.ErrorEventType, line)
		}
		if err := s.checkError(obj); err != nil {
			return rep, err
		}
		if rep.SessionID == "" {
			rep.SessionID = s.sessionFrom(obj)
		}
		if s.TextFilterPath != "" && strAt(obj, s.TextFilterPath) != s.TextFilterEquals {
			continue
		}
		if txt := strAt(obj, s.TextPath); txt != "" {
			parts = append(parts, txt)
		}
	}
	rep.Text = strings.TrimSpace(strings.Join(parts, "\n"))
	if rep.Text == "" {
		return rep, fmt.Errorf("reply has no text (%s empty)", s.TextPath)
	}
	return rep, nil
}

func (s Spec) checkError(obj map[string]any) error {
	if msg := strAt(obj, s.ErrorPath); msg != "" {
		return errors.New(msg)
	}
	if b, ok := boolAt(obj, s.ErrorBoolPath); ok && b {
		return errors.New("the harness reported an error")
	}
	return nil
}

func (s Spec) sessionFrom(obj map[string]any) string {
	if !strings.HasPrefix(s.Session, "output:") {
		return ""
	}
	return strAt(obj, strings.TrimPrefix(s.Session, "output:"))
}

func at(obj map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	cur := any(obj)
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func strAt(obj map[string]any, path string) string {
	v, ok := at(obj, path)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolAt(obj map[string]any, path string) (bool, bool) {
	v, ok := at(obj, path)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func floatAt(obj map[string]any, path string) (float64, bool) {
	v, ok := at(obj, path)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}
