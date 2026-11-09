package dock

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	if got := zero.title(); !strings.Contains(got, "(exited)") || strings.Contains(got, "exit 0") {
		t.Fatalf("exit-0 title = %q, want the exit named and the code omitted like Fresh", got)
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
	if got := p.title(); !strings.Contains(got, "(exited)") {
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
	// It is visible WITHOUT saving or a manual Ctrl+Space: the exit opens the
	// read-only transcript automatically (Fresh's read-only exited terminal).
	if !p.Scrolling() {
		t.Fatal("the exited shell did not open its read-only transcript automatically")
	}
	if got := p.title(); !strings.Contains(got, "(exited)") {
		t.Fatalf("auto transcript title = %q, want the exit named", got)
	}
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

// TestExitAutoScrollbackToggleKeepsOutputAndMarker checks the auto-shown exited
// transcript is the frozen output plus exactly one marker, and that Ctrl+Space
// still returns to the frozen live screen and back again without losing either.
func TestExitAutoScrollbackToggleKeepsOutputAndMarker(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(20, "frozen output")
	p := newTestPanel(t, &fakeStarter{term: term})
	p.Update(exitMsg{gen: p.gen})
	if !p.Scrolling() {
		t.Fatal("the exit did not open the read-only transcript")
	}
	// Ctrl+Space returns to the frozen live screen: no marker there, output kept.
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}); !ok || p.Scrolling() {
		t.Fatalf("Ctrl+Space from the auto transcript: taken %v scrolling %v", ok, p.Scrolling())
	}
	l := scrollLayout()
	g := grid.New(l.W, l.H)
	p.Draw(g, l, style.Styles{})
	frozen := strings.Join(g.Plain(), "\n")
	if strings.Contains(frozen, terminalExitMarker) || !strings.Contains(frozen, "frozen output") {
		t.Fatalf("frozen live screen = %q", g.Plain())
	}
	// Ctrl+Space again returns to the transcript with the marker still last.
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}); !ok || !p.Scrolling() {
		t.Fatalf("Ctrl+Space back to the transcript: taken %v scrolling %v", ok, p.Scrolling())
	}
	if got, ok := p.combinedText(p.combined() - 1); !ok || got != terminalExitMarker {
		t.Fatalf("transcript last row = %q, %v", got, ok)
	}
	g = grid.New(l.W, l.H)
	p.Draw(g, l, style.Styles{})
	if rows := g.Plain(); !strings.Contains(strings.Join(rows, "\n"), terminalExitMarker) {
		t.Fatalf("transcript paint = %q, want the marker", rows)
	}
	// One marker survives two saves.
	for range 2 {
		if n := strings.Count(strings.Join(p.SavedState().Rows, "\n"), terminalExitMarker); n != 1 {
			t.Fatalf("saved marker count = %d", n)
		}
	}
}

// TestExitMarkerAppearsWhenTheDockWasHidden checks a shell that exits while the
// dock has no layout rows (hidden) shows the transcript and marker when it is next
// synced/shown, not just after a save.
func TestExitMarkerAppearsWhenTheDockWasHidden(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(20, "hidden output")
	p := newTestPanel(t, &fakeStarter{term: term})
	// Simulate a dock that is not laid out (hidden) at the exit.
	p.SetHost(Host{
		RootDir: func() string { return "/ws" }, Notice: func(string) {},
		ContentSize: func() (int, int) { return 0, 0 }, Focus: func() {},
	})
	p.Hide()
	if _, ok := p.Update(exitMsg{gen: p.gen}); !ok {
		t.Fatal("the exit message was not handled")
	}
	if p.Scrolling() {
		t.Fatal("a dock without layout rows entered scrollback early")
	}
	// Laying it out and syncing opens the transcript automatically.
	p.SetHost(Host{
		RootDir: func() string { return "/ws" }, Notice: func(string) {},
		ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {},
	})
	p.Sync()
	if !p.Scrolling() {
		t.Fatal("the hidden exit did not open the transcript after layout")
	}
	if got, ok := p.combinedText(p.combined() - 1); !ok || got != terminalExitMarker {
		t.Fatalf("transcript last row = %q, %v", got, ok)
	}
	if got := p.title(); !strings.Contains(got, "(exited)") {
		t.Fatalf("title = %q, want the exit named", got)
	}
}

