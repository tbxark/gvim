package dock

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
)

// captureCopy gives the panel a clipboard writer that records the copied text.
func captureCopy(p *Panel) *[]string {
	var copied []string
	p.host.Copy = func(text string) tea.Cmd {
		copied = append(copied, text)
		return nil
	}
	return &copied
}

// copySelectionText runs the selection copy and returns the text it put on the
// clipboard.
func copySelectionText(t *testing.T, p *Panel) string {
	t.Helper()
	copied := captureCopy(p)
	cmd := p.copySelection()
	if cmd == nil {
		t.Fatal("copySelection returned no command")
	}
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if len(*copied) == 0 {
		return ""
	}
	return (*copied)[len(*copied)-1]
}

func TestDragOnLiveEntersScrollbackAndSelects(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello world")
	if p.Scrolling() || p.Selection() {
		t.Fatal("setup: a selection is active before the drag")
	}
	p.Mouse(clickAt(2, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	if p.Selection() {
		t.Fatal("a press without a drag started a selection")
	}
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if !p.Scrolling() || !p.Selection() {
		t.Fatalf("after the drag: scrolling %v selection %v, want both", p.Scrolling(), p.Selection())
	}
	// The press was on the live screen, mapped into the just-opened window.
	if got := copySelectionText(t, p); got != "llo world\nrow" {
		t.Fatalf("copied %q, want the first row from the press and the next row's start", got)
	}
	// The release keeps the selection; a later bare click clears it.
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if !p.Selection() {
		t.Fatal("the release cleared the selection")
	}
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	if p.Selection() {
		t.Fatal("a bare click did not clear the selection")
	}
}

func TestShiftDragSelectsWhileTheShellOwnsTheMouse(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse = true
	p.Mouse(clickAt(1, 1, tea.ModShift, tea.MouseLeft), scrollLayout(), 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft, Mod: tea.ModShift}), scrollLayout(), 1)
	if len(term.mice) != 0 {
		t.Fatalf("a Shift+drag was forwarded: %+v", term.mice)
	}
	if !p.Scrolling() || !p.Selection() {
		t.Fatalf("Shift+drag: scrolling %v selection %v, want both", p.Scrolling(), p.Selection())
	}
}

func TestDragWithTheShellOwningTheMouseIsForwarded(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse = true
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	if len(term.mice) != 1 {
		t.Fatalf("forwarded presses = %+v, want the press", term.mice)
	}
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if p.Selection() || p.Scrolling() {
		t.Fatal("a forwarded drag started a selection")
	}
}

func TestMouseDragSelectsOffMakesLiveDragsInert(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello")
	p.SetEffects(Effects{NoMouseDragSelects: true})
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if p.Scrolling() || p.Selection() {
		t.Fatal("a live drag selected with mouse_drag_selects off")
	}
	// A Shift+drag still selects: the user asked for it.
	p.Mouse(clickAt(1, 1, tea.ModShift, tea.MouseLeft), scrollLayout(), 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft, Mod: tea.ModShift}), scrollLayout(), 1)
	if !p.Selection() {
		t.Fatal("a Shift+drag did not select with mouse_drag_selects off")
	}
}

func TestDoubleAndTripleClickSelectInScrollback(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	// The window starts on the newest page: body row 0 is the screen's first row,
	// "live-1".
	p.Mouse(clickAt(2, 1, 0, tea.MouseLeft), scrollLayout(), 2)
	if !p.Selection() {
		t.Fatal("a double click did not select a word")
	}
	if got := copySelectionText(t, p); got != "live-1" {
		t.Fatalf("double click copied %q, want the word live-1", got)
	}
	p.Mouse(clickAt(2, 1, 0, tea.MouseLeft), scrollLayout(), 3)
	if got := copySelectionText(t, p); got != "live-1" {
		t.Fatalf("triple click copied %q, want the row", got)
	}
}

func TestDoubleClickOnTheLiveGridIsInert(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello world")
	p.Mouse(clickAt(2, 1, 0, tea.MouseLeft), scrollLayout(), 2)
	if p.Selection() || p.Scrolling() {
		t.Fatal("a live double click selected text")
	}
}

func TestSelectionYieldsJumpToEndOnOutput(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if !p.Selection() {
		t.Fatal("setup: no selection")
	}
	p.Update(outputMsg{gen: p.gen})
	if !p.Scrolling() {
		t.Fatal("new output left scrollback while a selection was active")
	}
}

func TestNewOutputReturnsToLiveWithoutSelection(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	if !p.Scrolling() {
		t.Fatal("setup: not in scrollback")
	}
	p.Update(outputMsg{gen: p.gen})
	if p.Scrolling() {
		t.Fatal("new output did not return to the live screen")
	}
}

