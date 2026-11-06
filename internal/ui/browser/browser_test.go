package browser

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// fixture writes a directory with sub/ (holding inner.md), a.go, b.txt and a
// hidden .hid. It returns the directory and a home directory for the "~/" row.
func fixture(t *testing.T) (dir, home string) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "root")
	home = filepath.Join(base, "home")
	must(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(t, os.MkdirAll(home, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "sub", "inner.md"), nil, 0o644))
	for _, name := range []string{"a.go", "b.txt", ".hid"} {
		must(t, os.WriteFile(filepath.Join(dir, name), nil, 0o644))
	}
	return dir, home
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// clock is a fake time source.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func typed(s string) tea.KeyPressMsg {
	r := []rune(s)[0]
	return tea.KeyPressMsg(tea.Key{Code: r, Text: s})
}

func altKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: tea.ModAlt})
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// send feeds msg to b, and returns the command's message when there is one.
func send(t *testing.T, b *Browser, msg tea.Msg) tea.Msg {
	t.Helper()
	next, cmd := b.Update(msg)
	if next != b {
		t.Fatal("Update returned a different browser")
	}
	if cmd == nil {
		return nil
	}
	return cmd()
}

// names returns the row names of b.
func names(b *Browser) []string {
	out := make([]string, 0, len(b.Rows()))
	for _, r := range b.Rows() {
		out = append(out, r.Name)
	}
	return out
}

// selectName moves the selection to the row called name.
func selectName(t *testing.T, b *Browser, name string) {
	t.Helper()
	i := slices.Index(names(b), name)
	if i < 0 {
		t.Fatalf("no row %q in %v", name, names(b))
	}
	b.jump(i)
}

func TestRowsOrderAndSpecials(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	want := []string{"../", "/", "~/", "sub/", "a.go", "b.txt"}
	if got := names(b); !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	if !b.Active() || b.Dir() != dir {
		t.Errorf("browser inactive or wrong dir %q", b.Dir())
	}
	if b.Rows()[3].Kind != RowDir || b.Rows()[4].Kind != RowFile {
		t.Errorf("row kinds %v %v", b.Rows()[3].Kind, b.Rows()[4].Kind)
	}
}

func TestRootHasNoParentRow(t *testing.T) {
	t.Parallel()
	b := New(Options{Dir: string(filepath.Separator), Home: t.TempDir()})
	if got := b.Rows()[0].Kind; got == RowParent {
		t.Errorf("the root directory listed a parent row")
	}
}