// TestAltScreenExitShowsFrozenFrameAndOneMarker checks the alternate-screen exit:
// there is no history to scroll, but the frozen frame is shown as the read-only
// transcript with Fresh's marker last (the exit is visible instead of hidden behind
// the frozen screen), Ctrl+Space still toggles the frozen frame, and the marker is
// saved exactly once after the frame's output.
func TestAltScreenExitShowsFrozenFrameAndOneMarker(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(20, "alt frame")
	term.alt = true
	p := newTestPanel(t, &fakeStarter{term: term})
	p.Update(exitMsg{gen: p.gen})
	if !p.Scrolling() {
		t.Fatal("an alternate-screen exit did not show its frozen-frame transcript")
	}
	if p.notice != "" {
		t.Fatalf("alternate-screen exit notice = %q, want none", p.notice)
	}
	if got := p.title(); !strings.Contains(got, "(exited)") {
		t.Fatalf("alt exit title = %q, want the exit named", got)
	}
	if got, ok := p.combinedText(p.combined() - 1); !ok || got != terminalExitMarker {
		t.Fatalf("alt transcript last row = %q, %v", got, ok)
	}
	// The frame is not overwritten: output above, marker below, in the pager.
	l := scrollLayout()
	g := grid.New(l.W, l.H)
	p.Draw(g, l, style.Styles{})
	rows := g.Plain()
	frameAt, markerAt := -1, -1
	for i, row := range rows {
		if strings.Contains(row, "alt frame") {
			frameAt = i
		}
		if strings.Contains(row, terminalExitMarker) {
			markerAt = i
		}
	}
	if frameAt < 0 || markerAt < 0 || markerAt <= frameAt {
		t.Fatalf("alt pager rows = %q, want frame then marker", rows)
	}
	// Ctrl+Space returns to the frozen frame (no marker there) and back.
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}); !ok || p.Scrolling() {
		t.Fatalf("Ctrl+Space from the alt transcript: taken %v scrolling %v", ok, p.Scrolling())
	}
	g = grid.New(l.W, l.H)
	p.Draw(g, l, style.Styles{})
	if got := strings.Join(g.Plain(), "\n"); strings.Contains(got, terminalExitMarker) || !strings.Contains(got, "alt frame") {
		t.Fatalf("frozen alt frame = %q", g.Plain())
	}
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}); !ok || !p.Scrolling() {
		t.Fatalf("Ctrl+Space back to the alt transcript: taken %v scrolling %v", ok, p.Scrolling())
	}
	// Saving twice records the frame once and the marker once.
	for range 2 {
		st := p.SavedState()
		joined := strings.Join(st.Rows, "\n")
		if n := strings.Count(joined, terminalExitMarker); n != 1 {
			t.Fatalf("saved alt marker count = %d in %q", n, st.Rows)
		}
		if n := strings.Count(joined, "alt frame"); n != 1 {
			t.Fatalf("saved alt frame count = %d in %q", n, st.Rows)
		}
		if st.Rows[len(st.Rows)-1] != terminalExitMarker {
			t.Fatalf("saved alt rows = %q, want the marker last", st.Rows)
		}
	}
	// A later sync keeps the transcript and adds no notice.
	p.Sync()
	if !p.Scrolling() || p.notice != "" {
		t.Fatalf("after sync: scrolling %v notice %q", p.Scrolling(), p.notice)
	}
}

