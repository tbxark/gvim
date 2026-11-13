package shellcmd_test

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/shellcmd"
)

// TestRunArgsKeepsArgumentsLiteral: an argument vector reaches the program as it is, so
// a query with spaces and quotes needs no shell quoting.
func TestRunArgsKeepsArgumentsLiteral(t *testing.T) {
	t.Parallel()
	out, code, err := shellcmd.RunArgsEnv(t.TempDir(), []string{"printf", "%s|", "a b", "c\"d", "$HOME"}, "", 10*time.Second, nil)
	if err != nil || code != 0 {
		t.Fatalf("RunArgs: %q, %d, %v", out, code, err)
	}
	if want := "a b|c\"d|$HOME|"; out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

// TestRunArgsReturnsTheExitCode: a non-zero exit is a result, not an error.
func TestRunArgsReturnsTheExitCode(t *testing.T) {
	t.Parallel()
	out, code, err := shellcmd.RunArgsEnv(t.TempDir(), []string{"sh", "-c", "printf hi; exit 3"}, "", 10*time.Second, nil)
	if err != nil {
		t.Fatalf("RunArgs error = %v, want the exit code", err)
	}
	if out != "hi" || code != 3 {
		t.Fatalf("output %q, code %d; want hi and 3", out, code)
	}
}

func TestRunArgsFailsOnAStartError(t *testing.T) {
	t.Parallel()
	if _, _, err := shellcmd.RunArgsEnv(t.TempDir(), []string{"no-such-command-gvim"}, "", time.Second, nil); err == nil {
		t.Fatal("a missing program gave no error")
	}
}

func TestRunArgsTimesOut(t *testing.T) {
	t.Parallel()
	if _, _, err := shellcmd.RunArgsEnv(t.TempDir(), []string{"sh", "-c", "sleep 5"}, "", 100*time.Millisecond, nil); err == nil {
		t.Fatal("a timed-out command gave no error")
	}
}
