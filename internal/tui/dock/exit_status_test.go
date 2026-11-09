package dock

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// exitedPanel is a live panel whose shell has just reported exit status code/known.
func exitedPanel(t *testing.T, code int, known bool) *Panel {
	t.Helper()
	term := newFakeTerm()
	term.screen = newScreen(20, "last output")
	p := newTestPanel(t, &fakeStarter{term: term})
	if _, ok := p.Update(exitMsg{gen: p.gen, code: code, codeOK: known}); !ok {
		t.Fatal("the exit message was not handled")
	}
	return p
}

// TestExitStatusTitleAndSavedMarker checks the dock surfaces the exit status in
// its title (non-zero/signal only, like Fresh's restart indicator) and saves the
// Fresh marker after the frozen screen without duplicating it.
func TestExitStatusTitleAndSavedMarker(t *testing.T) {
	t.Parallel()
	zero := exitedPanel(t, 0, true)
	if got := zero.title(); got != "Terminal (exited)" {
		t.Fatalf("exit-0 title = %q, want the code omitted like Fresh", got)
	}
	code := exitedPanel(t, 3, true)
	if got := code.title(); !strings.Contains(got, "exit 3") {
		t.Fatalf("non-zero title = %q", got)
	}
	signal := exitedPanel(t, -1, true)
	if got := signal.title(); !strings.Contains(got, "signal") {
		t.Fatalf("signal title = %q", got)
	}
	if got := code.title(); strings.Contains(got, "exit 0") {
		t.Fatalf("a zero exit printed a code: %q", got)
	}
	// The frozen screen stays before the marker; saving twice does not duplicate it.
	for range 2 {
		st := code.SavedState()
		if !st.ExitKnown || st.ExitCode != 3 {
			t.Fatalf("saved exit status = %+v", st)
		}
		if len(st.Rows) < 2 || st.Rows[len(st.Rows)-1] != terminalExitMarker {
			t.Fatalf("saved rows = %q, want the marker last", st.Rows)
		}
		if !strings.Contains(strings.Join(st.Rows[:len(st.Rows)-1], "\n"), "last output") {
			t.Fatalf("the marker overwrote the frozen output: %q", st.Rows)
		}
		if n := strings.Count(strings.Join(st.Rows, "\n"), terminalExitMarker); n != 1 {
			t.Fatalf("the marker appears %d times in one save", n)
		}
	}
	// An old record (no exit status) keeps the plain exited title.
	p := newTestPanel(t, &fakeStarter{term: newFakeTerm()})
	if _, ok := p.Update(exitMsg{gen: p.gen}); !ok {
		t.Fatal("the exit message was not handled")
	}
	if got := p.title(); got != "Terminal (exited)" {
		t.Fatalf("unknown-status title = %q", got)
	}
}

// TestRestartContinuesBelowTheExitMarker checks a restart captures the old
// transcript plus exactly one marker, then paints the new shell below it; the
// saved record keeps that order.
func TestRestartContinuesBelowTheExitMarker(t *testing.T) {
	t.Parallel()
	oldTerm := newFakeTerm()
	oldTerm.screen = newScreen(20, "old output")
	starter := &fakeStarter{term: newFakeTerm()}
	starter.term.screen = newScreen(20, "new output")
	p := newTestPanel(t, starter)
	// The panel currently holds the old shell; the starter will hand the new one on
	// restart. (The fake starter returns its single term, so swap the live term.)
	p.term = oldTerm
	if _, ok := p.Update(exitMsg{gen: p.gen, code: 2, codeOK: true}); !ok {
		t.Fatal("the exit message was not handled")
	}
	cmd := p.Restart()
	if cmd == nil {
		t.Fatal("Restart returned no command")
	}
	if msg := cmd(); msg != nil {
		if _, ok := p.Update(msg); !ok {
			t.Fatalf("the restart start was not taken (%T)", msg)
		}
	}
	if !p.Running() {
		t.Fatal("the restart did not start a shell")
	}
	if len(p.prior) < 2 || p.prior[len(p.prior)-1].Text() != terminalExitMarker {
		t.Fatalf("restart prior = %+v, want the transcript then one marker", p.prior)
	}
	if n := strings.Count(priorText(p.prior), terminalExitMarker); n != 1 {
		t.Fatalf("the restart duplicated the marker: %d", n)
	}
	st := p.SavedState()
	if got := strings.Join(st.Rows, "\n"); !strings.Contains(got, "old output") || !strings.Contains(got, "new output") {
		t.Fatalf("saved rows lost an output side: %q", got)
	}
	if n := strings.Count(strings.Join(st.Rows, "\n"), terminalExitMarker); n != 1 {
		t.Fatalf("saved rows hold %d markers, want one", n)
	}
	// The marker stays between the old and the new output.
	joined := strings.Join(st.Rows, "\n")
	if strings.Index(joined, "old output") > strings.Index(joined, terminalExitMarker) ||
		strings.Index(joined, terminalExitMarker) > strings.Index(joined, "new output") {
		t.Fatalf("marker order is wrong: %q", joined)
	}
}

