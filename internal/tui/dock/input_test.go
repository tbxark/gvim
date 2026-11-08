package dock

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestTokenAt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		col   int
		token string
		ok    bool
	}{
		{"a path under the pointer", "see /tmp/a.go here", 5, "/tmp/a.go", true},
		{"a relative path", "open src/main.go.", 7, "src/main.go", true},
		{"a bare file name", "wrote notes.txt", 8, "notes.txt", true},
		{"the trailing punctuation is trimmed", "read ./README.md, now", 10, "./README.md", true},
		{"a word is not a path", "hello world", 2, "", false},
		{"white space", "a b", 1, "", false},
		{"a quoted path", `open "/tmp/a b.go" now`, 8, "/tmp/a", true},
		{"a column past the end", "short", 20, "", false},
		{"a tilde path", "cd ~/notes.md", 6, "~/notes.md", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token, ok := tokenAt([]rune(tt.text), tt.col)
			if token != tt.token || ok != tt.ok {
				t.Fatalf("tokenAt(%q, %d) = %q, %v; want %q, %v", tt.text, tt.col, token, ok,
					tt.token, tt.ok)
			}
		})
	}
}

func TestResolveLink(t *testing.T) {
	t.Parallel()
	stat := func(paths ...string) func(string) bool {
		return func(path string) bool {
			for _, p := range paths {
				if path == p {
					return true
				}
			}
			return false
		}
	}
	tests := []struct {
		name                    string
		token, termCwd, rootDir string
		stat                    func(string) bool
		want                    string
		ok                      bool
	}{
		{"absolute", "/tmp/a.go", "/term", "/root", stat("/tmp/a.go"), "/tmp/a.go", true},
		{"the terminal cwd first", "src/a.go", "/term", "/root", stat("/term/src/a.go", "/root/src/a.go"), "/term/src/a.go", true},
		{"the editor cwd, when the terminal's does not have it", "src/a.go", "/term", "/root", stat("/root/src/a.go"), "/root/src/a.go", true},
		{"nothing exists", "src/a.go", "/term", "/root", stat(), "", false},
		{"no working directory", "src/a.go", "", "/root", stat(), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resolveLink(tt.token, tt.termCwd, tt.rootDir, tt.stat)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("resolveLink = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// clickAt builds a left press or another button's press at the given cell.
func clickAt(x, y int, mod tea.KeyMod, button tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Mod: mod, Button: button})
}

// livePanel is a panel whose live screen shows text on its first row.
func livePanel(t *testing.T, text string) (*Panel, *fakeTerm) {
	t.Helper()
	term := newFakeTerm()
	cells := make([]terminal.Cell, 4*80)
	for i, r := range []rune(text) {
		if i < 80 {
			cells[i] = terminal.Cell{Rune: r}
		}
	}
	for r := 1; r < 4; r++ {
		for i, ch := range []rune("row-" + itoa(r+1)) {
			cells[r*80+i] = terminal.Cell{Rune: ch}
		}
	}
	term.screen = terminal.NewScreen(4, 80, cells)
	p := newTestPanel(t, &fakeStarter{term: term})
	return p, term
}

func TestCtrlClickOpensAPath(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "see src/a.go now")
	term.cwd = "/term"
	p.host.Stat = func(path string) bool { return path == "/term/src/a.go" }
	colon := strings.Index("see src/a.go now", "src") + 2
	cmd, ok := p.Mouse(clickAt(colon, 1, tea.ModCtrl, tea.MouseLeft), scrollLayout(), 1)
	if !ok || cmd == nil {
		t.Fatalf("Ctrl+Click: taken %v, cmd %v; want a link command", ok, cmd != nil)
	}
	msgs := messages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v, want one", msgs)
	}
	open, ok := msgs[0].(OpenPathMsg)
	if !ok || open.Path != "/term/src/a.go" {
		t.Fatalf("message = %+v, want OpenPathMsg for the terminal cwd path", msgs[0])
	}
}

