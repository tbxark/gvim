package editor

import "testing"

// TestShellCommandSeesTheProjectEnvironment: SetShellEnv's overlay reaches a
// shell command (and the same job path runs formatters).
func TestShellCommandSeesTheProjectEnvironment(t *testing.T) {
	t.Parallel()
	e := withDoc("x")
	e.SetShellEnv(func() []string { return []string{"GVIM_SHELL_ENV=activated"} })
	if err := e.Do("shell_command", shellArgs(`printf %s "$GVIM_SHELL_ENV"`)).Err; err != nil {
		t.Fatalf("shell_command: %v", err)
	}
	if got := e.Active().Buffer.Text(); got != "activated" {
		t.Fatalf("output = %q, want the overlay value", got)
	}
}
