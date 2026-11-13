package dock

import (
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// scrollLayout is a frame with the dock on rows 0..4: a title row and 4 body rows.
func scrollLayout() frame.Layout {
	return frame.Layout{DockOn: true, W: 80, H: 5, DockX: 0, DockY: 0, DockH: 5, EditorW: 80}
}

// applyScroll runs the commands a scroll action returns and applies the snapshot
// and search messages, until the view has nothing more to fetch. It returns the
// last panel state through the receiver.
func applyScroll(t *testing.T, p *Panel, cmd tea.Cmd) {
	t.Helper()
	for round := 0; cmd != nil && round < 20; round++ {
		msgs := messages(cmd)
		cmd = nil
		for _, msg := range msgs {
			next, ok := p.Update(msg)
			if ok && next != nil {
				cmd = next
			}
		}
	}
}

// historyPanel is a panel with a running shell whose history holds n numbered
// lines, waiting for the first snapshot.
func historyPanel(t *testing.T, n int) (*Panel, *fakeTerm) {
	t.Helper()
	term := newFakeTerm()
	term.screen = terminal.NewScreen(4, 80, liveCells())
	for i := 1; i <= n; i++ {
		term.history = append(term.history, "line-"+itoa(i))
	}
	p := newTestPanel(t, &fakeStarter{term: term})
	return p, term
}

// liveCells are the cells of the fake live screen: the rows live-1 to live-4.
func liveCells() []terminal.Cell {
	cells := make([]terminal.Cell, 4*80)
	for r := 1; r <= 4; r++ {
		for i, ch := range []rune("live-" + itoa(r)) {
			cells[(r-1)*80+i] = terminal.Cell{Rune: ch}
		}
	}
	return cells
}

// itoa formats a small positive number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestCtrlSpaceEntersScrollback(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !ok || !p.Scrolling() {
		t.Fatalf("Ctrl+Space: taken %v scrolling %v, want the scrollback view", ok, p.Scrolling())
	}
	applyScroll(t, p, cmd)
	// 20 history rows and 4 live rows: the window starts at the last page.
	if got := p.ScrollTop(); got != 20 {
		t.Fatalf("top = %d, want 20 (the last page of 4 rows)", got)
	}
	if title := p.title(); !strings.HasPrefix(title, "Scrollback") || !strings.Contains(title, "rows 21-24 of 24") {
		t.Fatalf("title = %q, want the scrollback title and row window", title)
	}
}

func TestCtrlSpaceEntersScrollbackWithoutHistory(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello world")
	cmd, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !ok || !p.Scrolling() {
		t.Fatalf("Ctrl+Space with no history: taken %v scrolling %v, want the screen in the pager", ok, p.Scrolling())
	}
	applyScroll(t, p, cmd)
	if got := p.combined(); got != 4 {
		t.Fatalf("combined rows = %d, want the live screen's four rows", got)
	}
	if title := p.title(); !strings.Contains(title, "rows 1-4 of 4") {
		t.Fatalf("title = %q, want the screen rows", title)
	}
}

func TestScrollbackDrawsHistoryRows(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	g := grid.New(80, 5)
	p.Draw(g, scrollLayout(), style.Styles{})
	rows := g.Plain()
	if !strings.Contains(rows[0], "Scrollback") {
		t.Fatalf("title row = %q, want the scrollback title", rows[0])
	}
	// The window starts on the live screen; Home moves it to the oldest history.
	if got := strings.TrimRight(rows[1], " "); got != "live-1" {
		t.Fatalf("first painted row = %q, want the newest live row", got)
	}
	home, _ := p.ScrollKey(tea.Key{Code: tea.KeyHome})
	applyScroll(t, p, home)
	g = grid.New(80, 5)
	p.Draw(g, scrollLayout(), style.Styles{})
	rows = g.Plain()
	if got := strings.TrimRight(rows[1], " "); got != "line-1" {
		t.Fatalf("first history row = %q, want line-1", got)
	}
	if got := strings.TrimRight(rows[4], " "); got != "line-4" {
		t.Fatalf("last history row = %q, want line-4", got)
	}
	if _, _, ok := p.Caret(scrollLayout()); ok {
		t.Fatal("the scrollback view shows the shell caret")
	}
}

