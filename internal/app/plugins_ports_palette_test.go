package app

import (
	"strings"
	"testing"
	"time"
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

// TestLSPNavigationRealHost drives the default-off lsp_navigation plugin on the real app:
// enabling it in config makes its commands appear in the palette.
func TestLSPNavigationRealHost(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins":{"lsp_navigation":{"enabled":true}}}`)
	s := newSession(t, Options{})
	s.waitStatus("lsp_navigation", "loaded")
	s.openFile("a.go", "package a\n\nfunc main() {}\n")
	s.settle(30 * time.Millisecond)
	if !s.paletteHas("Go to LSP Symbol") {
		t.Fatalf("the palette has no %q row for the config-enabled plugin:\n%s", "Go to LSP Symbol", strings.Join(s.rows(), "\n"))
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