// TestAltScreenExitRestartAndRestoreKeepOneMarker checks the alternate-screen
// transcript survives a restart and a save/restore round trip: the frozen frame is
// above one marker, the restarted shell continues below it, and a restored read-only
// record does not duplicate either.
func TestAltScreenExitRestartAndRestoreKeepOneMarker(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(20, "alt frame")
	term.alt = true
	starter := &fakeStarter{term: term}
	p := newTestPanel(t, starter)
	p.Update(exitMsg{gen: p.gen})
	st := p.SavedState()
	// Restart from the frozen-frame transcript: the old frame and exactly one marker
	// stay above the new shell's output.
	starter.term = newFakeTerm()
	starter.term.screen = newScreen(20, "new output")
	cmd := p.Restart()
	if cmd == nil {
		t.Fatal("Restart from the alt transcript returned no command")
	}
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if !p.Running() || p.Scrolling() {
		t.Fatalf("after alt restart: running %v scrolling %v", p.Running(), p.Scrolling())
	}
	after := p.SavedState()
	joined := strings.Join(after.Rows, "\n")
	if n := strings.Count(joined, terminalExitMarker); n != 1 {
		t.Fatalf("marker after alt restart = %d in %q", n, after.Rows)
	}
	if n := strings.Count(joined, "alt frame"); n != 1 {
		t.Fatalf("old alt frame after restart = %d in %q", n, after.Rows)
	}
	// A save/restore round trip of the exited alt state keeps one marker and the frame.
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 4 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(st)
	restored := tabs.SavedState()
	joined = strings.Join(restored.Rows, "\n")
	if n := strings.Count(joined, terminalExitMarker); n != 1 || !strings.Contains(joined, "alt frame") {
		t.Fatalf("restored alt rows = %q", restored.Rows)
	}
	if !tabs.Restored() {
		t.Fatal("the restored alt record is not read-only")
	}
}

// TestRestartFromTheAutoShownTranscript checks a restart taken from the automatic
// exited transcript keeps one marker above the new shell and returns to a live view.
func TestRestartFromTheAutoShownTranscript(t *testing.T) {
	t.Parallel()
	old := newFakeTerm()
	old.screen = newScreen(20, "old auto output")
	starter := &fakeStarter{term: old}
	p := newTestPanel(t, starter)
	p.Update(exitMsg{gen: p.gen})
	if !p.Scrolling() {
		t.Fatal("the exit did not open the transcript")
	}
	// The restarted shell is a different session.
	starter.term = newFakeTerm()
	starter.term.screen = newScreen(20, "new live output")
	cmd := p.Restart()
	if cmd == nil {
		t.Fatal("Restart from the auto transcript returned no command")
	}
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if p.Scrolling() || !p.Running() || p.Exited() {
		t.Fatalf("after restart: scrolling %v running %v exited %v", p.Scrolling(), p.Running(), p.Exited())
	}
	rows := p.SavedState().Rows
	joined := strings.Join(rows, "\n")
	if n := strings.Count(joined, terminalExitMarker); n != 1 {
		t.Fatalf("marker count after restart = %d in %q", n, rows)
	}
	if !strings.Contains(joined, "old auto output") {
		t.Fatalf("restart lost the old output: %q", rows)
	}
}

// TestDockShowKeepsTheExitedTranscript checks opening the dock on a shell that
// exited while it was hidden shows the read-only transcript with the marker and
// never spawns a replacement shell (Fresh reopens the read-only terminal).
func TestDockShowKeepsTheExitedTranscript(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(20, "hidden frozen output")
	starter := &fakeStarter{term: term}
	p := newTestPanel(t, starter)
	p.Update(exitMsg{gen: p.gen})
	p.Hide()
	if p.Scrolling() {
		t.Fatal("hiding did not drop the transcript view")
	}
	// Show must not call start() on the exited panel.
	_ = p.Show()
	if p.Running() || !p.Exited() {
		t.Fatalf("reopening the dock restarted the shell: running %v exited %v", p.Running(), p.Exited())
	}
	if len(starter.dirs) != 1 {
		t.Fatalf("starter dirs = %v, want only the original start", starter.dirs)
	}
	if !p.Scrolling() {
		t.Fatal("reopening the dock did not show the exited transcript")
	}
	if got, ok := p.combinedText(p.combined() - 1); !ok || got != terminalExitMarker {
		t.Fatalf("reopened transcript last row = %q, %v", got, ok)
	}
	if got := p.title(); !strings.Contains(got, "(exited)") {
		t.Fatalf("reopened title = %q", got)
	}
}