func TestScrollKeysMoveTheWindow(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	p.ScrollKey(tea.Key{Code: tea.KeyUp})
	if got := p.ScrollSelected(); got != 22 {
		t.Fatalf("selected = %d after Up, want 22", got)
	}
	p.ScrollKey(tea.Key{Code: tea.KeyPgUp})
	if got := p.ScrollTop(); got >= 20 {
		t.Fatalf("top = %d after PageUp, want the window moved up", got)
	}
	p.ScrollKey(tea.Key{Code: tea.KeyEnd})
	if got := p.ScrollSelected(); got != 23 {
		t.Fatalf("selected = %d after End, want the last row 23", got)
	}
	p.ScrollKey(tea.Key{Code: tea.KeyHome})
	if got := p.ScrollTop(); got != 0 {
		t.Fatalf("top = %d after Home, want 0", got)
	}
	// Ctrl+Space leaves the view; Esc does too.
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeyEscape}); !ok || p.Scrolling() {
		t.Fatalf("Esc: taken %v scrolling %v, want the live screen", ok, p.Scrolling())
	}
	cmd, _ = p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	if _, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl}); !ok || p.Scrolling() {
		t.Fatalf("Ctrl+Space: taken %v scrolling %v, want the live screen", ok, p.Scrolling())
	}
}

