package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Harness is the seam: two real adapters means the seam is real, and the CLI
// spec keeps the number of implementations at one.
type Harness interface {
	Name() string
	Spec() Spec
	Start(ctx context.Context, prompt string, o Options) (Reply, error)
	Continue(ctx context.Context, prompt string, o Options) (Reply, error)
}

const waitDelay = 3 * time.Second

// CLI runs any harness described by a Spec.
type CLI struct{ spec Spec }

func NewCLI(s Spec) *CLI { return &CLI{spec: s} }

func (c *CLI) Name() string { return c.spec.Bin }
func (c *CLI) Spec() Spec   { return c.spec }

func (c *CLI) Start(ctx context.Context, prompt string, o Options) (Reply, error) {
	return c.run(ctx, c.spec.Render(false, prompt, o), prompt, o)
}

func (c *CLI) Continue(ctx context.Context, prompt string, o Options) (Reply, error) {
	if !c.spec.SupportsContinuity() {
		return c.Start(ctx, prompt, o)
	}
	return c.run(ctx, c.spec.Render(true, prompt, o), prompt, o)
}

func (c *CLI) run(ctx context.Context, args []string, prompt string, o Options) (Reply, error) {
	if len(args) == 0 {
		return Reply{}, errors.New("spec has no arguments")
	}
	cmd := exec.CommandContext(ctx, c.spec.Bin, args...)
	killGroup(cmd)
	// After a cancel, stop waiting for pipes a surviving child still holds.
	cmd.WaitDelay = waitDelay
	if o.Dir != "" {
		cmd.Dir = o.Dir
	}
	cmd.Env = append(os.Environ(), o.Env...)
	if c.spec.Prompt == "stdin" {
		cmd.Stdin = strings.NewReader(prompt)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if cerr := classify(ctx); cerr != nil {
		return Reply{}, cerr
	}
	if err != nil {
		return Reply{}, fmt.Errorf("%s: %w: %s%s", c.spec.Bin, err, tail(stderr.String(), 1200), c.spec.hint(stderr.String()))
	}
	rep, err := c.spec.Parse(stdout.String())
	if err != nil {
		return rep, fmt.Errorf("%s: %w", c.spec.Bin, err)
	}
	if rep.SessionID == "" && c.spec.Session == "arg" {
		rep.SessionID = o.SessionID
	}
	return rep, nil
}

// hint is the spec's advice for a failure whose stderr it recognises, ready to
// append to the error. The first match in key order wins, so it is stable.
func (s Spec) hint(stderr string) string {
	keys := make([]string, 0, len(s.ErrorHints))
	for k := range s.ErrorHints {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.Contains(stderr, k) {
			return " (hint: " + s.ErrorHints[k] + ")"
		}
	}
	return ""
}

// ListModels runs the spec's own model listing and returns what it printed.
func ListModels(ctx context.Context, s Spec, dir string) (string, error) {
	if len(s.ModelsArgs) == 0 {
		return "", fmt.Errorf("%s cannot list its models: the spec has no models_args", s.Bin)
	}
	cmd := exec.CommandContext(ctx, s.Bin, s.ModelsArgs...)
	killGroup(cmd)
	cmd.WaitDelay = waitDelay
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if cerr := classify(ctx); cerr != nil {
		return "", cerr
	}
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", s.Bin, strings.Join(s.ModelsArgs, " "), err, tail(stderr.String(), 1200))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// classify turns a context failure into the error the caller sees. A deadline
// is a timeout; an explicit cancellation (Ctrl-C, the MCP client going away) is
// not, and reporting it as one hid why the turn ended.
func classify(ctx context.Context) error {
	err := ctx.Err()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("timeout: %w", err)
	case err != nil:
		return fmt.Errorf("cancelled: %w", context.Cause(ctx))
	}
	return nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
