package palette

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// fakeFiles is a Files whose list and loading flag the test sets.
type fakeFiles struct {
	files   []string
	loading bool
}

func (f *fakeFiles) Snapshot() ([]string, bool) { return f.files, f.loading }

// fakeRanker keeps the files that contain the query, in their order. It records
// the limit it was asked for, and it reports the indexes it was given per path.
type fakeRanker struct {
	limit   int
	indexes map[string][]int
}

func (r *fakeRanker) Rank(query string, files []string, limit int) []Ranked {
	r.limit = limit
	var out []Ranked
	for _, f := range files {
		if strings.Contains(f, query) {
			out = append(out, Ranked{Path: f, Indexes: r.indexes[f]})
		}
	}
	return out
}

func draw(p *Palette, w, h int) *grid.Grid {
	g := grid.New(w, h)
	p.Draw(g)
	return g
}

// rowText returns row y of g with trailing blanks removed.
func rowText(g *grid.Grid, y int) string {
	return strings.TrimRight(g.Plain()[y], " ")
}

// hasRow reports whether some row of g contains s.
func hasRow(g *grid.Grid, s string) bool {
	for _, r := range g.Plain() {
		if strings.Contains(r, s) {
			return true
		}
	}
	return false
}

// typed is a key press that inserts text.
func typed(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: s, Code: []rune(s)[0]})
}

// key is a key press with no text.
func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

// click is a left press at (x, y).
func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// wheel is one notch of the wheel at (0, 0).
func wheel(b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{Button: b})
}

// runCmd runs cmd and returns the message it made, or nil.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// settle runs cmd and feeds each message back to p, as the root does, until the
// palette asks for no more work. It fails the test if that takes more than limit
// steps, so a poll that never ends does not hang the run.
func settle(t *testing.T, p *Palette, cmd tea.Cmd, limit int) {
	t.Helper()
	for range limit {
		if cmd == nil {
			return
		}
		cmd = p.update(cmd())
	}
	if cmd != nil {
		t.Fatalf("palette still asks for work after %d steps", limit)
	}
}

// countingRanker counts the Rank calls, so a test can tell where ranking runs.
type countingRanker struct {
	fakeRanker
	calls atomic.Int32
}

func (r *countingRanker) Rank(query string, files []string, limit int) []Ranked {
	r.calls.Add(1)
	return r.fakeRanker.Rank(query, files, limit)
}

// commandOptions opens the command table with the query and the state.
func commandOptions(query string, st *commands.State) Options {
	return Options{Query: query, Commands: commands.Commands(nil), State: st}
}

// fakeCommands returns n commands named "Cmd 00", "Cmd 01", and so on.
func fakeCommands(n int) []commands.Entry {
	out := make([]commands.Entry, 0, n)
	for i := range n {
		out = append(out, commands.Entry{
			Name:   fmt.Sprintf("Cmd %02d", i),
			Action: fmt.Sprintf("cmd_%02d", i),
			Source: commands.SourceBuiltin,
		})
	}
	return out
}

func TestPromptShowsQueryAndCaretCell(t *testing.T) {
	p := New(commandOptions(">save", nil))
	g := draw(p, 100, 30)
	// The query starts at the name column, so it lines up with the rows above it.
	x := layoutFor(100, 30).nameX
	if got := rowText(g, 29); got != strings.Repeat(" ", x)+">save" {
		t.Fatalf("prompt row = %q, want %q after %d blanks", got, ">save", x)
	}
	if caret := g.At(x+5, 29); !caret.Style.Reverse || caret.Text != " " {
		t.Errorf("caret cell = %+v, want a reversed blank after the query", caret)
	}
}

// TestEveryBoxRowHasBothBorders checks that the item rows are closed by a bar on
// the right, at any width, with rows, with a selection and with an empty list.
func TestEveryBoxRowHasBothBorders(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{{"commands", ">"}, {"no match", ">zzzqqq"}} {
		for _, w := range []int{20, 80, 100, 140} {
			t.Run(fmt.Sprintf("%s %d", tc.name, w), func(t *testing.T) {
				g := draw(New(commandOptions(tc.query, nil)), w, 30)
				geo := layoutFor(w, 30)
				for y := geo.top + 1; y < geo.bottom; y++ {
					if l, r := g.At(geo.left, y).Text, g.At(geo.right, y).Text; l != "│" || r != "│" {
						t.Errorf("row %d borders = %q, %q, want bars", y, l, r)
					}
				}
			})
		}
	}
}