func TestHomeRowUsesOption(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	for _, r := range b.Rows() {
		if r.Kind == RowHome && r.Path != home {
			t.Errorf("home row points at %q, want %q", r.Path, home)
		}
	}
	if !slices.Contains(names(b), "~/") {
		t.Errorf("no home row: %v", names(b))
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	send(t, b, typed("A"))
	if got := names(b); !slices.Equal(got, []string{"a.go"}) {
		t.Errorf("filter A gave %v, want only a.go", got)
	}
	if b.Input() != "A" {
		t.Errorf("input %q", b.Input())
	}
	send(t, b, key(tea.KeyBackspace))
	if got := names(b); len(got) != 6 {
		t.Errorf("backspace did not restore the rows: %v", got)
	}
}

func TestBackspaceGoesToParentWhenEmpty(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	send(t, b, key(tea.KeyBackspace))
	if b.Dir() != filepath.Dir(dir) {
		t.Errorf("backspace went to %q, want %q", b.Dir(), filepath.Dir(dir))
	}
}

func TestEnterOnDirectoryEntersIt(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	send(t, b, typed("s"))
	if got := send(t, b, key(tea.KeyEnter)); got != nil {
		t.Errorf("entering a directory sent %+v", got)
	}
	if b.Dir() != filepath.Join(dir, "sub") || b.Input() != "" {
		t.Errorf("after enter dir %q input %q", b.Dir(), b.Input())
	}
	if got := names(b); !slices.Equal(got, []string{"../", "/", "~/", "inner.md"}) {
		t.Errorf("rows in sub %v", got)
	}
	if !b.Active() {
		t.Error("entering a directory closed the browser")
	}
}

func TestEnterOnFileOpens(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	selectName(t, b, "a.go")
	got := send(t, b, key(tea.KeyEnter))
	if want := (Open{Path: filepath.Join(dir, "a.go")}); got != want {
		t.Errorf("enter on a file sent %+v, want %+v", got, want)
	}
	if b.Active() {
		t.Error("browser still active after a file was chosen")
	}
	if _, cmd := b.Update(key(tea.KeyEnter)); cmd != nil {
		t.Error("closed browser sent a message")
	}
}

func TestTypedLocationOpensAtPosition(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	for _, r := range "a.go:3:2" {
		send(t, b, typed(string(r)))
	}
	got := send(t, b, key(tea.KeyEnter))
	if want := (Open{Path: filepath.Join(dir, "a.go"), Line: 3, Col: 2}); got != want {
		t.Errorf("typed location sent %+v, want %+v", got, want)
	}
}

func TestTypedLocationWithLineOnly(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	abs := filepath.Join(dir, "b.txt")
	for _, r := range abs + ":7" {
		send(t, b, typed(string(r)))
	}
	got := send(t, b, key(tea.KeyEnter))
	if want := (Open{Path: abs, Line: 7, Col: 1}); got != want {
		t.Errorf("absolute path with a line sent %+v, want %+v", got, want)
	}
}

func TestTypedLocationMissingFileDoesNothing(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	for _, r := range "nope.go:3" {
		send(t, b, typed(string(r)))
	}
	if got := send(t, b, key(tea.KeyEnter)); got != nil {
		t.Errorf("missing file sent %+v", got)
	}
	if !b.Active() {
		t.Error("missing file closed the browser")
	}
}

func TestTypedLocationIgnoresDirectory(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	for _, r := range "sub:2" {
		send(t, b, typed(string(r)))
	}
	if _, ok := b.typedLocation(); ok {
		t.Error("a directory was accepted as a location")
	}
}

func TestHiddenToggle(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	if slices.Contains(names(b), ".hid") {
		t.Fatal("hidden entry shown by default")
	}
	send(t, b, altKey('.'))
	if !slices.Contains(names(b), ".hid") {
		t.Errorf("alt+. did not show hidden entries: %v", names(b))
	}
	if b.Notice() != "Show Hidden enabled" {
		t.Errorf("notice %q", b.Notice())
	}
	send(t, b, altKey('.'))
	if slices.Contains(names(b), ".hid") || b.Notice() != "Show Hidden disabled" {
		t.Errorf("second alt+. did not hide: %v notice %q", names(b), b.Notice())
	}
}

func TestFilterStartingWithDotShowsHidden(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	// The filter is a substring match, so "." also matches a.go and b.txt.
	send(t, b, typed("."))
	if got := names(b); !slices.Equal(got, []string{".hid", "a.go", "b.txt"}) {
		t.Errorf("filter . gave %v", got)
	}
	if b.Input() != "." {
		t.Errorf("input %q", b.Input())
	}
}

func TestRightAndTabDescend(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	selectName(t, b, "sub/")
	send(t, b, key(tea.KeyRight))
	if b.Dir() != filepath.Join(dir, "sub") {
		t.Errorf("right did not enter sub: %q", b.Dir())
	}
	send(t, b, key(tea.KeyTab)) // the selection is the first row, the parent
	if b.Dir() != dir {
		t.Errorf("tab on the parent went to %q", b.Dir())
	}
	selectName(t, b, "a.go")
	send(t, b, key(tea.KeyRight))
	if b.Dir() != dir {
		t.Errorf("right on a file changed the directory to %q", b.Dir())
	}
}

func TestEscapeCloses(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	if got := send(t, b, key(tea.KeyEscape)); got != nil {
		t.Errorf("escape sent %+v", got)
	}
	if b.Active() {
		t.Error("escape left the browser open")
	}
}

func TestMoveKeysAndWindow(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	b.Draw(grid.New(40, 9)) // a short window: six rows show in four
	send(t, b, key(tea.KeyDown))
	send(t, b, key(tea.KeyDown))
	if b.sel != 2 {
		t.Errorf("sel %d after two downs", b.sel)
	}
	send(t, b, key(tea.KeyEnd))
	if b.sel != len(b.Rows())-1 {
		t.Errorf("end selected %d", b.sel)
	}
	send(t, b, key(tea.KeyHome))
	if b.sel != 0 {
		t.Errorf("home selected %d", b.sel)
	}
	send(t, b, key(tea.KeyUp))
	if b.sel != 0 {
		t.Errorf("up at the top selected %d", b.sel)
	}
	send(t, b, key(tea.KeyPgDown))
	if b.sel == 0 {
		t.Error("page down did not move")
	}
	send(t, b, key(tea.KeyPgUp))
	if b.sel != 0 {
		t.Errorf("page up selected %d", b.sel)
	}
}

func TestNoMatchesLeavesEnterAlone(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	for _, r := range "zzz" {
		send(t, b, typed(string(r)))
	}
	if len(b.Rows()) != 0 {
		t.Fatalf("rows %v", names(b))
	}
	if got := send(t, b, key(tea.KeyEnter)); got != nil || !b.Active() {
		t.Errorf("enter with no rows sent %+v, active %v", got, b.Active())
	}
	g := grid.New(60, 20)
	b.Draw(g)
	if !strings.Contains(strings.Join(g.Plain(), "\n"), "No matches") {
		t.Error("empty list did not say no matches")
	}
}

func TestFolderModeChoosesDirectory(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home, FolderOnly: true})
	if got := names(b); !slices.Equal(got, []string{"../", "/", "~/", "sub/"}) {
		t.Errorf("folder rows %v", got)
	}
	selectName(t, b, "sub/")
	got := send(t, b, key(tea.KeyEnter))
	if want := (Choose{Path: filepath.Join(dir, "sub")}); got != want {
		t.Errorf("folder enter sent %+v, want %+v", got, want)
	}
	if b.Active() {
		t.Error("folder pick left the picker open")
	}
}