func TestStayInScrollbackKeepsTheViewOnOutput(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	p.SetEffects(Effects{StayInScrollback: true})
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	p.Update(outputMsg{gen: p.gen})
	if !p.Scrolling() {
		t.Fatal("jump_to_end_on_output = false did not keep the view")
	}
}

func TestShiftPageUpEntersScrollbackFromLive(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello")
	cmd, ok := p.ScrollKey(tea.Key{Code: tea.KeyPgUp, Mod: tea.ModShift})
	if !ok || !p.Scrolling() {
		t.Fatalf("Shift+PageUp: taken %v scrolling %v", ok, p.Scrolling())
	}
	_ = cmd
}

func TestCtrlFOpensSearchAndTogglesOptions(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 1) // focus the dock
	_, _ = p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}), scrollLayout(), 1)
	if _, ok := p.ScrollKey(tea.Key{Code: 'f', Mod: tea.ModCtrl}); !ok {
		t.Fatal("Ctrl+F was not taken")
	}
	if !p.scroll.typing {
		t.Fatal("Ctrl+F did not open the search input")
	}
	p.ScrollKey(tea.Key{Code: 'r', Mod: tea.ModCtrl})
	p.ScrollKey(tea.Key{Code: 'a', Mod: tea.ModCtrl})
	for _, r := range "line" {
		p.ScrollKey(tea.Key{Code: r, Text: string(r)})
	}
	cmd := p.searchInputKey(tea.Key{Code: tea.KeyEnter})
	msgs := messages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("search produced %d messages", len(msgs))
	}
	p.Update(msgs[0])
	title := p.title()
	if !strings.Contains(title, " rx") || !strings.Contains(title, " case") {
		t.Fatalf("title = %q, want the search option markers", title)
	}
}

func TestBadRegexShowsANotice(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	p.Mouse(clickAt(1, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	p.ScrollKey(tea.Key{Code: 'f', Mod: tea.ModCtrl})
	p.ScrollKey(tea.Key{Code: 'r', Mod: tea.ModCtrl})
	p.ScrollKey(tea.Key{Code: '(', Text: "("})
	var notice string
	p.host.Notice = func(text string) { notice = text }
	cmd := p.searchInputKey(tea.Key{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the bad regex produced no command")
	}
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}
	if !strings.Contains(notice, "Bad pattern") {
		t.Fatalf("notice = %q, want a bad-pattern notice", notice)
	}
}

// enterScrollbackView switches a live panel to the scrollback view and applies the
// snapshot commands.
func enterScrollbackView(t *testing.T, p *Panel) {
	t.Helper()
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	if !p.Scrolling() {
		t.Fatal("the scrollback view did not open")
	}
}

// dragMotion sends a left-button drag motion to body area point y (a screen row; it
// may be outside the dock) and returns the command.
func dragMotion(p *Panel, x, y int) tea.Cmd {
	cmd, _ := p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}), scrollLayout(), 1)
	return cmd
}

// runTicks applies the auto-scroll commands until max ticks ran or the chain stops,
// returning how many tick messages were applied.
func runTicks(t *testing.T, p *Panel, cmd tea.Cmd, max int) int {
	t.Helper()
	ran := 0
	for cmd != nil && ran < max {
		msgs := messages(cmd)
		cmd = nil
		for _, msg := range msgs {
			if _, ok := msg.(dragScrollMsg); !ok {
				if next, taken := p.Update(msg); taken && next != nil {
					cmd = next
				}
				continue
			}
			next, taken := p.Update(msg)
			if !taken {
				t.Fatalf("the dock did not take the auto-scroll tick")
			}
			ran++
			cmd = next
		}
	}
	return ran
}

func TestDragAboveTheBandAutoScrollsAndStopsonRelease(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 40)
	enterScrollbackView(t, p)
	startTop := p.ScrollTop()
	if startTop == 0 {
		t.Fatal("setup: the view starts at the top")
	}
	// A press on the body, then a drag up into the title row: above the band.
	p.Mouse(clickAt(2, 2, 0, tea.MouseLeft), scrollLayout(), 1)
	cmd := dragMotion(p, 2, 0)
	if !p.sel.dragging || p.sel.dir != -1 || !p.sel.autoRunning {
		t.Fatalf("drag state = %+v, want an upward auto-scroll", p.sel)
	}
	ran := runTicks(t, p, cmd, 3)
	if ran < 2 {
		t.Fatalf("applied %d auto-scroll ticks, want the repeat to keep running", ran)
	}
	if got := p.ScrollTop(); got != startTop-ran {
		t.Fatalf("scroll top = %d after %d ticks, want %d", got, ran, startTop-ran)
	}
	if p.sel.focus.row != p.ScrollTop() {
		t.Fatalf("the selection focus row = %d, want the window's first row %d", p.sel.focus.row, p.ScrollTop())
	}
	// Releasing outside the dock ends the drag and stops the repeat.
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 0, Button: tea.MouseLeft}), scrollLayout(), 1)
	if p.sel.dragging || p.sel.autoRunning {
		t.Fatalf("after the release: dragging %v auto %v", p.sel.dragging, p.sel.autoRunning)
	}
	if next, taken := p.Update(dragScrollMsg{seq: p.dragSeq}); taken && next != nil {
		t.Fatal("a tick still runs after the release")
	}
}