func TestEmptyBoxSaysWhatIsMissing(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		message string
	}{
		{"no command matches", commandOptions(">zzzqqq", nil), "No matching commands"},
		{"no buffer matches", Options{Query: "#zzz", Buffers: []Buffer{{Name: "a.txt"}}}, "No matching buffers"},
		{"file index done", Options{Query: "zzz", Files: &fakeFiles{}, Ranker: &fakeRanker{}}, "No matching files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.opts)
			settle(t, p, p.Init(), 4)
			g := draw(p, 100, 30)
			if !hasRow(g, tt.message) {
				t.Errorf("box does not show %q", tt.message)
			}
		})
	}
}

// A ':' query that is not a line number says so, and Enter keeps the palette open instead of
// closing it without a word (AK-11). "No matching lines" used to be the whole answer.
func TestLineModeExplainsAQueryThatIsNotALine(t *testing.T) {
	tests := []struct {
		query, message string
	}{
		{":abc", "Not a line number: abc"},
		{":x", "Not a line number: x"},
		{":", "Type a line number"},
		{":0", "Line numbers start at 1"},
		{":3:", "Not a line number: 3:"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			p := New(Options{Query: tt.query})
			settle(t, p, p.Init(), 4)
			if !hasRow(draw(p, 100, 30), tt.message) {
				t.Errorf("box does not show %q", tt.message)
			}
			if cmd := p.update(key(tea.KeyEnter)); cmd != nil || !p.Active() {
				t.Errorf("Enter on the message: cmd=%v active=%v, want nil and still open", cmd, p.Active())
			}
		})
	}
	p := New(Options{Query: ":12"})
	settle(t, p, p.Init(), 4)
	if cmd := p.update(key(tea.KeyEnter)); cmd == nil || p.Active() {
		t.Error("a valid line no longer jumps and closes")
	}
}

func TestFileModeWithoutRankerShowsLoading(t *testing.T) {
	p := New(Options{})
	g := draw(p, 100, 30)
	if !hasRow(g, "Loading files…") {
		t.Fatal("file mode without a Ranker does not show the loading row")
	}
	if cmd := p.update(key(tea.KeyEnter)); cmd != nil || p.Active() {
		t.Errorf("Enter on the loading row: cmd=%v active=%v, want nil and closed", cmd, p.Active())
	}
}

func TestFileModeRanksAndOpensUnderRoot(t *testing.T) {
	r := &fakeRanker{}
	files := &fakeFiles{files: []string{"src/a.go", "b.txt", "src/bb.go"}}
	p := New(Options{Root: "/work", Files: files, Ranker: r})
	draw(p, 100, 30)
	settle(t, p, p.Init(), 4)
	for _, c := range "b" {
		settle(t, p, p.update(typed(string(c))), 4)
	}
	if r.limit != fileLimit {
		t.Errorf("ranker limit = %d, want %d", r.limit, fileLimit)
	}
	g := draw(p, 100, 30)
	if !hasRow(g, "b.txt") || !hasRow(g, "src/bb.go") {
		t.Fatalf("matches not shown:\n%s", g)
	}
	cmd := p.update(key(tea.KeyEnter))
	want := OpenFileMsg{Path: "/work/b.txt", Rel: "b.txt"}
	if got := runCmd(cmd); got != want {
		t.Errorf("Enter emitted %#v, want %#v", got, want)
	}
	if p.Active() {
		t.Error("palette still open after a file was chosen")
	}
}

func TestFileRowsUpdateWhenTheIndexFinishes(t *testing.T) {
	files := &fakeFiles{loading: true}
	p := New(Options{Query: "a", Files: files, Ranker: &fakeRanker{}})
	draw(p, 100, 30)
	step := p.update(runCmd(p.Init()))
	if g := draw(p, 100, 30); !hasRow(g, "Loading files…") {
		t.Fatal("running index with no match does not show the loading row")
	}
	if step == nil {
		t.Fatal("running index did not ask for another ranking")
	}
	files.files, files.loading = []string{"a.go"}, false
	settle(t, p, step, 10)
	if g := draw(p, 100, 30); !hasRow(g, "a.go") {
		t.Error("the rows did not pick up the finished index without a key press")
	}
}