func TestFolderModeParentEntersAndTypedFileIgnored(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home, FolderOnly: true})
	selectName(t, b, "../")
	send(t, b, key(tea.KeyEnter))
	if b.Dir() != filepath.Dir(dir) {
		t.Errorf("parent row went to %q", b.Dir())
	}
	c := New(Options{Dir: dir, Home: home, FolderOnly: true})
	for _, r := range "b.txt:1" {
		send(t, c, typed(string(r)))
	}
	if got := send(t, c, key(tea.KeyEnter)); got != nil {
		t.Errorf("folder picker opened a file: %+v", got)
	}
}

func TestSpecialRowChoosesRootInFolderMode(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home, FolderOnly: true})
	selectName(t, b, "/")
	got := send(t, b, key(tea.KeyEnter))
	if want := (Choose{Path: string(filepath.Separator)}); got != want {
		t.Errorf("root row sent %+v", got)
	}
}

func TestFolderModeTitle(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home, FolderOnly: true})
	g := grid.New(60, 20)
	b.Draw(g)
	if !strings.Contains(strings.Join(g.Plain(), "\n"), "Switch Project") {
		t.Error("folder picker title missing")
	}
}

func TestClickFileSelectsThenOpens(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	clk := &clock{now: time.Unix(1000, 0)}
	b := New(Options{Dir: dir, Home: home, Now: clk.Now})
	g := grid.New(60, 20)
	b.Draw(g)
	i := slices.Index(names(b), "b.txt")
	x, y := b.box.x+3, b.box.y+3+(i-b.top)

	if got := send(t, b, click(x, y)); got != nil {
		t.Errorf("single click on a file sent %+v", got)
	}
	if b.sel != i {
		t.Errorf("click selected %d, want %d", b.sel, i)
	}
	clk.now = clk.now.Add(100 * time.Millisecond)
	got := send(t, b, click(x, y))
	if want := (Open{Path: filepath.Join(dir, "b.txt")}); got != want {
		t.Errorf("double click sent %+v, want %+v", got, want)
	}
}

