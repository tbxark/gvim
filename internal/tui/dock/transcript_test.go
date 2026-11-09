package dock

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestRestartKeepsTheTranscriptAndTheCwd(t *testing.T) {
	t.Parallel()
	p, term := historyPanel(t, 3) // history line-1..line-3 and the live-1..4 screen
	p.dir = "/start"
	term.cwd = "/shell/dir"
	starter := &fakeStarter{term: newFakeTerm()}
	p.starter = starter
	p.open = true
	cmd := p.Restart()
	if cmd == nil {
		t.Fatal("Restart returned no command")
	}
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if len(starter.dirs) != 1 || starter.dirs[0] != "/shell/dir" {
		t.Fatalf("restarted in %v, want the OSC 7 directory", starter.dirs)
	}
	if len(p.prior) == 0 {
		t.Fatal("the restart dropped the old transcript")
	}
	var texts []string
	for i := range p.prior {
		texts = append(texts, p.prior[i].Text())
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "line-1") || !strings.Contains(joined, "live-1") {
		t.Fatalf("prior transcript = %q, want history and screen rows", joined)
	}
	if p.Scrolling() {
		t.Fatal("the restarted shell is not live")
	}
}

func TestRestoredTranscriptIsReadOnlyAndDead(t *testing.T) {
	t.Parallel()
	p := newTestPanel(t, &fakeStarter{term: newFakeTerm()})
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion,
		Open:    true,
		Cwd:     "/restored",
		Rows:    []string{"old output", "second row"},
	})
	if !p.Restored() || !p.Scrolling() || !p.Open() {
		t.Fatalf("restored %v scrolling %v open %v, want a shown read-only transcript", p.Restored(), p.Scrolling(), p.Open())
	}
	if got := p.combined(); got != 2 {
		t.Fatalf("combined rows = %d, want the two saved rows", got)
	}
	if title := p.title(); !strings.Contains(title, "restored") {
		t.Fatalf("title = %q, want it to say restored", title)
	}
	// The transcript paints as the pager body.
	g := grid.New(80, 5)
	p.Draw(g, scrollLayout(), style.Styles{})
	rows := g.Plain()
	if !strings.Contains(rows[1], "old output") || !strings.Contains(rows[2], "second row") {
		t.Fatalf("painted rows = %q, want the transcript", rows)
	}
	// Ctrl+Space cannot return to a live screen that does not exist.
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}); !ok || !p.Scrolling() {
		t.Fatalf("Ctrl+Space on a restored transcript: taken %v scrolling %v", ok, p.Scrolling())
	}
	// Restart starts a shell in the saved directory, keeping the transcript above.
	starter := &fakeStarter{term: newFakeTerm()}
	p.starter = starter
	cmd := p.Restart()
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if len(starter.dirs) != 1 || starter.dirs[0] != "/restored" {
		t.Fatalf("restarted in %v, want the saved directory", starter.dirs)
	}
	if p.Restored() || p.Scrolling() || !p.Running() {
		t.Fatalf("after the restart: restored %v scrolling %v running %v", p.Restored(), p.Scrolling(), p.Running())
	}
	if len(p.prior) != 2 {
		t.Fatalf("prior rows = %d, want the restored transcript kept", len(p.prior))
	}
}

func TestRestoredTranscriptCanBeSelectedAndSearched(t *testing.T) {
	t.Parallel()
	p := newTestPanel(t, &fakeStarter{term: newFakeTerm()})
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Rows: []string{"old one", "old two"},
	})
	// Drag over both restored rows and copy the flow.
	p.Mouse(clickAt(0, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if got := copySelectionText(t, p); got != "old one\nold" {
		t.Fatalf("restored selection copy = %q", got)
	}
	// A double click selects the word on a restored row.
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 2)
	if got := copySelectionText(t, p); got != "old" {
		t.Fatalf("restored double click copy = %q", got)
	}
	// The search covers the restored transcript too.
	p.ScrollKey(tea.Key{Code: 'f', Mod: tea.ModCtrl})
	for _, r := range "old" {
		p.ScrollKey(tea.Key{Code: r, Text: string(r)})
	}
	cmd := p.searchInputKey(tea.Key{Code: tea.KeyEnter})
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if got := p.SearchMatches(); got != 2 {
		t.Fatalf("search matches = %d, want both restored rows", got)
	}
}

func TestSavedStateBoundsAndCleansTheTranscript(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	p := newTestPanel(t, starter)
	term := starter.term
	p.dir = "/saved"
	term.history = []string{"old \x1b[31mred\x1b[0m", "row-2"}
	st := p.SavedState()
	if st.Version != terminal.SavedStateVersion || !st.Open || st.Cwd != "/saved" {
		t.Fatalf("saved state = %+v", st)
	}
	if st.Width != 80 {
		t.Fatalf("saved width = %d, want the pane width 80", st.Width)
	}
	for _, row := range st.Rows {
		if strings.ContainsAny(row, "\x1b\x00\n\r") {
			t.Fatalf("saved row %q holds control characters", row)
		}
	}
	if len(st.Rows) == 0 || st.Rows[0] != "old [31mred[0m" {
		t.Fatalf("saved rows = %q, want the cleaned history first", st.Rows)
	}
}

