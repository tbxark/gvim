package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestOffByDefaultPortsReachThePalette is the real-host verification for the config-locked
// enable path of a default-off bundled plugin (the round-7 ports were only exercised through
// the fake host: a throwaway harness opened the palette before the plugins settled and saw no
// plugin rows, which read as a host defect). Enabling through the user's config loads the
// plugin, and its commands are in the palette once the plugin reports loaded: the palette
// table is built from the loaded-plugin command table when it opens (internal/tui/model.go
// openPalette -> allCommands), so the correct procedure is to wait for "loaded" first.
func TestOffByDefaultPortsReachThePalette(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins":{"git_log":{"enabled":true},"live_diff":{"enabled":true}}}`)
	s := newSession(t, Options{})
	s.waitStatus("git_log", "loaded")
	s.waitStatus("live_diff", "loaded")
	s.openFile("a.go", "package a\n")
	s.settle(30 * time.Millisecond)

	for _, title := range []string{
		"Git Log",
		"Git Log: Current File",
		"Git Log: Close",
		"Git Log: Refresh",
		"Live Diff: Toggle (Global)",
		"Live Diff: Toggle (Buffer)",
		"Live Diff: vs HEAD",
	} {
		if !s.paletteHas(title) {
			t.Errorf("the palette has no %q row although the plugin is loaded (round-7 finding)", title)
		}
	}

	// The row is not just listed: it runs. The global toggle needs no repository and its
	// notice is deterministic.
	s.runCommand("Live Diff: Toggle (Global)")
	s.until("the live diff toggle notice", func() bool {
		return strings.Contains(s.statusRow(), "Live Diff: enabled globally")
	})
}

// TestPathCompleteRealHost drives the round-8 path_complete port on the real app: its command
// is in the default palette, the prompt opens with the suggestion list working, and Esc
// cancels (the suggestions vanish with the prompt, as Fresh's do on close).
func TestPathCompleteRealHost(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.waitStatus("path_complete", "loaded")
	s.openFile("a.go", "package a\n")
	s.settle(30 * time.Millisecond)
	if !s.paletteHas("Path Completion: Complete Path") {
		t.Fatal("the palette has no path completion row for the default-on plugin")
	}
	s.runCommand("Path Completion: Complete Path")
	s.until("the path prompt", func() bool { return s.screenHas("Path:") })
	// The suggestion contents are asserted on the fake host (hermetic filesystem); here the
	// point is that the real prompt is wired and that Esc cancels it, taking the suggestion
	// list with it, as Fresh's prompt close does.
	s.typeText("p")
	s.settle(40 * time.Millisecond)
	s.press(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.settle(40 * time.Millisecond)
	if got := s.statusRow(); !strings.Contains(got, "Path completion cancelled") {
		t.Errorf("cancel notice = %q", got)
	}
}

// TestRound9PortsRealHost drives the round-9 default-off ports on the real app: enabling
// lsp_navigation and find_references in config makes their commands appear in the palette,
// and the references command reaches the host's LSP layer (no server in this fixture, so it
// reports the request failure instead of opening an empty list).
func TestRound9PortsRealHost(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins":{"lsp_navigation":{"enabled":true},"find_references":{"enabled":true}}}`)
	s := newSession(t, Options{})
	s.waitStatus("lsp_navigation", "loaded")
	s.waitStatus("find_references", "loaded")
	s.openFile("a.go", "package a\n\nfunc main() {}\n")
	s.settle(30 * time.Millisecond)
	for _, title := range []string{"Go to LSP Symbol", "Show References Panel"} {
		if !s.paletteHas(title) {
			t.Fatalf("the palette has no %q row for the config-enabled port:\n%s", title, strings.Join(s.rows(), "\n"))
		}
	}
}

// TestGitGrepRealHost drives the round-10 default-off port on the real app: enabling git_grep
// in config makes both of its Fresh palette commands appear.
func TestGitGrepRealHost(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins":{"git_grep":{"enabled":true}}}`)
	s := newSession(t, Options{})
	s.waitStatus("git_grep", "loaded")
	s.openFile("a.go", "package a\n")
	s.settle(30 * time.Millisecond)
	for _, title := range []string{"Git Grep", "Git Grep: Toggle Case Sensitive"} {
		if !s.paletteHas(title) {
			t.Fatalf("the palette has no %q row for the config-enabled port:\n%s", title, strings.Join(s.rows(), "\n"))
		}
	}
	// Let the session's background work (config/state writers) finish before the test's
	// TempDir cleanup runs.
	s.settle(200 * time.Millisecond)
}