func TestFileRankingRunsInACommandNotInUpdateOrDraw(t *testing.T) {
	const n = 50000
	paths := make([]string, n)
	for i := range paths {
		paths[i] = fmt.Sprintf("dir%03d/file%05d.go", i%500, i)
	}
	r := &countingRanker{}
	p := New(Options{Files: &fakeFiles{files: paths}, Ranker: r})
	draw(p, 100, 30)
	if got := r.calls.Load(); got != 0 {
		t.Fatalf("New ranked %d times, want 0 until Init runs", got)
	}
	cmd := p.update(typed("f"))
	draw(p, 100, 30)
	if got := r.calls.Load(); got != 0 {
		t.Fatalf("typing ranked %d times in Update and Draw, want 0", got)
	}
	if cmd == nil {
		t.Fatal("typing a file query returned no ranking command")
	}
	got := cmd()
	msg, ok := got.(FileRankedMsg)
	if !ok {
		t.Fatalf("ranking command returned %T, want FileRankedMsg", got)
	}
	if got := r.calls.Load(); got != 1 {
		t.Fatalf("ranker calls after the command = %d, want 1", got)
	}
	p.update(msg)
	if len(p.rows) == 0 || p.rows[0].kind != rowFile {
		t.Fatalf("result did not become file rows: %+v", p.rows)
	}
}

func TestStaleFileResultIsDropped(t *testing.T) {
	files := &fakeFiles{files: []string{"a.txt", "ab.go"}}
	p := New(Options{Files: files, Ranker: &fakeRanker{}})
	draw(p, 100, 30)
	first := p.update(typed("a"))
	second := p.update(typed("b"))
	p.update(runCmd(second)) // the newer query lands first
	p.update(runCmd(first))  // the older one arrives late and must not win
	if len(p.rows) != 1 || p.rows[0].name != "ab.go" {
		t.Fatalf("rows = %+v, want only ab.go for the query \"ab\"", p.rows)
	}
}

func TestFileRowsStayUntilTheNewResultArrives(t *testing.T) {
	p := New(Options{Files: &fakeFiles{files: []string{"a.txt", "b.txt"}}, Ranker: &fakeRanker{}})
	draw(p, 100, 30)
	settle(t, p, p.Init(), 4)
	cmd := p.update(typed("b"))
	if len(p.rows) != 2 {
		t.Fatalf("rows before the new result = %d, want the 2 from the old query", len(p.rows))
	}
	p.update(runCmd(cmd))
	if len(p.rows) != 1 || p.rows[0].name != "b.txt" {
		t.Fatalf("rows after the result = %+v, want b.txt", p.rows)
	}
}

func TestMatchedRunesAreBold(t *testing.T) {
	r := &fakeRanker{indexes: map[string][]int{"abc.go": {0, 2}}}
	p := New(Options{Query: "", Files: &fakeFiles{files: []string{"abc.go"}}, Ranker: r})
	settle(t, p, p.Init(), 4)
	g := draw(p, 100, 30)
	y := layoutFor(100, 30).top + 1
	name := layoutFor(100, 30).nameX
	for i, bold := range []bool{true, false, true} {
		if got := g.At(name+i, y).Style.Bold; got != bold {
			t.Errorf("cell %d bold = %v, want %v", i, got, bold)
		}
	}
}

func TestCommandEnterRunsTheAction(t *testing.T) {
	st := &commands.State{TextBuffer: true, Editable: true, HasPath: true, Modified: true}
	p := New(commandOptions(">Save File", st))
	draw(p, 100, 30)
	if p.rows[0].action != "save" {
		t.Fatalf("first row action = %q, want save", p.rows[0].action)
	}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != (RunCommandMsg{Action: "save"}) {
		t.Errorf("Enter emitted %#v, want RunCommandMsg save", got)
	}
	if p.Active() {
		t.Error("palette still open after a command ran")
	}
}

func TestDisabledCommandDoesNotRunAndStaysOpen(t *testing.T) {
	p := New(commandOptions(">Save All", &commands.State{TextBuffer: true, Editable: true}))
	draw(p, 100, 30)
	disabled := -1
	for i, r := range p.rows {
		if r.disabled {
			disabled = i
			break
		}
	}
	if disabled < 0 {
		t.Fatal("precondition: no disabled row in the matches")
	}
	p.nav.Select(disabled)
	if cmd := p.update(key(tea.KeyEnter)); cmd != nil || !p.Active() {
		t.Errorf("Enter on a disabled row: cmd=%v active=%v, want nil and open", cmd, p.Active())
	}
	g := draw(p, 100, 30)
	geo := layoutFor(100, 30)
	first, _ := p.nav.Window()
	if y := geo.top + 1 + disabled - first; !g.At(geo.nameX, y).Style.Faint {
		t.Errorf("disabled row at y=%d is not drawn faint", y)
	}
}