func TestSavedStateKeepsOnlyTheNewestRows(t *testing.T) {
	t.Parallel()
	starter2 := &fakeStarter{term: newFakeTerm()}
	p2 := newTestPanel(t, starter2)
	term := starter2.term
	term.history = make([]string, maxSavedRows+50)
	for i := range term.history {
		term.history[i] = "r"
	}
	st := p2.SavedState()
	if len(st.Rows) > maxSavedRows {
		t.Fatalf("saved %d rows, over the bound %d", len(st.Rows), maxSavedRows)
	}
}

func TestSavedStateOfAClosedDockIsNotOpen(t *testing.T) {
	t.Parallel()
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	if st := p.SavedState(); st.Open || len(st.Rows) != 0 {
		t.Fatalf("a dock with no shell and no transcript saved state: %+v", st)
	}
}

// TestRestoredTranscriptReflowsToTheContentWidth checks the saved-transcript
// reflow: a transcript saved at one width is re-wrapped when the pane shows it at
// another, the dock says it rewrapped, and widening it again keeps the rows.
func TestRestoredTranscriptReflowsToTheContentWidth(t *testing.T) {
	t.Parallel()
	cols := 12
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return cols, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Width: 20,
		Rows: []string{"abcdefghijklmnopqrst"},
	})
	p.Sync()
	if !p.reflowed {
		t.Fatal("the restored transcript was not marked rewrapped")
	}
	if got := p.combined(); got != 2 {
		t.Fatalf("combined rows = %d, want the 20-column row re-wrapped at 12", got)
	}
	if title := p.title(); !strings.Contains(title, "rewrapped") {
		t.Fatalf("title = %q, want it to say rewrapped", title)
	}
	// Widening again keeps the two stored rows: this is a text re-wrap, so rows
	// that became two are not rejoined (documented limitation); the flag stays set.
	cols = 24
	p.Sync()
	if got := p.combined(); got != 2 {
		t.Fatalf("combined rows after widening = %d, want the two stored rows", got)
	}
}

// TestSavedCwdIsCleanedAndBounded checks the persisted cwd is treated as untrusted
// input: control characters are stripped (so a record cannot smuggle them into a
// start) and the field is bounded like launch_dir.
func TestSavedCwdIsCleanedAndBounded(t *testing.T) {
	t.Parallel()
	p := newTestPanel(t, &fakeStarter{term: newFakeTerm()})
	p.dir = "/ws/ok\x1b]0;evil\x07\n"
	if got := p.SavedState().Cwd; got != "/ws/ok]0;evil" {
		t.Fatalf("saved cwd = %q, want the cleaned path", got)
	}
	p.dir = "/" + strings.Repeat("a", maxSavedDirBytes+10)
	if got := len(p.SavedState().Cwd); got != maxSavedDirBytes {
		t.Fatalf("saved cwd length = %d, want %d", got, maxSavedDirBytes)
	}
	// Restoring a hand-edited record applies the same bounds.
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Cwd:  "/restored\x00\x1b",
		Rows: []string{"out"},
	})
	if got := tabs.tabs[0].dir; got != "/restored" {
		t.Fatalf("restored cwd = %q, want the cleaned value", got)
	}
}

// TestReflowReRunsCommittedSearch checks the restored-transcript search survives a
// reflow: re-wrapping changes row indexes, so a committed search is re-run over the
// new rows in Sync (never in Draw) and its matches still point at the query text.
func TestReflowReRunsCommittedSearch(t *testing.T) {
	t.Parallel()
	width := 14
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return width, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	p.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Width: width,
		Rows: []string{"alpha beta gamma delta", "epsilon beta zeta"},
	})
	p.Sync()
	p.scroll.search = "beta"
	for _, msg := range messages(p.searchCmd("beta", false, false)) {
		p.Update(msg)
	}
	if len(p.scroll.matches) == 0 {
		t.Fatalf("initial search matches = %v", p.scroll.matches)
	}
	// A narrower pane re-wraps the saved transcript; Sync must rebuild the matches
	// for the new row indexes.
	width = 7
	cmd := p.Sync()
	rebuilt := false
	for _, msg := range messages(cmd) {
		if _, ok := msg.(searchMsg); ok {
			rebuilt = true
		}
		p.Update(msg)
	}
	if !rebuilt {
		t.Fatal("Sync did not rebuild the committed search after the reflow")
	}
	if p.scroll.search != "beta" {
		t.Fatalf("search after reflow = %q", p.scroll.search)
	}
	if len(p.scroll.matches) == 0 {
		t.Fatalf("no matches after the reflow: rows %v", p.prior)
	}
	for _, abs := range p.scroll.matches {
		text, ok := p.combinedText(abs)
		if !ok || !strings.Contains(text, "beta") {
			t.Fatalf("match at %d is %q, %v (rows %v)", abs, text, ok, p.prior)
		}
	}
	if !strings.Contains(p.title(), "/beta") {
		t.Fatalf("title after the rebuilt search = %q", p.title())
	}
	// Without a committed search a reflow does not schedule search work.
	width = 5
	p.scroll.search = ""
	p.scroll.matches, p.scroll.match = nil, 0
	if msgs := messages(p.Sync()); len(msgs) != 0 {
		t.Fatalf("reflow without a search returned messages %v", msgs)
	}
}
