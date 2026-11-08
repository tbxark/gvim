package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

// mkTmuxSocket creates $TMUX_TMPDIR with one socket dir and socket, and points the
// process environment at it for the scan.
func mkTmuxSocket(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tmux := filepath.Join(dir, "tmux-501")
	if err := os.MkdirAll(tmux, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmux, "default"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	return dir
}

// TestAgentDiscovererHonoursTheSpawnGate checks the app's discovery port: every
// tmux command goes through the session spawn gate, a refusal becomes a stated
// scan problem, and the scanner falls back to the socket-only row.
func TestAgentDiscovererHonoursTheSpawnGate(t *testing.T) {
	mkTmuxSocket(t)
	denied := errors.New("workspace trust: the workspace is Blocked, so no processes may run")
	var calls [][]string
	gate := func(_ context.Context, argv []string, cwd string, env map[string]string) error {
		calls = append(calls, append([]string(nil), argv...))
		if env != nil {
			t.Fatalf("discovery passed a child environment: %v", env)
		}
		if cwd != "/ws" {
			t.Fatalf("gate cwd = %q, want the workspace root", cwd)
		}
		return denied
	}
	d := newAgentDiscoverer(func() string { return "/ws" }, gate, nil)
	report := d.Discover(context.Background())
	if len(calls) == 0 {
		t.Fatal("the scan ran no command through the gate")
	}
	if calls[0][0] != "tmux" || !strings.Contains(strings.Join(calls[0], " "), "list-sessions") {
		t.Fatalf("first gated argv = %v, want tmux list-sessions", calls[0])
	}
	if len(report.Problems) == 0 || !strings.Contains(strings.Join(report.Problems, "; "), "Blocked") {
		t.Fatalf("problems = %v, want the gate refusal", report.Problems)
	}
	if len(report.Sessions) != 1 || !report.Sessions[0].SocketOnly {
		t.Fatalf("sessions = %+v, want the socket-only fallback", report.Sessions)
	}
}

// TestAgentDiscovererKnownAgents checks that the configured terminal.agents
// commands extend the pane name-match set.
func TestAgentDiscovererKnownAgents(t *testing.T) {
	d := newAgentDiscoverer(func() string { return "/ws" }, nil, []config.TerminalAgent{
		{Name: "mine", Command: "/opt/bin/myagent"},
		{Name: "claude", Command: "claude"},
		{Name: "empty"},
	})
	if want := []string{"/opt/bin/myagent", "claude"}; !slices.Equal(d.known, want) {
		t.Fatalf("known agents = %v, want %v", d.known, want)
	}
}

// TestAgentDiscovererIncludesLocalAgentTranscripts checks the app adapter merges
// the read-only Claude Code/Codex stores into the same scan report the picker
// receives: no command is started for it (the tmux store here is empty, and the
// external scan only reads files).
func TestAgentDiscovererIncludesLocalAgentTranscripts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	dir := filepath.Join(home, ".claude", "projects", "-tmp-proj")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := "{\"type\":\"user\",\"cwd\":\"/tmp/proj\",\"sessionId\":\"11111111-2222-3333-4444-555555555555\"}"
	if err := os.WriteFile(filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jsonl"), []byte(record+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := newAgentDiscoverer(func() string { return "/ws" }, nil, nil)
	report := d.Discover(context.Background())
	if len(report.External.Sessions) != 1 {
		t.Fatalf("external sessions = %+v, want the Claude transcript", report.External.Sessions)
	}
	row := report.External.Sessions[0]
	if row.Agent != "claude" || row.CWD != "/tmp/proj" {
		t.Fatalf("external row = %+v", row)
	}
	if !slices.Equal(row.ResumeArgv, []string{"claude", "--resume", "11111111-2222-3333-4444-555555555555"}) {
		t.Fatalf("resume argv = %v", row.ResumeArgv)
	}
	if len(report.External.Tools) != 2 || !report.External.Tools[0].Installed {
		t.Fatalf("tools = %+v", report.External.Tools)
	}
}