func TestScrollbackCopyLineAndScreen(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	var copied []string
	p.host.Copy = func(text string) tea.Cmd {
		copied = append(copied, text)
		return nil
	}
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	// Scroll to the oldest history, then copy the selected row and the visible
	// rows.
	home, _ := p.ScrollKey(tea.Key{Code: tea.KeyHome})
	applyScroll(t, p, home)
	p.ScrollKey(tea.Key{Code: 'y'})
	if len(copied) != 1 || copied[0] != "line-1" {
		t.Fatalf("copied = %v, want the selected line", copied)
	}
	p.ScrollKey(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	if len(copied) != 2 || copied[1] != "line-1\nline-2\nline-3\nline-4" {
		t.Fatalf("screen copy = %q, want the visible rows", copied)
	}
}

func TestScrollbackSearch(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	// / then the query, then Enter.
	p.ScrollKey(tea.Key{Code: '/'})
	for _, r := range "line-3" {
		p.ScrollKey(tea.Key{Code: r, Text: string(r)})
	}
	cmd, _ = p.ScrollKey(tea.Key{Code: tea.KeyEnter})
	search := messages(cmd)
	if len(search) != 1 {
		t.Fatalf("search produced %d messages, want one", len(search))
	}
	next, _ := p.Update(search[0])
	applyScroll(t, p, next)
	if p.SearchQuery() != "line-3" || p.SearchMatches() != 1 {
		t.Fatalf("search = %q with %d matches, want line-3 with one", p.SearchQuery(), p.SearchMatches())
	}
	if title := p.title(); !strings.Contains(title, "/line-3 1/1") {
		t.Fatalf("title = %q, want the search and its match count", title)
	}
	// The first match (history row 2) is selected and visible.
	if got := p.ScrollSelected(); got != 2 {
		t.Fatalf("selected = %d after the search, want history row 2", got)
	}
}

// shifted is an uppercase letter as Bubble Tea delivers it: the lower-case code with
// Shift, and the typed text.
func shifted(r rune) tea.Key {
	return tea.Key{Code: unicode.ToLower(r), ShiftedCode: r, Mod: tea.ModShift, Text: string(r)}
}

func TestScrollbackSearchTakesShiftedKeys(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	p.ScrollKey(tea.Key{Code: '/'})
	for _, r := range "LINE-1" {
		k := tea.Key{Code: r, Text: string(r)}
		if unicode.IsUpper(r) {
			k = shifted(r)
		}
		p.ScrollKey(k)
	}
	cmd, _ = p.ScrollKey(tea.Key{Code: tea.KeyEnter})
	for _, msg := range messages(cmd) {
		next, _ := p.Update(msg)
		applyScroll(t, p, next)
	}
	if p.SearchQuery() != "LINE-1" || p.SearchMatches() < 2 {
		t.Fatalf("search = %q with %d matches, want LINE-1 with several", p.SearchQuery(), p.SearchMatches())
	}
	first := p.ScrollSelected()
	// Shift+N goes to the previous match, which wraps to the last one.
	cmd, _ = p.ScrollKey(shifted('N'))
	applyScroll(t, p, cmd)
	if p.ScrollSelected() == first {
		t.Fatalf("Shift+N left the selection on row %d", first)
	}
}

func TestScrollbackRefusedOnAltScreen(t *testing.T) {
	t.Parallel()
	p, term := historyPanel(t, 20)
	term.alt = true
	var notice string
	p.host.Notice = func(text string) { notice = text }
	cmd, ok := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !ok || p.Scrolling() || cmd != nil {
		t.Fatalf("Ctrl+Space on the alternate screen: taken %v scrolling %v, want it taken and refused", ok, p.Scrolling())
	}
	if notice != altScrollbackNotice() {
		t.Fatalf("notice = %q, want %q", notice, altScrollbackNotice())
	}
}

func TestScrollbackHidesWhenTheShellEntersTheAltScreen(t *testing.T) {
	t.Parallel()
	p, term := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	if !p.Scrolling() {
		t.Fatal("the dock did not enter scrollback")
	}
	term.alt = true
	p.Update(outputMsg{gen: p.gen})
	if p.Scrolling() {
		t.Fatal("scrollback stayed on the alternate screen")
	}
}

func TestCloseOnExitHidesTheDock(t *testing.T) {
	t.Parallel()
	p, term := historyPanel(t, 20)
	p.SetEffects(Effects{CloseOnExit: true})
	if _, ok := p.Update(exitMsg{gen: p.gen}); !ok {
		t.Fatal("the exit message was not taken")
	}
	_ = term
	if p.Open() || p.Running() {
		t.Fatalf("after the exit: open %v running %v, want the dock closed", p.Open(), p.Running())
	}
}

func TestCloseOnExitOffKeepsTheScreen(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	if _, ok := p.Update(exitMsg{gen: p.gen}); !ok {
		t.Fatal("the exit message was not taken")
	}
	if !p.Open() || p.Running() || !p.Exited() {
		t.Fatalf("after the exit: open %v running %v exited %v", p.Open(), p.Running(), p.Exited())
	}
	if got := p.title(); !strings.Contains(got, "(exited)") {
		t.Fatalf("title = %q, want the exited title", got)
	}
	// The exited shell now shows its read-only transcript (ending in the marker)
	// automatically, like Fresh's read-only exited terminal.
	if !p.Scrolling() {
		t.Fatal("the exited shell did not show its transcript automatically")
	}
}

// A shell that exited while the dock had no rows opens its transcript at the next
// Sync, and that Sync must also run the history fetch the view starts with.
func TestSyncRunsTheHistoryFetchOfTheExitTranscriptItOpens(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	for i := range 10 {
		term.history = append(term.history, "hist-"+itoa(i))
	}
	term.screen = terminal.NewScreen(1, 80, make([]terminal.Cell, 80))
	rows := 4
	var p Panel
	p.SetHost(Host{
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, rows },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: &fakeStarter{term: term}})
	p.SetEffects(Effects{StayInScrollback: true})
	p.Update(p.Show()())
	rows = 0
	p.Update(exitMsg{gen: p.gen})
	if p.Scrolling() {
		t.Fatal("setup: the transcript opened with no rows to show it")
	}
	rows = 4
	cmd := p.Sync()
	if !p.Scrolling() {
		t.Fatal("Sync did not open the exited shell's transcript")
	}
	applyScroll(t, &p, cmd)
	if p.scroll.pending {
		t.Fatal("the history fetch of the opened view was dropped, so it never completes")
	}
	if text, ok := p.combinedText(p.scroll.top, false); !ok || text != "hist-8" {
		t.Fatalf("first visible row = %q, %v; want the fetched hist-8", text, ok)
	}
}

// SearchQuery is the committed scrollback search, or "".
func (p *Panel) SearchQuery() string { return p.scroll.search }

// SearchMatches is the number of rows the scrollback search matches.
func (p *Panel) SearchMatches() int { return len(p.scroll.matches) }

// ToggleScrollback is the Ctrl+Space switch: it enters the scrollback view, or
// returns to the live screen.
func (p *Panel) ToggleScrollback() tea.Cmd {
	if p.scroll.on {
		return p.leaveScrollback()
	}
	return p.enterScrollback()
}
