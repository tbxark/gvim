package shellcmd_test

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/shellcmd"
)

func TestRunSucceedsWhenABackgroundChildKeepsOutputOpen(t *testing.T) {
	t.Parallel()
	// The shell exits 0, but the background sleep keeps the output pipe open, so
	// Wait returns exec.ErrWaitDelay. A command that exits 0 must still succeed.
	out, err := shellcmd.Run(t.TempDir(), "echo hi; sleep 3 &", "", 30*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "hi\n" {
		t.Fatalf("output = %q, want %q", out, "hi\n")
	}
}
