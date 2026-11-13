package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFailureReadsTheReasonOnStdout(t *testing.T) {
	s, _ := Known("claude")
	// Captured from claude 2.1.322 with a subscription whose payment is past
	// due: exit 1, empty stderr, the reason in the result field.
	stdout := `{"type":"result","is_error":true,"api_error_code":"oauth_not_allowed_for_organization","result":"Claude Code can't use your Claude subscription, usually because a payment is past due · Pay or renew at https://claude.ai/settings/billing"}`
	err := s.failure(errors.New("exit status 1"), stdout, "")
	for _, want := range []string{"turn failed (auth)", "payment is past due"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q missing from %q", want, err)
		}
	}
}

func TestFailureClassifiesQuotaAndKeepsTheResetDate(t *testing.T) {
	s, _ := Known("cursor")
	stderr := "You've hit your usage limit. Your limit will reset when your monthly cycle ends on 11/7/2026"
	err := s.failure(errors.New("exit status 1"), "", stderr)
	for _, want := range []string{"turn failed (quota)", "usage limit", "11/7/2026"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q missing from %q", want, err)
		}
	}
}

func TestFailureLeavesUnclassifiedTextAlone(t *testing.T) {
	s, _ := Known("claude")
	err := s.failure(errors.New("exit status 1"), "", "something broke")
	if got, want := err.Error(), "claude: exit status 1: something broke"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFailureReadsAnErrorInsideTheStream(t *testing.T) {
	s, _ := Known("pi")
	// pi reports a 401 inside the stream and exits 0; Parse turns it into an
	// error, and the same message must survive into the failure.
	stdout := `{"type":"turn_end","message":{"stopReason":"error","errorMessage":"401 unauthorized","content":[]}}`
	err := s.failure(errors.New("the harness reported an error"), stdout, "")
	if !strings.Contains(err.Error(), "401 unauthorized") {
		t.Fatalf("the stream's message is lost: %q", err)
	}
}

func TestCLIReadsStdoutWhenTheCommandFails(t *testing.T) {
	s := Spec{
		Bin:           "sh",
		Start:         []string{"-c", `echo '{"is_error":true,"result":"payment is past due"}'; exit 1`},
		Prompt:        "stdin",
		Output:        "json",
		TextPath:      "result",
		ErrorBoolPath: "is_error",
		ErrorKinds:    map[string]string{"payment is past due": "auth"},
	}
	if _, err := NewCLI(s).Start(context.Background(), "hi", Options{}); err == nil || !strings.Contains(err.Error(), "payment is past due") {
		t.Fatalf("a non-zero exit must not hide what the harness printed on stdout: %v", err)
	}
}

func TestFailureKeepsTheTrustHint(t *testing.T) {
	s, _ := Known("cursor")
	err := s.failure(errors.New("exit status 1"), "", "Workspace Trust Required")
	if !strings.Contains(err.Error(), "turn failed (trust)") || !strings.Contains(err.Error(), "(hint:") {
		t.Fatalf("%q", err)
	}
}