func TestEnterWithNoRowsCloses(t *testing.T) {
	p := New(commandOptions(">zzzqqq", nil))
	draw(p, 100, 30)
	if cmd := p.update(key(tea.KeyEnter)); cmd != nil || p.Active() {
		t.Errorf("Enter with no rows: cmd=%v active=%v, want nil and closed", cmd, p.Active())
	}
}

func TestLineModeGoesToLineAndColumn(t *testing.T) {
	p := New(Options{Query: ":12:3"})
	g := draw(p, 100, 30)
	if !hasRow(g, "Go to line 12") {
		t.Fatal("line row not shown")
	}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != (GotoLineMsg{Line: 12, Col: 3}) {
		t.Errorf("Enter emitted %#v, want GotoLineMsg{12, 3}", got)
	}
}

func TestBufferModeActivatesTheMatchingTab(t *testing.T) {
	buffers := []Buffer{{Name: "a.txt"}, {Name: "b.txt", Modified: true}, {Name: ""}}
	p := New(Options{Query: "#b", Buffers: buffers})
	draw(p, 100, 30)
	if len(p.rows) != 1 || p.rows[0].name != "b.txt" {
		t.Fatalf("rows = %+v, want only b.txt", p.rows)
	}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != (ActivateBufferMsg{Index: 1}) {
		t.Errorf("Enter emitted %#v, want ActivateBufferMsg{1}", got)
	}

	p = New(Options{Query: "#name", Buffers: buffers})
	draw(p, 100, 30)
	if len(p.rows) != 1 || p.rows[0].name != "[No Name]" {
		t.Errorf("unnamed buffer rows = %+v, want [No Name]", p.rows)
	}
}

func TestClickRunsTheRowUnderThePointer(t *testing.T) {
	st := &commands.State{TextBuffer: true, Editable: true, HasPath: true, Modified: true}
	p := New(commandOptions(">Save File", st))
	draw(p, 100, 30)
	geo := layoutFor(100, 30)
	got := runCmd(p.update(click(10, geo.top+1)))
	if got != (RunCommandMsg{Action: "save"}) {
		t.Errorf("click emitted %#v, want RunCommandMsg save", got)
	}
	if p.Active() {
		t.Error("palette still open after a click ran a row")
	}
}

func TestClicksOutsideTheRowsDoNotDismiss(t *testing.T) {
	p := New(commandOptions(">Save File", nil))
	draw(p, 100, 30)
	geo := layoutFor(100, 30)
	misses := []tea.MouseClickMsg{
		click(0, 0),            // the menu row, outside the box
		click(50, 2),           // outside the box, above it
		click(10, geo.top),     // the top border
		click(0, geo.top+1),    // the left border of a row
		click(10, geo.bottom),  // the bottom border
		click(10, geo.footY),   // the hint row
		click(10, geo.promptY), // the query line
		tea.MouseClickMsg(tea.Mouse{X: 10, Y: geo.top + 1, Button: tea.MouseRight}),
	}
	for _, m := range misses {
		if cmd := p.update(m); cmd != nil || !p.Active() {
			t.Errorf("click %+v: cmd=%v active=%v, want nil and open", m, cmd, p.Active())
		}
	}
}

func TestClickBeforeDrawChangesNothing(t *testing.T) {
	p := New(commandOptions(">Save File", nil))
	if cmd := p.update(click(10, 17)); cmd != nil || !p.Active() {
		t.Errorf("click before the first Draw: cmd=%v active=%v", cmd, p.Active())
	}
}

func TestWheelScrollsThreeRowsAndKeepsTheHighlight(t *testing.T) {
	p := New(Options{Query: ">Cmd", Commands: fakeCommands(30)})
	draw(p, 100, 30)
	if first, count := p.nav.Window(); first != 0 || count != maxRows {
		t.Fatalf("window = %d+%d, want 0+%d", first, count, maxRows)
	}
	p.update(wheel(tea.MouseWheelDown))
	g := draw(p, 100, 30)
	if first, _ := p.nav.Window(); first != wheelRows {
		t.Errorf("after one notch down, first = %d, want %d", first, wheelRows)
	}
	if !hasRow(g, p.rows[wheelRows].name) {
		t.Errorf("row %d is not in view after the wheel", wheelRows)
	}
	if p.nav.Selected() != 0 {
		t.Errorf("wheel moved the highlight to %d", p.nav.Selected())
	}
	p.update(wheel(tea.MouseWheelUp))
	p.update(wheel(tea.MouseWheelUp))
	draw(p, 100, 30)
	if first, _ := p.nav.Window(); first != 0 {
		t.Errorf("scrolling above the top: first = %d, want 0", first)
	}
}

