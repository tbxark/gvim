package authority

import (
	"strings"
	"testing"
)

// TestRemoteLoginCommandEnv checks the remote terminal's environment prefix: the delta is
// env K=V entries, sorted and POSIX-quoted as data, applied before the remote login shell,
// and an empty/malformed delta keeps the exact old command.
func TestRemoteLoginCommandEnv(t *testing.T) {
	plain := remoteLoginCommand("/remote/proj")
	if got := remoteLoginCommandEnv("/remote/proj", nil); got != "" {
		t.Fatalf("empty env command = %q, want \"\" (the plain command stays)", got)
	}
	got := remoteLoginCommandEnv("/remote/proj", []string{
		"PATH=/venv/bin:/usr/bin", "PROJECT='quoted' value", "MALFORMED", "A=1",
	})
	wantParts := []string{
		"cd " + QuoteDir("/remote/proj") + " && env",
		ShellQuote("A=1"),
		ShellQuote("PATH=/venv/bin:/usr/bin"),
		ShellQuote("PROJECT='quoted' value"),
		"\"$" + "{SHELL:-/bin/sh}\" -l",
	}
	pos := 0
	for _, part := range wantParts {
		idx := strings.Index(got[pos:], part)
		if idx < 0 {
			t.Fatalf("command %q lacks %q", got, part)
		}
		pos += idx + len(part)
	}
	if strings.Contains(got, "MALFORMED") {
		t.Fatalf("a malformed entry reached the command: %q", got)
	}
	// No directory: the env prefix is the whole command.
	got = remoteLoginCommandEnv("", []string{"A=1"})
	if !strings.HasPrefix(got, "env ") || strings.Contains(got, "cd ") {
		t.Fatalf("dirless command = %q", got)
	}
	// The old plain command is byte-identical when no env is applied.
	if plain != "cd "+QuoteDir("/remote/proj")+" && exec "+"$"+"{SHELL:-/bin/sh}"+" -l" {
		t.Fatalf("plain command changed: %q", plain)
	}
}
