package tui

import (
	"fmt"
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
	if got[0].ID == 0 || got[0].ID != m.dock.TabID(0) || got[0].First != 0 {
		t.Fatalf("snapshot identity/first = %+v, want id %d first 0", got[0], m.dock.TabID(0))
	}
	if got[1].ID != m.dock.TabID(1) {
		t.Fatalf("second snapshot id = %d, want %d", got[1].ID, m.dock.TabID(1))
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
	got, first := terminalSourceLines(rows, 10)
	if len(got) != palette.GrepTerminalsMaxRows {
		t.Fatalf("rows = %d, want %d", len(got), palette.GrepTerminalsMaxRows)
	}
	if first != 10+len(rows)-len(got) {
		t.Fatalf("first row = %d, want %d after the tail crop", first, 10+len(rows)-len(got))
	}
	if got[len(got)-1] != "row" || len(got) == 0 {
		t.Fatalf("cropped rows = %+v", got)
	}
	// A row that alone exceeds the tab byte budget leaves the window empty rather than
	// storing a truncated line.
	over, first := terminalSourceLines([]string{strings.Repeat("x", palette.GrepTerminalsMaxTabBytes+1)}, 3)
	if len(over) != 0 || first != 4 {
		t.Fatalf("over-long row snapshot = %+v first %d", over, first)
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
	second := m.dock.TabID(1)
	m.Update(palette.OpenTerminalMsg{ID: second, Row: 0})
	if !m.dock.Open() || m.dock.ActiveIndex() != 1 || !m.dock.Focus() {
		t.Fatalf("dock after the terminal hit = open %v index %d focus %v", m.dock.Open(), m.dock.ActiveIndex(), m.dock.Focus())
	}
	if len(starter.terms) != 0 {
		t.Fatalf("focusing a terminal hit started %d shells", len(starter.terms))
	}
	// The restored tab has no live session, so there is no pager to position: the tab
	// is focused and the notice says the line could not be located (honest fallback).
	if !strings.Contains(m.notice, "cannot be located") {
		t.Fatalf("restored terminal hit notice = %q", m.notice)
	}
	// A stale id (the tab closed) leaves the active tab alone and says so.
	before := m.dock.ActiveIndex()
	m.Update(palette.OpenTerminalMsg{ID: 999, Row: 0})
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

// TestOpenTerminalMsgRevealsTheTranscriptRow checks a terminal hit with a stable id
// and an absolute transcript row focuses the tab and opens the read-only pager at
// that line, starting no shell.
func TestOpenTerminalMsgRevealsTheTranscriptRow(t *testing.T) {
	t.Parallel()
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), starter)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	openDock(t, m)
	waitDock(t, m, "the dock shell to start", func() bool { return m.dock.Running() })
	term := starter.terms[0]
	term.history = make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		term.history = append(term.history, "row")
	}
	id := m.dock.TabID(0)
	m.Update(palette.OpenTerminalMsg{ID: id, Row: 42})
	if !m.dock.Scrolling() {
		t.Fatal("the hit did not open the scrollback pager")
	}
	if got := m.dock.ScrollSelected(); got != 42 {
		t.Fatalf("selected transcript row = %d, want 42", got)
	}
	if len(starter.terms) != 1 {
		t.Fatalf("revealing the line started shells: %d", len(starter.terms))
	}
}