func TestHoverMovesTheHighlightOnlyOverRows(t *testing.T) {
	p := New(Options{Query: ">Cmd", Commands: fakeCommands(5)})
	draw(p, 100, 30)
	geo := layoutFor(100, 30)
	p.update(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: geo.top + 3}))
	if p.nav.Selected() != 2 {
		t.Errorf("hover on row 2: highlight = %d, want 2", p.nav.Selected())
	}
	p.update(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: 0}))
	if p.nav.Selected() != 2 {
		t.Errorf("hover off the box moved the highlight to %d", p.nav.Selected())
	}
}

func TestArrowKeysMoveAndTypingResetsTheHighlight(t *testing.T) {
	p := New(Options{Query: ">Cmd", Commands: fakeCommands(5)})
	draw(p, 100, 30)
	p.update(key(tea.KeyUp))
	if p.nav.Selected() != 0 {
		t.Errorf("Up at the top moved to %d", p.nav.Selected())
	}
	p.update(key(tea.KeyDown))
	p.update(key(tea.KeyDown))
	if p.nav.Selected() != 2 {
		t.Errorf("two Downs: highlight = %d, want 2", p.nav.Selected())
	}
	p.update(typed("1"))
	if p.nav.Selected() != 0 {
		t.Errorf("typing did not return the highlight to the top: %d", p.nav.Selected())
	}
}

func TestEscIsNotHandledByThePalette(t *testing.T) {
	p := New(commandOptions(">Save", nil))
	draw(p, 100, 30)
	before := p.input.Value()
	if cmd := p.update(key(tea.KeyEscape)); cmd != nil || !p.Active() || p.input.Value() != before {
		t.Errorf("Esc changed the palette: cmd=%v active=%v query=%q", cmd, p.Active(), p.input.Value())
	}
}

func TestPasteInsertsIntoTheQuery(t *testing.T) {
	p := New(commandOptions(">", nil))
	p.update(tea.PasteMsg{Content: "save"})
	if got := p.input.Value(); got != ">save" {
		t.Errorf("query after paste = %q, want >save", got)
	}
}

func TestClosedPaletteIgnoresInput(t *testing.T) {
	p := New(Options{Query: ":3"})
	draw(p, 100, 30)
	p.update(key(tea.KeyEnter))
	if cmd := p.update(typed("x")); cmd != nil || p.Active() {
		t.Errorf("closed palette: cmd=%v active=%v", cmd, p.Active())
	}
	draw(p, 100, 30)
}

func TestSmallFramesDoNotPanic(t *testing.T) {
	for w := 0; w <= 12; w++ {
		for h := 0; h <= 6; h++ {
			p := New(commandOptions(">save", nil))
			draw(p, w, h)
			p.update(click(w/2, h/2))
			p.update(wheel(tea.MouseWheelDown))
			draw(p, w, h)
		}
	}
}

func TestLayoutRules(t *testing.T) {
	tests := []struct {
		w, h     int
		top      int
		keysX    int
		descW    int
		srcOK    bool
		nameX    int
		nameEnd  int
		footerOK bool
	}{
		{100, 30, 16, 67, 0, true, 3, 65, true},
		{80, 24, 10, 47, 0, true, 3, 45, true},
		{40, 15, 1, -1, 0, false, 2, 38, true},
		{32, 15, 1, -1, 0, false, 2, 30, true},
		{60, 3, -1, 41, 0, false, 3, 39, true},
		{120, 40, 26, 53, 32, true, 3, 51, true},
		{200, 50, 36, 109, 55, true, 4, 107, true},
		{20, 10, 0, -1, 0, false, 2, 18, true},
	}
	for _, tt := range tests {
		g := layoutFor(tt.w, tt.h)
		if g.top != tt.top || g.keysX != tt.keysX || g.descW != tt.descW || g.srcOK != tt.srcOK ||
			g.nameX != tt.nameX || g.nameEnd != tt.nameEnd {
			t.Errorf("layoutFor(%d, %d) = %+v, want top=%d keysX=%d descW=%d srcOK=%v nameX=%d nameEnd=%d",
				tt.w, tt.h, g, tt.top, tt.keysX, tt.descW, tt.srcOK, tt.nameX, tt.nameEnd)
		}
	}
}
