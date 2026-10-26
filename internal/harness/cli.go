package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Harness is the seam: two real adapters means the seam is real, and the CLI
// spec keeps the number of implementations at one.
type Harness interface {
	Name() string
	Spec() Spec
	Start(ctx context.Context, prompt string, o Options) (Reply, error)
	Continue(ctx context.Context, prompt string, o Options) (Reply, error)
}

// CLI runs any harness described by a Spec.
type CLI struct{ spec Spec }

func NewCLI(s Spec) *CLI { return &CLI{spec: s} }

func (c *CLI) Name() string { return c.spec.Bin }
func (c *CLI) Spec() Spec   { return c.spec }

func (c *CLI) Start(ctx context.Context, prompt string, o Options) (Reply, error) {
	return c.run(ctx, c.spec.Render(false, prompt, o.SessionID, o.Model, o.Dir), prompt, o)
}

func (c *CLI) Continue(ctx context.Context, prompt string, o Options) (Reply, error) {
	if !c.spec.SupportsContinuity() {
		return c.Start(ctx, prompt, o)
	}
	return c.run(ctx, c.spec.Render(true, prompt, o.SessionID, o.Model, o.Dir), prompt, o)
}

func (c *CLI) run(ctx context.Context, args []string, prompt string, o Options) (Reply, error) {
	if len(args) == 0 {
		return Reply{}, errors.New("spec has no arguments")
	}
	cmd := exec.CommandContext(ctx, c.spec.Bin, args...)
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
	if ctx.Err() != nil {
		return Reply{}, fmt.Errorf("timeout: %w", ctx.Err())
	}
	if err != nil {
		return Reply{}, fmt.Errorf("%s: %w: %s", c.spec.Bin, err, tail(stderr.String(), 1200))
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

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