// TestLiveGrepTerminalExportKeepsStableIDAndRow checks Alt+Q's terminal encoding uses
// the stable id and absolute transcript row, and Enter on that list row reveals the
// same place while the list stays open.
func TestLiveGrepTerminalExportKeepsStableIDAndRow(t *testing.T) {
	t.Parallel()
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), starter)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	openDock(t, m)
	waitDock(t, m, "the dock shell to start", func() bool { return m.dock.Running() })
	term := starter.terms[0]
	for i := 0; i < 100; i++ {
		term.history = append(term.history, "row")
	}
	id := m.dock.TabID(0)
	hit := palette.GrepHit{Source: palette.GrepTerminals, TerminalID: id, Index: 0, Rel: "shell",
		Line: 43, TranscriptFirst: 0, Text: "needle"}
	if got, want := liveGrepExportValue(hit), fmt.Sprintf("T\t%d\t42\tshell", id); got != want {
		t.Fatalf("export value = %q, want %q", got, want)
	}
	cmd := m.openLiveGrepResultsList(palette.GrepExportMsg{
		Root: "/ws", Query: "needle", Hits: []palette.GrepHit{hit},
	})
	if cmd != nil {
		t.Fatalf("opening the export returned %v", cmd)
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if !m.dock.Scrolling() || m.dock.ScrollSelected() != 42 {
		t.Fatalf("export Enter did not reveal the row: scrolling %v selected %d", m.dock.Scrolling(), m.dock.ScrollSelected())
	}
	if len(starter.terms) != 1 {
		t.Fatalf("export Enter started shells: %d", len(starter.terms))
	}
	if len(m.overlays) != 1 {
		t.Fatal("the quickfix list closed after revealing the terminal row")
	}
}

// TestTerminalSourcesAfterStartDiscovered checks the root snapshot path does not rely
// on array alignment that a discovered-agent tab could break: a discovered launch gets
// a nonzero stable id and terminalSources returns it without panicking.
func TestTerminalSourcesAfterStartDiscovered(t *testing.T) {
	t.Parallel()
	starter := &shellsStarter{makeTerm: func() *fakeTerm { return newFakeTerm() }}
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), starter)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	cmd, ok := m.dock.StartDiscovered([]string{"/usr/bin/claude", "--x"}, "discovered")
	if !ok || cmd == nil {
		t.Fatalf("StartDiscovered = %v, %v", cmd, ok)
	}
	// Apply the start message directly: a recursive feed would also run the dock's
	// blocking output wait for this plain fake shell. The tab state (and its id) is
	// what this test needs.
	msgs := collect(cmd)
	if len(msgs) == 0 {
		t.Fatal("StartDiscovered produced no start message")
	}
	m.Update(msgs[0])
	got := m.terminalSources()
	if len(got) != 1 || got[0].ID == 0 || got[0].ID != m.dock.TabID(0) {
		t.Fatalf("terminalSources after StartDiscovered = %+v, tab id %d", got, m.dock.TabID(0))
	}
}

// TestOpenTerminalMsgUsesTheIDAfterAClose checks the original wrong-tab bug: a hit
// captured for the third tab must still focus that tab after the first tab closes
// (its snapshot position shifts), not the tab now at the old position; after the
// target itself closes the id is stale and only a notice is shown.
func TestOpenTerminalMsgUsesTheIDAfterAClose(t *testing.T) {
	t.Parallel()
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), starter)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{
			{Name: "a", Open: true, Rows: []string{"a"}},
			{Name: "b", Open: true, Rows: []string{"b"}},
			{Name: "c", Open: true, Rows: []string{"c"}},
		},
	})
	target := m.dock.TabID(2)
	// Close the first tab; c moves from 2 to 1, b from 1 to 0.
	m.dock.SetActive(0)
	m.dock.CloseTab()
	before := m.dock.ActiveIndex()
	m.Update(palette.OpenTerminalMsg{ID: target, Row: 0})
	if got := m.dock.ActiveIndex(); got != 1 || m.dock.Name(got) != "c" {
		t.Fatalf("stable id focused index %d (%q), want index 1 (c); active before %d", got, m.dock.Name(got), before)
	}
	// Closing the target itself makes the id stale: no shifted tab is focused.
	m.dock.SetActive(1)
	m.dock.CloseTab()
	before = m.dock.ActiveIndex()
	m.Update(palette.OpenTerminalMsg{ID: target, Row: 0})
	if m.dock.ActiveIndex() != before {
		t.Fatal("a stale id focused a shifted tab")
	}
	if !strings.Contains(m.notice, "no longer open") {
		t.Fatalf("stale id notice = %q", m.notice)
	}
}