func TestCtrlClickWithoutAFileShowsANotice(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "see src/a.go now")
	var notice string
	p.host.Notice = func(text string) { notice = text }
	p.host.Stat = func(string) bool { return false }
	cmd, _ := p.Mouse(clickAt(6, 1, tea.ModCtrl, tea.MouseLeft), scrollLayout(), 1)
	if cmd == nil {
		t.Fatal("Ctrl+Click produced no command")
	}
	if _, taken := p.Update(messages(cmd)[0]); !taken {
		t.Fatal("the link miss was not taken")
	}
	if !strings.Contains(notice, "No file at") {
		t.Fatalf("notice = %q, want a no-file notice", notice)
	}
}

func TestCtrlClickIsOffOnTheAltScreen(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "see src/a.go now")
	term.alt = true
	p.host.Stat = func(string) bool { return true }
	cmd, ok := p.Mouse(clickAt(6, 1, tea.ModCtrl, tea.MouseLeft), scrollLayout(), 1)
	if !ok || cmd != nil {
		t.Fatalf("Ctrl+Click on the alt screen: taken %v, cmd %v; want it ignored", ok, cmd != nil)
	}
}

func TestMouseForwardedOnRequest(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse = true
	layout := scrollLayout()
	p.Mouse(clickAt(3, 1, 0, tea.MouseLeft), layout, 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 4, Y: 1, Button: tea.MouseLeft}), layout, 1)
	p.Mouse(tea.MouseReleaseMsg(tea.Mouse{X: 4, Y: 1, Button: tea.MouseLeft}), layout, 1)
	if len(term.mice) != 2 {
		t.Fatalf("forwarded = %+v, want a press and a release (motion needs all-motion)", term.mice)
	}
	if term.mice[0].Kind != terminal.MousePress || term.mice[0].X != 3 || term.mice[0].Y != 0 || term.mice[0].Button != terminal.MouseLeft {
		t.Fatalf("press = %+v, want (3,0) left", term.mice[0])
	}
	if term.mice[1].Kind != terminal.MouseRelease {
		t.Fatalf("second event = %+v, want the release", term.mice[1])
	}
}

func TestMouseMotionForwardedUnderAllMotion(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse, term.allMotion = true, true
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 4, Y: 1, Button: tea.MouseLeft}), scrollLayout(), 1)
	if len(term.mice) != 1 || term.mice[0].Kind != terminal.MouseMotion || term.mice[0].Button != terminal.MouseNone {
		t.Fatalf("forwarded = %+v, want a buttonless motion", term.mice)
	}
}

func TestDragMotionForwardedWithButtonHeld(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse, term.drag = true, true
	layout := scrollLayout()
	p.Mouse(clickAt(3, 1, 0, tea.MouseLeft), layout, 1)
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 4, Y: 1, Button: tea.MouseLeft}), layout, 1)
	if len(term.mice) != 2 {
		t.Fatalf("forwarded = %+v, want a press and a drag", term.mice)
	}
	if term.mice[1].Kind != terminal.MouseMotion || term.mice[1].Button != terminal.MouseLeft {
		t.Fatalf("drag = %+v, want motion with the held button", term.mice[1])
	}
}

func TestShiftBypassesForwarding(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse = true
	p.Mouse(clickAt(3, 1, tea.ModShift, tea.MouseLeft), scrollLayout(), 1)
	if len(term.mice) != 0 {
		t.Fatalf("a Shift+Click was forwarded: %+v", term.mice)
	}
}

func TestMouseNotForwardedWhenTheShellDidNotAsk(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	p.Mouse(clickAt(3, 1, 0, tea.MouseLeft), scrollLayout(), 1)
	if len(term.mice) != 0 {
		t.Fatalf("the mouse was forwarded without reporting: %+v", term.mice)
	}
}

func TestLegacyForwardingOnTheAltScreen(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.alt = true
	p.SetEffects(Effects{MouseForwarding: MouseForwardAltScreen})
	p.Mouse(clickAt(3, 1, tea.ModShift, tea.MouseLeft), scrollLayout(), 1)
	if len(term.mice) != 1 {
		t.Fatalf("legacy forwarding sent %+v, want the event even with Shift", term.mice)
	}
}

