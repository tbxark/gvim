//go:build unix

package agentdiscover

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestExecRunnerReportsATimeout checks a command the timeout kills reports the
// deadline, not a bare exit status.
func TestExecRunnerReportsATimeout(t *testing.T) {
	res := ExecRunner{Timeout: 50 * time.Millisecond}.Run(context.Background(), "sleep", "5")
	if res.Code != -1 || !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("result = code %d err %v, want -1 and the deadline", res.Code, res.Err)
	}
}