func TestClickDirectoryEnters(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	clk := &clock{now: time.Unix(1000, 0)}
	b := New(Options{Dir: dir, Home: home, Now: clk.Now})
	g := grid.New(60, 20)
	b.Draw(g)
	i := slices.Index(names(b), "sub/")
	x, y := b.box.x+3, b.box.y+3+(i-b.top)
	send(t, b, click(x, y))
	if b.Dir() != filepath.Join(dir, "sub") {
		t.Errorf("click on sub/ went to %q", b.Dir())
	}
}

func TestClickOutsideIsSwallowed(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	b.Draw(grid.New(60, 20))
	if got := send(t, b, click(0, 0)); got != nil || !b.Active() {
		t.Errorf("outside click sent %+v, active %v", got, b.Active())
	}
	if _, ok := b.rowAt(1000, 1000); ok {
		t.Error("row found far outside the box")
	}
	if got := send(t, b, tea.MouseClickMsg(tea.Mouse{X: 5, Y: 5, Button: tea.MouseRight})); got != nil {
		t.Errorf("right click sent %+v", got)
	}
}

func TestClickBeforeDrawIsIgnored(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	if got := send(t, b, click(10, 10)); got != nil {
		t.Errorf("click before draw sent %+v", got)
	}
}

func TestWheelScrollsWindow(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	for i := range 30 {
		must(t, os.WriteFile(filepath.Join(base, string(rune('a'+i%26))+string(rune('a'+i/26))), nil, 0o644))
	}
	b := New(Options{Dir: base, Home: t.TempDir()})
	b.Draw(grid.New(60, 20))
	if b.vis != maxRows {
		t.Fatalf("window %d, want %d", b.vis, maxRows)
	}
	wheel := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown})
	send(t, b, wheel)
	if b.top != wheelRows {
		t.Errorf("wheel down top %d, want %d", b.top, wheelRows)
	}
	if b.sel < b.top || b.sel >= b.top+b.vis {
		t.Errorf("selection %d left the window [%d, %d)", b.sel, b.top, b.top+b.vis)
	}
	send(t, b, tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	send(t, b, tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if b.top != 0 {
		t.Errorf("wheel up top %d, want 0", b.top)
	}
	send(t, b, tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelRight}))
	if b.top != 0 {
		t.Errorf("sideways wheel moved the window to %d", b.top)
	}
}

func TestDrawBoxText(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	g := grid.New(60, 20)
	b.Draw(g)
	lines := g.Plain()
	text := strings.Join(lines, "\n")
	for _, want := range []string{"Open File", "> ", "../", "sub/", "a.go", "Enter open"} {
		if !strings.Contains(text, want) {
			t.Errorf("frame lacks %q:\n%s", want, text)
		}
	}
	// The box sits above the status row and is centered.
	if b.box.y+3+b.box.h != 18 {
		t.Errorf("bottom border at row %d, want 18", b.box.y+3+b.box.h)
	}
	if got := ansi.StringWidth(lines[b.box.y]); got != 60 {
		t.Errorf("top border row is %d cells", got)
	}
}

func TestDrawTooSmallLeavesFrame(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	g := grid.New(15, 20)
	b.Draw(g)
	if b.box.ok {
		t.Error("box drawn in a 15-column frame")
	}
	g = grid.New(60, 7)
	b.Draw(g)
	if b.box.ok {
		t.Error("box drawn in a 7-row frame")
	}
}

