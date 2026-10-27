//go:build unix

package harness

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCancelKillsTheHarnessChildren(t *testing.T) {
	// The child holds stdout open: killing only sh would leave parley waiting
	// for the pipe until WaitDelay.
	c := NewCLI(Spec{Bin: "sh", Start: []string{"-c", "sleep 30 & wait"}, Prompt: "stdin", Output: "text"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	begin := time.Now()
	_, err := c.Start(ctx, "", Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a timeout, got %v", err)
	}
	if took := time.Since(begin); took >= waitDelay {
		t.Fatalf("took %s: the child outlived the cancel", took)
	}
}