func TestDragAutoScrollStopsAtTheTop(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 40)
	enterScrollbackView(t, p)
	home, _ := p.ScrollKey(tea.Key{Code: tea.KeyHome})
	applyScroll(t, p, home)
	if p.ScrollTop() != 0 {
		t.Fatal("setup: the view is not at the top")
	}
	p.Mouse(clickAt(2, 2, 0, tea.MouseLeft), scrollLayout(), 1)
	cmd := dragMotion(p, 2, 0)
	runTicks(t, p, cmd, 2)
	if p.ScrollTop() != 0 {
		t.Fatalf("scroll top = %d, want the top to hold", p.ScrollTop())
	}
	if p.sel.autoRunning {
		t.Fatal("the repeat still runs at the top")
	}
}

func TestDragBelowTheBandScrollsDownAndStopsAtTheBottom(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 40)
	enterScrollbackView(t, p)
	home, _ := p.ScrollKey(tea.Key{Code: tea.KeyHome})
	applyScroll(t, p, home)
	// Start one row above the newest window so the repeat reaches the bottom.
	p.scroll.top = max(p.maxTop()-1, 0)
	p.Mouse(clickAt(2, 2, 0, tea.MouseLeft), scrollLayout(), 1)
	cmd := dragMotion(p, 2, 10) // below the dock, which ends at row 4
	if p.sel.dir != 1 {
		t.Fatalf("drag direction = %d, want down", p.sel.dir)
	}
	ran := runTicks(t, p, cmd, 5)
	if p.ScrollTop() != p.maxTop() {
		t.Fatalf("scroll top = %d after %d ticks, want the bottom %d", p.ScrollTop(), ran, p.maxTop())
	}
	if p.sel.autoRunning {
		t.Fatal("the repeat still runs at the bottom")
	}
	if got := p.sel.focus.row; got != min(p.ScrollTop()+max(p.bodyRows(), 1)-1, p.combined()-1) {
		t.Fatalf("the focus row = %d, want the window's last row", got)
	}
}

func TestWordSelectionDragExtendsByWholeWords(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello world")
	enterScrollbackView(t, p)
	// A double click on "world", then a drag left over "hello".
	p.Mouse(clickAt(8, 1, 0, tea.MouseLeft), scrollLayout(), 2)
	if got := copySelectionText(t, p); got != "world" {
		t.Fatalf("double click selected %q, want world", got)
	}
	dragMotion(p, 2, 1)
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 1, Button: tea.MouseLeft}), scrollLayout(), 1)
	if got := copySelectionText(t, p); got != "hello world" {
		t.Fatalf("drag left copied %q, want both whole words", got)
	}
	// A double click on "hello", then a drag right over "world".
	p.Mouse(clickAt(2, 1, 0, tea.MouseLeft), scrollLayout(), 2)
	dragMotion(p, 8, 1)
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 8, Y: 1, Button: tea.MouseLeft}), scrollLayout(), 1)
	if got := copySelectionText(t, p); got != "hello world" {
		t.Fatalf("drag right copied %q, want both whole words", got)
	}
}

func TestTripleClickSelectionDragExtendsWholeRows(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "hello world")
	enterScrollbackView(t, p)
	p.Mouse(clickAt(2, 1, 0, tea.MouseLeft), scrollLayout(), 3)
	if got := copySelectionText(t, p); got != "hello world" {
		t.Fatalf("triple click selected %q, want the row", got)
	}
	dragMotion(p, 2, 2) // down a row
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 2, Button: tea.MouseLeft}), scrollLayout(), 1)
	if got := copySelectionText(t, p); got != "hello world\nrow-2" {
		t.Fatalf("row drag copied %q, want both whole rows", got)
	}
}

// A restored transcript has no shell: dragging below its last row and copying must
// not reach for the missing session.
func TestCopySelectionPastTheEndOfARestoredTranscriptNeedsNoShell(t *testing.T) {
	t.Parallel()
	var p Panel
	p.SetHost(Host{
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.Restore(terminal.SavedTab{Rows: []string{"old output"}})
	if p.term != nil || !p.Scrolling() {
		t.Fatal("setup: want a restored read-only view without a shell")
	}
	p.Mouse(clickAt(0, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 5, Y: 3, Button: tea.MouseLeft}), scrollLayout(), 1)
	if !p.Selection() {
		t.Fatal("the drag did not select")
	}
	if got := copySelectionText(t, &p); got != "old output" {
		t.Fatalf("copied %q, want the transcript row", got)
	}
}