func TestDrawScrollbarInBox(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	for i := range 20 {
		must(t, os.WriteFile(filepath.Join(base, string(rune('a'+i))+".txt"), nil, 0o644))
	}
	styles := style.Styles{}
	b := New(Options{Dir: base, Home: t.TempDir(), Styles: styles})
	g := grid.New(60, 12) // window of six rows
	b.Draw(g)
	if !b.box.ok || b.vis != 6 {
		t.Fatalf("box %+v vis %d", b.box, b.vis)
	}
	x := b.box.x + b.box.w - 2
	if got := g.At(x, b.box.y+3).Text; got != " " {
		t.Errorf("scrollbar cell %q", got)
	}
}

func TestDrawFullBoxRowsStayInside(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	g := grid.New(60, 20)
	b.Draw(g)
	last := b.box.y + 3 + b.box.h
	if g.At(b.box.x, last).Text != "└" || g.At(b.box.x+b.box.w-1, last).Text != "┘" {
		t.Errorf("bottom corners missing on row %d", last)
	}
	if g.At(b.box.x, b.box.y).Text != "┌" {
		t.Error("top corner missing")
	}
}

func TestListErrorShowsNotice(t *testing.T) {
	t.Parallel()
	b := New(Options{
		Dir:  "/tmp/some-dir",
		Home: t.TempDir(),
		List: func(string, explorer.ListOptions) ([]explorer.Entry, error) {
			return nil, errors.New("explorer: list: permission denied")
		},
	})
	if !strings.HasPrefix(b.Notice(), "Explorer: list") {
		t.Errorf("notice %q", b.Notice())
	}
	if got := names(b); slices.Contains(got, "a.go") {
		t.Errorf("entries listed after an error: %v", got)
	}
	if !b.Active() {
		t.Error("list error closed the browser")
	}
}

func TestListOptionsAndDefaultListing(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	var got explorer.ListOptions
	b := New(Options{
		Dir:  dir,
		Home: home,
		List: func(d string, o explorer.ListOptions) ([]explorer.Entry, error) {
			got = o
			return explorer.ListDir(d, o)
		},
	})
	if got.ShowHidden || !got.ShowIgnored {
		t.Errorf("default options %+v, want no hidden and ignored shown", got)
	}
	send(t, b, altKey('.'))
	if !got.ShowHidden {
		t.Errorf("show hidden not passed on: %+v", got)
	}
}

func TestDefaultListerAndWorkingDirectory(t *testing.T) {
	t.Parallel()
	b := New(Options{Home: t.TempDir()})
	if !filepath.IsAbs(b.Dir()) {
		t.Errorf("dir %q is not absolute", b.Dir())
	}
	if len(b.Rows()) == 0 {
		t.Error("no rows for the working directory")
	}
}