func TestNoMouseForwardingInScrollback(t *testing.T) {
	t.Parallel()
	p, term := historyPanel(t, 20)
	term.mouse = true
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	if _, ok := p.Mouse(clickAt(3, 1, 0, tea.MouseLeft), scrollLayout(), 1); !ok {
		t.Fatal("the scrollback press was not taken")
	}
	if len(term.mice) != 0 {
		t.Fatalf("the scrollback view forwarded a mouse event: %+v", term.mice)
	}
}

func TestWheelForwardedWhenTheShellAsked(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse = true
	p.Mouse(tea.MouseWheelMsg(tea.Mouse{X: 3, Y: 1, Button: tea.MouseWheelUp}), scrollLayout(), 1)
	if len(term.mice) != 1 || term.mice[0].Kind != terminal.MouseWheel || term.mice[0].Button != terminal.MouseWheelUp {
		t.Fatalf("forwarded = %+v, want the wheel notch", term.mice)
	}
	if p.Scrolling() {
		t.Fatal("a forwarded wheel entered scrollback")
	}
}

func TestWheelOnAltScreenWithoutReportingSendsArrows(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.alt = true
	p.Mouse(tea.MouseWheelMsg(tea.Mouse{X: 3, Y: 1, Button: tea.MouseWheelUp}), scrollLayout(), 1)
	if len(term.mice) != 0 {
		t.Fatalf("forwarded = %+v, want none", term.mice)
	}
	if len(p.queue) == 0 {
		t.Fatal("no alternate-scroll arrow keys were queued")
	}
}

func TestWheelUpOnTheLiveMainScreenEntersScrollback(t *testing.T) {
	t.Parallel()
	p, _ := historyPanel(t, 20)
	cmd, ok := p.Mouse(tea.MouseWheelMsg(tea.Mouse{X: 3, Y: 1, Button: tea.MouseWheelUp}), scrollLayout(), 1)
	if !ok || !p.Scrolling() {
		t.Fatalf("wheel: taken %v scrolling %v, want the scrollback view", ok, p.Scrolling())
	}
	applyScroll(t, p, cmd)
	if p.ScrollTop() >= p.maxTop() {
		t.Fatalf("top = %d after the wheel, want the window scrolled up from the bottom", p.ScrollTop())
	}
}

func TestTitleRowPressOnlyFocuses(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "hello")
	term.mouse = true
	if _, ok := p.Mouse(clickAt(3, 0, 0, tea.MouseLeft), scrollLayout(), 1); !ok {
		t.Fatal("the title row press was not taken")
	}
	if len(term.mice) != 0 {
		t.Fatalf("the title row press was forwarded: %+v", term.mice)
	}
	if !p.Focus() {
		t.Fatal("the title row press did not focus the dock")
	}
}

func TestSplitLinkTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		token     string
		path      string
		line, col int
		ok        bool
	}{
		{"src/a.go", "", 0, 0, false},
		{"src/a.go:12", "src/a.go", 12, 0, true},
		{"src/a.go:12:3", "src/a.go", 12, 3, true},
		{"a.go:0", "", 0, 0, false},
		{"a.go:", "", 0, 0, false},
		{":12", "", 0, 0, false},
		{"C:\\ws\\a.go:7", "C:\\ws\\a.go", 7, 0, true},
		{"odd:1.go", "", 0, 0, false},
	}
	for _, tt := range tests {
		path, line, col, ok := splitLinkTarget(tt.token)
		if path != tt.path || line != tt.line || col != tt.col || ok != tt.ok {
			t.Errorf("splitLinkTarget(%q) = %q, %d, %d, %v; want %q, %d, %d, %v",
				tt.token, path, line, col, ok, tt.path, tt.line, tt.col, tt.ok)
		}
	}
}

func TestCtrlClickOpensPathAtALineAndColumn(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "see src/a.go:12:3 now")
	term.cwd = "/term"
	p.host.Stat = func(path string) bool { return path == "/term/src/a.go" }
	cmd, ok := p.Mouse(clickAt(10, 1, tea.ModCtrl, tea.MouseLeft), scrollLayout(), 1)
	if !ok || cmd == nil {
		t.Fatalf("Ctrl+Click: taken %v, cmd %v", ok, cmd != nil)
	}
	msgs := messages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", msgs)
	}
	open, ok := msgs[0].(OpenPathMsg)
	if !ok || open.Path != "/term/src/a.go" || open.Line != 12 || open.Col != 3 {
		t.Fatalf("message = %+v, want the path at 12:3", msgs[0])
	}
}

