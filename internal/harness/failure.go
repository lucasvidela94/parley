package harness

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// failure is the error for a turn that produced no reply. The useful part is
// the harness's own message, and not every harness puts it on stderr: claude
// prints an is_error JSON object on stdout and exits non-zero with an empty
// stderr, pi reports the failure inside a stream with exit 0, and codex uses a
// turn.failed event. Both streams are read, and the spec's error_kinds add a
// stable classification when the text is one it has seen.
func (s Spec) failure(cause error, stdout, stderr string) error {
	text := s.failureText(stdout, stderr)
	prefix := ""
	if kind := s.kind(text); kind != "" {
		prefix = "turn failed (" + kind + "): "
	}
	hint := s.hint(text)
	if text == "" {
		return fmt.Errorf("%s: %s%w%s", s.Bin, prefix, cause, hint)
	}
	return fmt.Errorf("%s: %s%w: %s%s", s.Bin, prefix, cause, text, hint)
}

// failureText is what the harness said about the failure: the parsed stdout
// message when the output format carries one, then stderr. Only when neither
// has anything does the raw stdout stand in, so a text-mode CLI that prints its
// error on stdout is not silent.
func (s Spec) failureText(stdout, stderr string) string {
	var parts []string
	if msg := s.stdoutError(stdout); msg != "" {
		parts = append(parts, msg)
	}
	if msg := strings.TrimSpace(tail(stderr, 1200)); msg != "" {
		parts = append(parts, msg)
	}
	if len(parts) == 0 {
		if raw := strings.TrimSpace(tail(stdout, 600)); raw != "" {
			parts = append(parts, raw)
		}
	}
	return strings.Join(parts, "; ")
}

// stdoutError reads the failure message from a json or jsonl reply. It is
// deliberately separate from Parse: a failed turn has no answer, only an error,
// and Parse would reject the missing text.
func (s Spec) stdoutError(stdout string) string {
	if strings.TrimSpace(stdout) == "" {
		return ""
	}
	switch s.Output {
	case "json":
		var obj map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(stdout)), &obj) != nil {
			return ""
		}
		return s.objectError(obj)
	case "jsonl":
		var msgs []string
		for _, line := range strings.Split(stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(line), &obj) != nil {
				continue
			}
			if msg := s.objectError(obj); msg != "" {
				msgs = append(msgs, msg)
			}
		}
		return strings.Join(msgs, "; ")
	}
	return ""
}

func (s Spec) objectError(obj map[string]any) string {
	if msg := strAt(obj, s.ErrorPath); msg != "" {
		return msg
	}
	if isEvent, ok := boolAt(obj, s.ErrorBoolPath); ok && isEvent {
		if msg := strAt(obj, s.TextPath); msg != "" {
			return msg
		}
		return "the harness reported an error"
	}
	if s.ErrorEventType != "" && strAt(obj, "type") == s.ErrorEventType {
		return "the harness reported an error"
	}
	return ""
}

// kind is the spec's classification for a failure, matched case-insensitively
// against everything the harness said. Keys are sorted so the result is stable
// when more than one matches.
func (s Spec) kind(text string) string {
	if text == "" || len(s.ErrorKinds) == 0 {
		return ""
	}
	lower := strings.ToLower(text)
	keys := make([]string, 0, len(s.ErrorKinds))
	for k := range s.ErrorKinds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.Contains(lower, strings.ToLower(k)) {
			return s.ErrorKinds[k]
		}
	}
	return ""
}