func TestTruncateLeft(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"short", 10, "short"},
		{"/a/very/long/path/name.go", 10, "…h/name.go"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := truncateLeft(tt.in, tt.w); got != tt.want {
			t.Errorf("truncateLeft(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

func TestPrintableAndClamp(t *testing.T) {
	t.Parallel()
	if printable("") || printable("\x01") || !printable(" ") || !printable("é") {
		t.Error("printable classification wrong")
	}
	if clamp(5, 0, 3) != 3 || clamp(-1, 0, 3) != 0 || clamp(2, 4, 1) != 4 {
		t.Error("clamp wrong")
	}
}

func TestThumbOf(t *testing.T) {
	t.Parallel()
	if start, length := thumbOf(40, 6, 0); start != 0 || length != 1 {
		t.Errorf("thumbOf(40,6,0) = %d,%d", start, length)
	}
	if start, length := thumbOf(6, 6, 0); start != 0 || length != 6 {
		t.Errorf("thumbOf(6,6,0) = %d,%d", start, length)
	}
	if start, _ := thumbOf(40, 6, 39); start > 5 {
		t.Errorf("thumb past the window: %d", start)
	}
}

func TestDoubleClickWindowExpires(t *testing.T) {
	t.Parallel()
	var c clickCounter
	t0 := time.Unix(0, 0)
	if c.press(t0, 1, 1) != 1 {
		t.Fatal("first press not 1")
	}
	if c.press(t0.Add(time.Second), 1, 1) != 1 {
		t.Error("press after the window still counted")
	}
	if c.press(t0.Add(time.Second+10*time.Millisecond), 2, 1) != 1 {
		t.Error("press in another cell counted")
	}
}

func TestCommandsAreNilWhenIdle(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	if _, cmd := b.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF5})); cmd != nil {
		t.Error("an unbound key sent a command")
	}
	if _, cmd := b.Update(tea.MouseMotionMsg(tea.Mouse{})); cmd != nil {
		t.Error("a motion sent a command")
	}
}

func TestEnterOnEmptyRowsIsSafe(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	b := New(Options{Dir: base, Home: t.TempDir(), List: func(string, explorer.ListOptions) ([]explorer.Entry, error) {
		return nil, nil
	}})
	selectName(t, b, "/")
	b.jump(len(b.Rows()) + 5) // clamped to the last row
	if b.sel != len(b.Rows())-1 {
		t.Errorf("jump past the end selected %d", b.sel)
	}
	b.rows = nil
	if got := send(t, b, key(tea.KeyEnter)); got != nil {
		t.Errorf("enter without rows sent %+v", got)
	}
	b.move(1)
	b.jump(0)
	if _, ok := b.selected(); ok {
		t.Error("selected a row in an empty list")
	}
}

// TestAltSCyclesSortAndKeepsDirectoriesFirst checks the sort orders. Alt+S cycles
// Name, Size and Modified, each ascending, and a directory stays first in every
// order. The directory is larger than the smallest file, so a sort that ignored
// the kind would put it in the middle.
func TestAltSCyclesSortAndKeepsDirectoriesFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "d"), 0o755))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	files := []struct {
		name string
		size int
		age  time.Duration
	}{
		{"a.txt", 30, 3 * time.Hour},
		{"b.txt", 1, 1 * time.Hour},
		{"c.txt", 500, 2 * time.Hour},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		must(t, os.WriteFile(p, make([]byte, f.size), 0o644))
		must(t, os.Chtimes(p, base.Add(f.age), base.Add(f.age)))
	}
	b := New(Options{Dir: dir, Home: filepath.Join(dir, "home")})
	if got, want := entryNames(b), []string{"d/", "a.txt", "b.txt", "c.txt"}; !slices.Equal(got, want) {
		t.Fatalf("Name order = %v, want %v", got, want)
	}
	steps := []struct {
		notice string
		order  []string
	}{
		{"Sort by Size", []string{"d/", "b.txt", "a.txt", "c.txt"}},
		{"Sort by Modified", []string{"d/", "b.txt", "c.txt", "a.txt"}},
		{"Sort by Name", []string{"d/", "a.txt", "b.txt", "c.txt"}},
	}
	for _, s := range steps {
		send(t, b, altKey('s'))
		if got := b.Notice(); got != s.notice {
			t.Errorf("notice = %q, want %q", got, s.notice)
		}
		if got := entryNames(b); !slices.Equal(got, s.order) {
			t.Errorf("%s order = %v, want %v", s.notice, got, s.order)
		}
	}
}

// entryNames returns the names of the directory and file rows, in order, so the
// special rows do not take part in the sort check.
func entryNames(b *Browser) []string {
	var out []string
	for _, r := range b.Rows() {
		if r.Kind == RowDir || r.Kind == RowFile {
			out = append(out, r.Name)
		}
	}
	return out
}