func TestCtrlClickTriesTheWholeTokenForAColonName(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "see odd:1.go:2 now")
	term.cwd = "/term"
	p.host.Stat = func(path string) bool { return path == "/term/odd:1.go:2" }
	cmd, ok := p.Mouse(clickAt(6, 1, tea.ModCtrl, tea.MouseLeft), scrollLayout(), 1)
	if !ok || cmd == nil {
		t.Fatalf("Ctrl+Click: taken %v, cmd %v", ok, cmd != nil)
	}
	msgs := messages(cmd)
	open, ok := msgs[0].(OpenPathMsg)
	if !ok || open.Path != "/term/odd:1.go:2" || open.Line != 0 || open.Col != 0 {
		t.Fatalf("message = %+v, want the whole colon name", msgs[0])
	}
}

// hoverAt moves the pointer over body cell x,row with Ctrl held (the link hover).
func hoverAt(p *Panel, x, y int) {
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Mod: tea.ModCtrl}), scrollLayout(), 1)
}

func TestCtrlHoverUnderlinesTheLinkAndHints(t *testing.T) {
	t.Parallel()
	p, _ := livePanel(t, "see src/a.go now")
	var hinted string
	p.host.Notice = func(text string) { hinted = text }
	hoverAt(p, 8, 1)
	if !p.hover.on || p.hover.token != "src/a.go" || p.hover.row != 0 {
		t.Fatalf("hover = %+v, want the token on the live row", p.hover)
	}
	if !strings.Contains(hinted, "Ctrl+Click to open src/a.go") {
		t.Fatalf("hint = %q", hinted)
	}
	g := grid.New(80, 5)
	p.Draw(g, scrollLayout(), style.Styles{})
	start := strings.Index("see src/a.go now", "src")
	for x := start; x < start+len("src/a.go"); x++ {
		if cell := g.At(x, 1); !cell.Style.Underline {
			t.Fatalf("cell %d is not underlined", x)
		}
	}
	if g.At(start-1, 1).Style.Underline {
		t.Fatal("a cell before the token is underlined")
	}
	// A buttonless motion without Ctrl ends the hover.
	p.Mouse(tea.MouseMotionMsg(tea.Mouse{X: 8, Y: 1}), scrollLayout(), 1)
	if p.hover.on {
		t.Fatal("a plain motion did not clear the hover")
	}
}

func TestHoverClearsOnAClickAndOnTheAltScreen(t *testing.T) {
	t.Parallel()
	p, term := livePanel(t, "see src/a.go now")
	hoverAt(p, 8, 1)
	if !p.hover.on {
		t.Fatal("setup: no hover")
	}
	p.Mouse(clickAt(2, 2, 0, tea.MouseLeft), scrollLayout(), 1)
	if p.hover.on {
		t.Fatal("a press did not clear the hover")
	}
	// The alternate screen is a full-screen program's own drawing: no hover.
	term.alt = true
	hoverAt(p, 8, 1)
	if p.hover.on {
		t.Fatal("the alt screen underlines a link")
	}
}

func TestHoverInTheScrollbackUsesTheAbsoluteRow(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = terminal.NewScreen(4, 80, liveCells())
	term.history = []string{"see src/a.go"}
	p := newTestPanel(t, &fakeStarter{term: term})
	cmd, _ := p.ScrollKey(tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl})
	applyScroll(t, p, cmd)
	// The view starts on the live screen; Home moves it to the oldest history.
	home, _ := p.ScrollKey(tea.Key{Code: tea.KeyHome})
	applyScroll(t, p, home)
	hoverAt(p, 8, 1)
	if !p.hover.on || !p.hover.scroll || p.hover.row != p.scroll.top {
		t.Fatalf("scrollback hover = %+v, want it on the window's first combined row", p.hover)
	}
	g := grid.New(80, 5)
	p.Draw(g, scrollLayout(), style.Styles{})
	start := strings.Index("see src/a.go", "src")
	if !g.At(start, 1).Style.Underline {
		t.Fatal("the scrollback link is not underlined")
	}
}
