package editor

import (
	"strings"
	"testing"
	"time"
)

func TestShellCommandTimesOut(t *testing.T) {
	old := shellTimeout
	shellTimeout = 200 * time.Millisecond
	t.Cleanup(func() { shellTimeout = old })

	for _, a := range []Action{"shell_command", "shell_command_replace"} {
		e := withDoc("keep")
		start := time.Now()
		r := e.Do(a, shellArgs("sleep 5"))
		if r.Err == nil || !strings.Contains(r.Err.Error(), "timed out") {
			t.Fatalf("Do(%s) err = %v, want a timeout", a, r.Err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("Do(%s) took %v after a 200ms timeout", a, elapsed)
		}
		if got := text(e); got != "keep" {
			t.Fatalf("Do(%s) changed the text to %q", a, got)
		}
	}
}
