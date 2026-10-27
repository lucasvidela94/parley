package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClassifyContextError(t *testing.T) {
	// A deadline in the past is already exceeded, so no sleep is needed.
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name    string
		ctx     context.Context
		want    string
		wantErr error
	}{
		{"live", context.Background(), "", nil},
		{"deadline", deadline, "timeout: ", context.DeadlineExceeded},
		{"cancelled", cancelled, "cancelled: ", context.Canceled},
	}
	for _, c := range cases {
		err := classify(c.ctx)
		if c.want == "" {
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Fatalf("%s: got %v, want prefix %q", c.name, err, c.want)
		}
		if !errors.Is(err, c.wantErr) {
			t.Fatalf("%s: %v does not wrap %v", c.name, err, c.wantErr)
		}
	}
}