// priorText joins a prior transcript for marker counting.
func priorText(prior []terminal.Line) string {
	var b strings.Builder
	for _, line := range prior {
		b.WriteString(line.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

// TestRestoredExitStatusAndMarkerOrder checks a restored exited tab keeps its exit
// status/marker (the marker stays after the styled screen and is not duplicated)
// and that an old record invents neither.
func TestRestoredExitStatusAndMarkerOrder(t *testing.T) {
	t.Parallel()
	screen := &terminal.SavedScreen{
		Cols: 6,
		Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: 'o', Fg: "#ff0000"}, {Rune: 'u'}, {Rune: 't'},
		}}},
	}
	state := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false,
		Rows:     []string{"out", terminalExitMarker},
		Screen:   screen,
		ExitCode: 4, ExitKnown: true,
		Tabs: []terminal.SavedTab{{
			Name: "t", Open: true, Rows: []string{"out", terminalExitMarker},
			Screen: screen, ExitCode: 4, ExitKnown: true,
		}},
	}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(state)
	p := tabs.tabs[0]
	if !strings.Contains(p.title(), "exit 4") {
		t.Fatalf("restored title = %q", p.title())
	}
	if len(p.prior) != 2 || p.prior[1].Text() != terminalExitMarker {
		t.Fatalf("restored prior order = %+v", p.prior)
	}
	// The styled screen still applies to the row before the marker.
	if p.prior[0].Cells[0].Style.Fg == nil {
		t.Fatalf("the restored styled screen was dropped: %+v", p.prior[0].Cells)
	}
	st := tabs.SavedState()
	if !st.Tabs[0].ExitKnown || st.Tabs[0].ExitCode != 4 {
		t.Fatalf("re-saved exit status = %+v", st.Tabs[0])
	}
	if n := strings.Count(strings.Join(st.Rows, "\n"), terminalExitMarker); n != 1 {
		t.Fatalf("restored re-save wrote %d markers", n)
	}
	// A restart from the restored read-only tab keeps the marker once, above the
	// new shell's output.
	cmd := p.Restart()
	if cmd == nil {
		t.Fatal("the restored tab could not restart")
	}
	if msg := cmd(); msg != nil {
		if _, ok := p.Update(msg); !ok {
			t.Fatalf("the restored restart start was not taken (%T)", msg)
		}
	}
	if !p.Running() {
		t.Fatal("the restored tab did not restart")
	}
	after := tabs.SavedState()
	if n := strings.Count(strings.Join(after.Rows, "\n"), terminalExitMarker); n != 1 {
		t.Fatalf("restart from a restored tab wrote %d markers: %q", n, after.Rows)
	}

	// An old record: no exit status, no invented marker.
	old := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"}}
	var oldTabs Tabs
	oldTabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	oldTabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	oldTabs.Restore(old)
	oldPanel := oldTabs.tabs[0]
	if got := oldPanel.title(); !strings.Contains(got, "Scrollback (restored)") || strings.Contains(got, "exit ") {
		t.Fatalf("old-record restored title = %q", got)
	}
	if got := oldPanel.exitedTitle(); got != "Terminal (exited)" {
		t.Fatalf("old-record exited title = %q", got)
	}
	if st := oldTabs.SavedState(); st.ExitKnown || len(st.Rows) != 1 || st.Rows[0] != "out" {
		t.Fatalf("old record gained exit state: %+v", st)
	}
	_ = color.RGBA{}
}

