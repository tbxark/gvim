package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/theme"
)

// statusViewModel returns a model over a one-line document with the given view
// options, sized wide enough for the whole status row.
func statusViewModel(t *testing.T, opts *ViewOptions) *Model {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, View: opts})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

// statusRowText lays out the model's status row and joins its run texts.
func statusRowText(m *Model) string {
	var b strings.Builder
	for _, r := range m.layout().Status {
		b.WriteString(r.Text)
	}
	return b.String()
}

// TestStatusRowIsTheConfiguredElementLists checks that editor.status_bar drives the
// row: the configured elements paint in order, the separator between them, and an
// element the list does not name is not on the row.
func TestStatusRowIsTheConfiguredElementLists(t *testing.T) {
	m := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar: StatusBarOptions{
			Left:      []string{"{cursor}", "{messages}"},
			Right:     []string{"{language}", "{diagnostics}"},
			Separator: "|",
		},
	})
	v := m.statusView()
	if len(v.Row.Left) != 2 || v.Row.Left[0].Kind != frame.StatusElementCursor || v.Row.Left[1].Kind != frame.StatusElementMessages {
		t.Fatalf("configured left row = %+v", v.Row.Left)
	}
	row := statusRowText(m)
	for _, want := range []string{" Ln 1, Col 1 ", "|", " Text "} {
		if !strings.Contains(row, want) {
			t.Errorf("status row %q has no %q", row, want)
		}
	}
	for _, gone := range []string{" LF ", " UTF-8 ", " LSP ", " Local ", " Palette: Ctrl+P "} {
		if strings.Contains(row, gone) {
			t.Errorf("status row %q paints %q although the configured lists do not name it", row, gone)
		}
	}
	if got := m.layout().Hit(1, m.layout().StatusY); got.Kind != hitStatusSegment {
		t.Errorf("hit on the configured row = %+v", got)
	}
}

// TestStatusRowDefaultsToFreshsLists checks the model a session without configured
// lists builds: Fresh's default element lists, trust pill first.
func TestStatusRowDefaultsToFreshsLists(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	v := m.statusView()
	if len(v.Row.Left) != len(frame.DefaultStatusLeft()) || v.Row.Left[0].Kind != frame.StatusElementTrust {
		t.Errorf("default row = %+v, want Fresh's default left list starting with {trust}", v.Row.Left)
	}
	row := statusRowText(m)
	for _, want := range []string{" Local ", " Ln 1, Col 1 ", " LF ", " ASCII ", " Text ", " Palette: Ctrl+P "} {
		if !strings.Contains(row, want) {
			t.Errorf("default status row %q has no %q", row, want)
		}
	}
}

// TestEmptyConfiguredStatusRowPaintsNothing checks an explicitly empty element list:
// it is a configured empty side, not a request for the defaults.
func TestEmptyConfiguredStatusRowPaintsNothing(t *testing.T) {
	m := statusViewModel(t, &ViewOptions{
		ShowStatusBar: true,
		StatusBar:     StatusBarOptions{Left: []string{}, Right: []string{}},
	})
	if row := statusRowText(m); row != "" {
		t.Errorf("an empty configured status row paints %q", row)
	}
}

// TestMessagesElementOpensTheWarningsList checks Fresh's click on {messages}: gvim
// opens its status log, the warnings list.
func TestMessagesElementOpensTheWarningsList(t *testing.T) {
	m := statusViewModel(t, nil)
	m.notes.keep("a warning from the test")
	var messages *run
	l := m.layout()
	for i := range l.Status {
		if l.Status[i].Kind == hitStatusSegment && l.Status[i].Index == frame.StatusSegmentMessages {
			messages = &l.Status[i]
		}
	}
	if messages == nil {
		t.Fatalf("no messages segment on the status row: %+v", l.Status)
	}
	_, cmd := m.Update(click(messages.X+1, l.StatusY, tea.MouseLeft))
	for _, msg := range deliver(cmd) {
		m.Update(msg)
	}
	if len(m.overlays) == 0 {
		t.Fatal("a click on the status message opened no list")
	}
}

// TestTerminalRestartElementRestartsTheDock checks Fresh's {terminal_restart}: it is
// on the row only while the dock's shell has exited, and a click restarts the shell.
func TestTerminalRestartElementRestartsTheDock(t *testing.T) {
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
	openDock(t, m)
	if got := m.statusView().TerminalRestart; got != "" {
		t.Errorf("a running dock paints %q", got)
	}
	close(starter.terms[0].done)
	dockSettle(t, m, nil)
	if !m.dock.Exited() {
		t.Fatal("precondition: the shell has exited")
	}
	if got := m.statusView().TerminalRestart; got != "⟳ Restart terminal" {
		t.Fatalf("exited dock: {terminal_restart} = %q", got)
	}
	l := m.layout()
	var restart *run
	for i := range l.Status {
		if l.Status[i].Kind == hitStatusSegment && l.Status[i].Index == frame.StatusSegmentTerminalRestart {
			restart = &l.Status[i]
		}
	}
	if restart == nil {
		t.Fatalf("no terminal restart segment on the status row: %+v", l.Status)
	}
	_, cmd := m.Update(click(restart.X+1, l.StatusY, tea.MouseLeft))
	dockSettle(t, m, cmd)
	if len(starter.terms) != 2 || m.dock.Exited() || !m.dock.Running() {
		t.Fatalf("shells = %d, exited = %v; want a new shell running after the click", len(starter.terms), m.dock.Exited())
	}
}
