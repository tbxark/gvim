package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// TestTerminalSourcesSnapshotBoundsAndNames checks the root's Live Grep terminal
// snapshot: open dock tabs with their explicit names/titles/launch programs, the
// newest per-tab transcript window bounded to 500 rows/1 MiB, at most 16 tabs, and
// no snapshot at all without a dock.
func TestTerminalSourcesSnapshotBoundsAndNames(t *testing.T) {
	t.Parallel()
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), &shellsStarter{})
	if got := m.terminalSources(); got != nil {
		t.Fatalf("a dock without tabs offered terminals: %+v", got)
	}
	long := make([]string, 0, 600)
	for i := 0; i < 600; i++ {
		long = append(long, "row")
	}
	tabs := []terminal.SavedTab{
		{Name: "shell", Open: true, Rows: []string{"first", "todo here"}},
		{Title: "vim — x", Open: true, Rows: []string{"title row"}},
		{LaunchArgv: []string{"/usr/bin/claude", "--x"}, Open: true, Rows: []string{"agent row"}},
		{Open: true, Rows: long},
	}
	for i := 0; i < 20; i++ {
		tabs = append(tabs, terminal.SavedTab{Open: true, Rows: []string{"t"}})
	}
	m.RestoreTerminal(terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Tabs: tabs})
	got := m.terminalSources()
	if len(got) == 0 || len(got) > palette.GrepTerminalsMaxTabs {
		t.Fatalf("terminal tabs = %d, want 1..%d", len(got), palette.GrepTerminalsMaxTabs)
	}
	if got[0].Name != "shell" || got[0].Index != 0 || len(got[0].Lines) != 2 || got[0].Lines[1] != "todo here" {
		t.Fatalf("named tab snapshot = %+v", got[0])
	}
	if got[1].Name != "vim — x" {
		t.Fatalf("title fallback = %q", got[1].Name)
	}
	if got[2].Name != "claude" {
		t.Fatalf("launch-program fallback = %q", got[2].Name)
	}
	// The 600-row tab was trimmed to the dock's saved bound, and the source bound keeps
	// the newest rows while never exceeding the per-tab cap.
	for _, term := range got {
		if len(term.Lines) > palette.GrepTerminalsMaxRows {
			t.Fatalf("tab %d has %d rows, over the %d cap", term.Index, len(term.Lines), palette.GrepTerminalsMaxRows)
		}
		size := 0
		for _, line := range term.Lines {
			size += len(line) + 1
		}
		if size > palette.GrepTerminalsMaxTabBytes {
			t.Fatalf("tab %d has %d bytes, over the %d cap", term.Index, size, palette.GrepTerminalsMaxTabBytes)
		}
	}
}

// TestTerminalSourceLinesKeepsTheNewestRows checks the per-tab crop keeps the tail
// (nearest the live screen) and stops at a row that would exceed the byte budget.
func TestTerminalSourceLinesKeepsTheNewestRows(t *testing.T) {
	t.Parallel()
	rows := make([]string, 0, 600)
	for i := 0; i < 600; i++ {
		rows = append(rows, "row")
	}
	got := terminalSourceLines(rows)
	if len(got) != palette.GrepTerminalsMaxRows {
		t.Fatalf("rows = %d, want %d", len(got), palette.GrepTerminalsMaxRows)
	}
	if got[len(got)-1] != "row" || len(got) == 0 {
		t.Fatalf("cropped rows = %+v", got)
	}
	// A row that alone exceeds the tab byte budget leaves the window empty rather than
	// storing a truncated line.
	over := terminalSourceLines([]string{strings.Repeat("x", palette.GrepTerminalsMaxTabBytes+1)})
	if len(over) != 0 {
		t.Fatalf("over-long row snapshot = %+v", over)
	}
}

// TestOpenTerminalMsgFocusesTheDockTab checks a terminal Live Grep hit shows the dock
// and focuses that tab, starting no shell (the transcript is already open).
func TestOpenTerminalMsgFocusesTheDockTab(t *testing.T) {
	t.Parallel()
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), starter)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{
			{Name: "one", Open: true, Rows: []string{"a"}},
			{Name: "two", Open: true, Rows: []string{"b"}},
		},
	})
	m.Update(palette.OpenTerminalMsg{Index: 1})
	if !m.dock.Open() || m.dock.ActiveIndex() != 1 || !m.dock.Focus() {
		t.Fatalf("dock after the terminal hit = open %v index %d focus %v", m.dock.Open(), m.dock.ActiveIndex(), m.dock.Focus())
	}
	if len(starter.terms) != 0 {
		t.Fatalf("focusing a terminal hit started %d shells", len(starter.terms))
	}
	// A stale index (the tab set changed) leaves the state alone and says so.
	before := m.dock.ActiveIndex()
	m.Update(palette.OpenTerminalMsg{Index: 9})
	if m.dock.ActiveIndex() != before {
		t.Fatal("a stale terminal hit changed the active dock tab")
	}
	if !strings.Contains(m.notice, "no longer open") {
		t.Fatalf("stale terminal hit notice = %q", m.notice)
	}
}

// TestLiveGrepTerminalExportFocusesDockTab: a terminal hit exported with Alt+Q is a list
// row that focuses the existing dock tab, not an attempt to open a file of that name.
func TestLiveGrepTerminalExportFocusesDockTab(t *testing.T) {
	t.Parallel()
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), starter)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: false,
		Tabs: []terminal.SavedTab{
			{Name: "one", Open: true, Rows: []string{"a"}},
			{Name: "shell", Open: true, Rows: []string{"todo terminal"}},
		},
	})
	cmd := m.openLiveGrepResultsList(palette.GrepExportMsg{
		Root:  "/ws",
		Query: "todo",
		Hits: []palette.GrepHit{{
			Source: palette.GrepTerminals, Index: 1, Rel: "shell", Line: 1, Text: "todo terminal",
		}},
	})
	if cmd != nil {
		t.Fatalf("opening the export returned %v", cmd)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want one quickfix list", len(m.overlays))
	}
	// Enter on the only row must focus dock tab 1 and keep the list open.
	feed(m, runKey(m, special(tea.KeyEnter)))
	if !m.dock.Open() || m.dock.ActiveIndex() != 1 {
		t.Fatalf("dock after terminal export Enter = open %v index %d, want tab 1", m.dock.Open(), m.dock.ActiveIndex())
	}
	if len(starter.terms) != 0 {
		t.Fatalf("focusing the exported terminal started %d shells", len(starter.terms))
	}
	if len(m.overlays) != 1 {
		t.Fatal("the quickfix list closed after focusing the terminal")
	}
}

// TestLiveGrepTerminalExportEncoding pins the wire format: terminal hits are t, dock tab
// index, label, transcript line, so the export callback can never mistake them for files.
func TestLiveGrepTerminalExportEncoding(t *testing.T) {
	t.Parallel()
	got := liveGrepExportValue(palette.GrepHit{Source: palette.GrepTerminals, Index: 2, Rel: "shell", Line: 3})
	if got != "t\t2\tshell\t3" {
		t.Fatalf("terminal export value = %q, want t<TAB>2<TAB>shell<TAB>3", got)
	}
}