// TestExitMarkerAppearsAtExit checks the marker is part of the scrollback
// transcript immediately when the shell exits (not only on save): it is the last
// combined row, copyable, painted in the pager, and saved exactly once after the
// frozen output.
func TestExitMarkerAppearsAtExit(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(20, "bye now")
	p := newTestPanel(t, &fakeStarter{term: term})
	if _, ok := p.Update(exitMsg{gen: p.gen, code: 3, codeOK: true}); !ok {
		t.Fatal("the exit message was not handled")
	}
	if !p.exitMarkerPending() {
		t.Fatal("the exit marker is not pending at the exit")
	}
	if got, ok := p.combinedText(p.combined() - 1); !ok || got != terminalExitMarker {
		t.Fatalf("last transcript row at exit = %q, %v", got, ok)
	}
	// It is visible in the pager without any save: ordering is output, then marker.
	applyScroll(t, p, p.ToggleScrollback())
	l := scrollLayout()
	g := grid.New(l.W, l.H)
	p.Draw(g, l, style.Styles{})
	rows := g.Plain()
	markerAt, outputAt := -1, -1
	for i, row := range rows {
		if strings.Contains(row, terminalExitMarker) {
			markerAt = i
		}
		if strings.Contains(row, "bye now") {
			outputAt = i
		}
	}
	if markerAt < 0 || outputAt < 0 || markerAt <= outputAt {
		t.Fatalf("marker/output order in the pager: marker=%d output=%d rows=%q", markerAt, outputAt, rows)
	}
	// Saving twice still records one marker after the frozen output.
	for range 2 {
		st := p.SavedState()
		if len(st.Rows) < 2 || st.Rows[len(st.Rows)-1] != terminalExitMarker {
			t.Fatalf("saved rows = %q", st.Rows)
		}
		if n := strings.Count(strings.Join(st.Rows, "\n"), terminalExitMarker); n != 1 {
			t.Fatalf("saved marker count = %d", n)
		}
		if !strings.Contains(strings.Join(st.Rows[:len(st.Rows)-1], "\n"), "bye now") {
			t.Fatalf("saved rows lost the frozen output: %q", st.Rows)
		}
	}
}

// TestExitMarkerNotDuplicatedAfterRestore checks an old/restored transcript whose
// marker is an ordinary row does not gain the synthetic row: the combined
// transcript ends with that row exactly once.
func TestExitMarkerNotDuplicatedAfterRestore(t *testing.T) {
	t.Parallel()
	screen := &terminal.SavedScreen{Cols: 3, Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
		{Rune: 'o'}, {Rune: 'u'}, {Rune: 't'},
	}}}}
	state := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false,
		Rows: []string{"out", terminalExitMarker}, Screen: screen,
	}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(state)
	p := tabs.tabs[0]
	if p.exitMarkerPending() {
		t.Fatal("a restored marker row became a synthetic row too")
	}
	if got, ok := p.combinedText(p.combined() - 1); !ok || got != terminalExitMarker {
		t.Fatalf("restored last row = %q, %v", got, ok)
	}
	st := tabs.SavedState()
	if n := strings.Count(strings.Join(st.Rows, "\n"), terminalExitMarker); n != 1 {
		t.Fatalf("restored re-save marker count = %d (%q)", n, st.Rows)
	}
}
