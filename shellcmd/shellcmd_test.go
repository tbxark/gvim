package shellcmd_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/shellcmd"
)

func TestRunSucceedsWhenABackgroundChildKeepsOutputOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sleep.pid")
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proc.Kill()
				}
			}
		}
	})
	// The shell exits 0, but the background sleep keeps the output pipe open longer
	// than WaitDelay (1s), so Wait returns exec.ErrWaitDelay. A command that exits 0
	// must still succeed.
	out, err := shellcmd.Run(dir, "echo hi; sleep 2 & echo $! > sleep.pid", "", 30*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "hi\n" {
		t.Fatalf("output = %q, want %q", out, "hi\n")
	}
}
