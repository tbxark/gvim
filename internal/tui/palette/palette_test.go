package palette

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
// palette asks for no more work. A batch (a rank tick and a preview read, say) is run
// one command at a time, as the program's event loop runs it. It fails the test if that
// takes more than limit steps, so a poll that never ends does not hang the run.
func settle(t *testing.T, p *Palette, cmd tea.Cmd, limit int) {
	t.Helper()
	steps := 0
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		if steps >= limit {
			t.Fatalf("palette still asks for work after %d steps", limit)
		}
		steps++
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				run(sub)
			}
			return
		}
		run(p.update(msg))
	}
	run(cmd)
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
	geo := layoutFor(100, 30)
	// The query sits on the card's input row, at the name column, so it lines up with
	// the rows above it. The row is closed by the card's side borders.
	if got := g.At(geo.left, geo.inputY).Text; got != "│" {
		t.Fatalf("input row left border = %q, want a bar", got)
	}
	for i, want := range ">save" {
		if got := g.At(geo.nameX+i, geo.inputY).Text; got != string(want) {
			t.Errorf("query cell %d = %q, want %q", i, got, string(want))
		}
	}
	if caret := g.At(geo.nameX+5, geo.inputY); !caret.Style.Reverse || caret.Text != " " {
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
		click(0, 0),           // the menu row, outside the box
		click(50, 2),          // outside the box, above it
		click(10, geo.top),    // the top border
		click(0, geo.top+1),   // left of the card's border
		click(10, geo.bottom), // the bottom border, with the hints
		click(10, geo.inputY), // the query row
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
		w, h    int
		top     int
		bottom  int // the card is docked to the frame's last row
		inputY  int
		keysX   int
		descW   int
		srcOK   bool
		nameX   int
		nameEnd int
	}{
		{100, 30, 17, 29, 28, 67, 0, true, 3, 65},
		{80, 24, 11, 23, 22, 47, 0, true, 3, 45},
		{40, 15, 2, 14, 13, -1, 0, false, 2, 38},
		{32, 15, 2, 14, 13, -1, 0, false, 2, 30},
		{60, 3, 0, 2, 1, 41, 0, false, 3, 39},
		{120, 40, 27, 39, 38, 53, 32, true, 3, 51},
		{200, 50, 37, 49, 48, 109, 55, true, 4, 107},
		{20, 10, 1, 9, 8, -1, 0, false, 2, 18},
	}
	for _, tt := range tests {
		g := layoutFor(tt.w, tt.h)
		if g.top != tt.top || g.bottom != tt.bottom || g.inputY != tt.inputY ||
			g.keysX != tt.keysX || g.descW != tt.descW || g.srcOK != tt.srcOK ||
			g.nameX != tt.nameX || g.nameEnd != tt.nameEnd {
			t.Errorf("layoutFor(%d, %d) = %+v, want top=%d bottom=%d inputY=%d keysX=%d descW=%d srcOK=%v nameX=%d nameEnd=%d",
				tt.w, tt.h, g, tt.top, tt.bottom, tt.inputY, tt.keysX, tt.descW, tt.srcOK, tt.nameX, tt.nameEnd)
		}
	}
}

// TestTabAcceptsTheSelectedSuggestion covers Fresh's prompt_accept_suggestion in the
// command palette: Tab fills the selected row's value into the query, preserving the
// >/#/: mode prefix, and the palette stays open (Enter remains the run/open key).
func TestTabAcceptsTheSelectedSuggestion(t *testing.T) {
	t.Run("command", func(t *testing.T) {
		p := New(commandOptions(">Save", nil))
		i := p.nav.Selected()
		if i < 0 || i >= len(p.rows) || p.rows[i].disabled {
			t.Fatalf("no enabled command row selected: i=%d rows=%+v", i, p.rows)
		}
		want := ">" + p.rows[i].name
		if cmd := p.update(key(tea.KeyTab)); cmd != nil {
			t.Fatalf("Tab on a command row returned a command: %v", cmd)
		}
		if got := p.input.Value(); got != want {
			t.Fatalf("query after Tab = %q, want %q", got, want)
		}
		if !p.Active() {
			t.Fatal("Tab closed the command palette; only Enter should run the command")
		}
	})

	t.Run("command keeps the selected row", func(t *testing.T) {
		p := New(Options{Query: ">Cmd", Commands: fakeCommands(20)})
		p.update(key(tea.KeyDown))
		p.update(key(tea.KeyDown))
		i := p.nav.Selected()
		want := ">" + p.rows[i].name
		p.update(key(tea.KeyTab))
		if got := p.input.Value(); got != want {
			t.Fatalf("query after Tab = %q, want the selected row %q", got, want)
		}
	})

	t.Run("buffer", func(t *testing.T) {
		p := New(Options{Query: "#al", Buffers: []Buffer{{Name: "alpha"}, {Name: "beta"}}})
		i := p.nav.Selected()
		want := "#" + p.rows[i].name
		p.update(key(tea.KeyTab))
		if got := p.input.Value(); got != want {
			t.Fatalf("query after Tab = %q, want %q", got, want)
		}
	})

	t.Run("line", func(t *testing.T) {
		p := New(Options{Query: ":12:3"})
		p.update(key(tea.KeyTab))
		if got := p.input.Value(); got != ":12:3" {
			t.Fatalf("query after Tab = %q, want the line target :12:3", got)
		}
	})

	t.Run("file", func(t *testing.T) {
		files := &fakeFiles{files: []string{"main.go", "main_test.go", "other.txt"}}
		p := New(Options{Files: files, Ranker: &fakeRanker{}})
		settle(t, p, p.Init(), 4)
		for _, c := range "mai" {
			settle(t, p, p.update(typed(string(c))), 4)
		}
		i := p.nav.Selected()
		if i < 0 || i >= len(p.rows) {
			t.Fatalf("no file row selected: %+v", p.rows)
		}
		want := p.rows[i].rel
		cmd := p.update(key(tea.KeyTab))
		settle(t, p, cmd, 4)
		if got := p.input.Value(); got != want {
			t.Fatalf("query after Tab = %q, want the file %q", got, want)
		}
		if !p.Active() {
			t.Fatal("Tab closed the file palette; it should only accept the suggestion")
		}
	})
}

// TestTabLeavesAMessageOrDisabledRowAlone: a message row (not a suggestion) and a
// command the state does not allow are not accepted.
func TestTabLeavesAMessageOrDisabledRowAlone(t *testing.T) {
	t.Run("message", func(t *testing.T) {
		p := New(Options{Query: ":abc"})
		if cmd := p.update(key(tea.KeyTab)); cmd != nil {
			t.Fatalf("Tab on a message row returned a command: %v", cmd)
		}
		if got := p.input.Value(); got != ":abc" {
			t.Fatalf("query after Tab = %q, want it unchanged", got)
		}
	})
	t.Run("disabled command", func(t *testing.T) {
		st := &commands.State{} // no text buffer: save is unavailable
		p := New(Options{Query: ">Save", Commands: []commands.Entry{{Name: "Save File", Action: "save"}}, State: st})
		if len(p.rows) == 0 || !p.rows[0].disabled {
			t.Fatalf("the save row is not disabled: %+v", p.rows)
		}
		if cmd := p.update(key(tea.KeyTab)); cmd != nil {
			t.Fatalf("Tab on a disabled row returned a command: %v", cmd)
		}
		if got := p.input.Value(); got != ">Save" {
			t.Fatalf("query after Tab = %q, want it unchanged", got)
		}
	})
}

// TestTabIsANoOpInLiveGrep: Fresh's floating Live Grep overlay consumes Tab without
// accepting its opaque suggestion values; the gvim mode matches.
func TestTabIsANoOpInLiveGrep(t *testing.T) {
	p := New(Options{Live: true})
	before := p.input.Value()
	if cmd := p.update(key(tea.KeyTab)); cmd != nil {
		t.Fatalf("Tab in Live Grep returned a command: %v", cmd)
	}
	if got := p.input.Value(); got != before {
		t.Fatalf("Live Grep query after Tab = %q, want %q", got, before)
	}
	if !p.Active() {
		t.Fatal("Tab closed Live Grep")
	}
}

// TestTabFileSuggestionKeepsTheLineTarget: a file query's :line[:col] target survives
// Tab. The path may contain a colon (a Windows drive or a colon in a file name); the
// target is read from the end and the path is not cut. A leading palette mode
// character in the file name is escaped with "./" so the query stays in file mode and
// the ranker sees the unescaped path.
func TestTabFileSuggestionKeepsTheLineTarget(t *testing.T) {
	tests := []struct {
		name  string
		query string
		files []string
		want  string
		path  string
		line  int
		col   int
	}{
		{
			name: "path with target", query: "src/a.go:12:3",
			files: []string{"src/a.go", "src/a_test.go"},
			want:  "src/a.go:12:3", path: "src/a.go", line: 12, col: 3,
		},
		{
			name: "windows drive and target", query: "C:\\repo\\a.go:7",
			files: []string{"C:\\repo\\a.go"},
			want:  "C:\\repo\\a.go:7", path: "C:\\repo\\a.go", line: 7,
		},
		{
			name: "colon in a file name", query: "dir/a:b.go:5:2",
			files: []string{"dir/a:b.go"},
			want:  "dir/a:b.go:5:2", path: "dir/a:b.go", line: 5, col: 2,
		},
		{
			name: "leading mode prefix needs an escape", query: "notes:3",
			files: []string{">notes.txt"},
			want:  "./>notes.txt:3", path: ">notes.txt", line: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Options{Query: tt.query, Files: &fakeFiles{files: tt.files}, Ranker: &fakeRanker{}})
			settle(t, p, p.Init(), 4)
			if len(p.rows) == 0 || p.rows[p.nav.Selected()].kind != rowFile {
				t.Fatalf("no file row selected for %q: %+v", tt.query, p.rows)
			}
			cmd := p.update(key(tea.KeyTab))
			settle(t, p, cmd, 4)
			if got := p.input.Value(); got != tt.want {
				t.Fatalf("query after Tab = %q, want %q", got, tt.want)
			}
			mode, _ := commands.ParseQuery(p.input.Value())
			if mode != commands.ModeFile {
				t.Fatalf("query %q is in mode %v, want file mode", p.input.Value(), mode)
			}
			if p.fileText != tt.path || p.fileLine != tt.line || p.fileCol != tt.col {
				t.Fatalf("parsed file target = %q:%d:%d, want %q:%d:%d", p.fileText, p.fileLine, p.fileCol, tt.path, tt.line, tt.col)
			}
		})
	}
}

// TestTabFileSuggestionWithoutATarget: a file query with no jump target still accepts
// the path; a path whose own name ends in a colon+number is kept verbatim instead of
// being re-read as a jump target, and a leading mode character is escaped.
func TestTabFileSuggestionWithoutATarget(t *testing.T) {
	t.Run("numeric suffix in the path", func(t *testing.T) {
		p := New(Options{Query: "dir/a", Files: &fakeFiles{files: []string{"dir/a:12"}}, Ranker: &fakeRanker{}})
		settle(t, p, p.Init(), 4)
		if len(p.rows) == 0 || p.rows[p.nav.Selected()].kind != rowFile {
			t.Fatalf("no file row selected: %+v", p.rows)
		}
		settle(t, p, p.update(key(tea.KeyTab)), 4)
		if got := p.input.Value(); got != "dir/a:12" {
			t.Fatalf("query after Tab = %q, want the verbatim path dir/a:12", got)
		}
		if p.fileText != "dir/a:12" || p.fileLine != 0 || p.fileCol != 0 {
			t.Fatalf("parsed file = %q:%d:%d, want dir/a:12 with no target", p.fileText, p.fileLine, p.fileCol)
		}
	})
	t.Run("leading mode prefix", func(t *testing.T) {
		p := New(Options{Query: "note", Files: &fakeFiles{files: []string{">notes.txt"}}, Ranker: &fakeRanker{}})
		settle(t, p, p.Init(), 4)
		if len(p.rows) == 0 || p.rows[p.nav.Selected()].kind != rowFile {
			t.Fatalf("no file row selected: %+v", p.rows)
		}
		settle(t, p, p.update(key(tea.KeyTab)), 4)
		if got := p.input.Value(); got != "./>notes.txt" {
			t.Fatalf("query after Tab = %q, want the escaped path ./%s", got, ">notes.txt")
		}
		if p.fileText != ">notes.txt" || p.fileLine != 0 || p.fileCol != 0 {
			t.Fatalf("parsed file = %q:%d:%d, want %q with no target", p.fileText, p.fileLine, p.fileCol, ">notes.txt")
		}
	})
}

// TestTabFileRawPathDoesNotLeakAcrossModes: the verbatim accepted-path bypass is only
// for the accepted file query. Leaving file mode clears it, so returning to a query
// with the same text parses normally again instead of keeping a stale bypass.
func TestTabFileRawPathDoesNotLeakAcrossModes(t *testing.T) {
	p := New(Options{Query: "dir/a", Files: &fakeFiles{files: []string{"dir/a:12"}}, Ranker: &fakeRanker{}})
	settle(t, p, p.Init(), 4)
	settle(t, p, p.update(key(tea.KeyTab)), 4)
	if p.fileRaw != "dir/a:12" {
		t.Fatalf("accepted raw path = %q, want dir/a:12", p.fileRaw)
	}
	// Move to the start, enter command mode, and come back to the same file text.
	settle(t, p, p.update(key(tea.KeyHome)), 4)
	settle(t, p, p.update(typed(">")), 4)
	if got := p.input.Value(); got != ">dir/a:12" {
		t.Fatalf("command-mode query = %q, want >dir/a:12", got)
	}
	if p.fileRaw != "" {
		t.Fatalf("the file bypass survived a mode switch: %q", p.fileRaw)
	}
	settle(t, p, p.update(key(tea.KeyBackspace)), 4)
	if got := p.input.Value(); got != "dir/a:12" {
		t.Fatalf("file-mode query = %q, want dir/a:12", got)
	}
	// With the stale bypass gone, the query parses as the original contract:
	// path dir/a with line 12. (The user would retype the full path to avoid this.)
	if p.fileText != "dir/a" || p.fileLine != 12 {
		t.Fatalf("file parse = %q:%d, want dir/a:12 as path:line without the bypass", p.fileText, p.fileLine)
	}
}

// TestBufferModeUsesTheFuzzyMatcher: buffer mode shares Fresh's BufferProvider rules,
// so whitespace terms match independently in any order and a fuzzy subsequence (not
// only a contiguous substring) matches. Score orders the rows; tab order breaks ties.
func TestBufferModeUsesTheFuzzyMatcher(t *testing.T) {
	buffers := []Buffer{
		{Name: "other.go"},
		{Name: "my file.txt"},
		{Name: "save_file.go"},
		{Name: "prefix_my_file.log"},
	}
	tests := []struct {
		query string
		first string
	}{
		{"#file save", "save_file.go"},
		{"#save file", "save_file.go"},
		{"#my file", "my file.txt"},
		{"#sflg", "save_file.go"},
	}
	for _, tt := range tests {
		p := New(Options{Query: tt.query, Buffers: buffers})
		if len(p.rows) == 0 || p.rows[0].name != tt.first {
			t.Fatalf("%q rows = %+v, want %q first", tt.query, p.rows, tt.first)
		}
		for _, r := range p.rows {
			if r.kind != rowBuffer {
				t.Fatalf("%q row = %+v, want a buffer row", tt.query, r)
			}
		}
	}
	// Every term must match: "save my" has no buffer with both terms.
	if p := New(Options{Query: "#save my", Buffers: buffers}); len(p.rows) != 0 {
		t.Fatalf("all-term query matched %+v, want no rows", p.rows)
	}
}

// probeFiles is a Files whose optional literal-path probe returns probe's answer and
// counts calls. It models the finder index during its initial enumeration.
type probeFiles struct {
	fakeFiles
	calls int
	probe func(ctx context.Context, query string) ([]string, error)
}

func (f *probeFiles) Probe(ctx context.Context, query string) ([]string, error) {
	f.calls++
	if f.probe != nil {
		return f.probe(ctx, query)
	}
	return nil, nil
}

// blockingProbeFiles blocks its probe until the palette cancels it.
type blockingProbeFiles struct {
	fakeFiles
	started  chan struct{}
	canceled chan struct{}
	once     sync.Once
}

func (f *blockingProbeFiles) Probe(ctx context.Context, _ string) ([]string, error) {
	f.once.Do(func() { close(f.started) })
	<-ctx.Done()
	close(f.canceled)
	return nil, ctx.Err()
}

// TestFileModeUsesTheLiteralPathProbe: the exact path is added before ranking while
// the index is loading, and the probe runs only in the tea.Cmd (never in New/Update).
func TestFileModeUsesTheLiteralPathProbe(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: true}}
	files.probe = func(context.Context, string) ([]string, error) { return []string{"a.go"}, nil }
	p := New(Options{Query: "a.go", Files: files, Ranker: &fakeRanker{}})
	if files.calls != 0 {
		t.Fatalf("probe ran synchronously %d times in New", files.calls)
	}
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init returned no ranking command")
	}
	if files.calls != 0 {
		t.Fatalf("probe ran synchronously when the command was built")
	}
	msg := cmd()
	if files.calls != 1 {
		t.Fatalf("probe calls = %d, want 1", files.calls)
	}
	p.update(msg)
	if paths, ok := p.cachedProbe("a.go"); !ok || len(paths) != 1 || paths[0] != "a.go" {
		t.Fatalf("probe cache = %v, %v; want a.go", paths, ok)
	}
	if len(p.rows) == 0 || p.rows[0].kind != rowFile || p.rows[0].name != "a.go" {
		t.Fatalf("rows = %+v, want the probed a.go", p.rows)
	}
}

// TestFileProbeResultIsDroppedForAChangedQuery: a probe/ranking message is displayed
// only for the current seq and text.
func TestFileProbeResultIsDroppedForAChangedQuery(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: true}}
	p := New(Options{Query: "a.go", Files: files, Ranker: &fakeRanker{}})
	// Force a newer query without running the old command.
	p.update(typed("x"))
	if cmd := p.update(FileRankedMsg{Seq: p.seq - 1, Text: "a.go", Matches: []Ranked{{Path: "a.go"}}, Loading: false, Probed: []string{"a.go"}, ProbeDone: true}); cmd != nil {
		t.Fatalf("stale probe result returned a command: %v", cmd)
	}
	for _, r := range p.rows {
		if r.kind == rowFile {
			t.Fatalf("stale probe result showed a file row: %+v", p.rows)
		}
	}
	if _, ok := p.cachedProbe("a.go"); ok {
		t.Fatal("stale probe result populated the probe cache")
	}
}

// TestFileProbeIsCanceledWhenTheQueryChanges: Update returns at once while the probe
// is blocked, and the next query cancels the in-flight probe context.
func TestFileProbeIsCanceledWhenTheQueryChanges(t *testing.T) {
	files := &blockingProbeFiles{
		fakeFiles: fakeFiles{loading: true},
		started:   make(chan struct{}),
		canceled:  make(chan struct{}),
	}
	p := New(Options{Query: "a", Files: files, Ranker: &fakeRanker{}})
	cmd := p.Init()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-files.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the probe did not start")
	}
	// Update must not wait for the probe; it replaces/cancels it.
	next := p.update(typed("b"))
	_ = next
	select {
	case <-files.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("changing the query did not cancel the blocked probe")
	}
	select {
	case msg := <-done:
		if msg != nil {
			t.Fatalf("a canceled probe command returned %#v, want nil", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled probe command did not return")
	}
}

// TestFileProbeIsCachedWhileLoading: poll ticks while the index is still loading reuse
// the probe answer instead of probing once per tick.
func TestFileProbeIsCachedWhileLoading(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: true}}
	files.probe = func(context.Context, string) ([]string, error) { return []string{"a.go"}, nil }
	p := New(Options{Query: "a.go", Files: files, Ranker: &fakeRanker{}})
	msg := p.Init()()
	p.update(msg)
	if files.calls != 1 {
		t.Fatalf("probe calls after first rank = %d, want 1", files.calls)
	}
	tick := p.update(fileTickMsg{Seq: p.seq})
	if tick == nil {
		t.Fatal("a loading tick returned no ranking command")
	}
	p.update(tick())
	if files.calls != 1 {
		t.Fatalf("probe calls after a loading tick = %d, want the cached 1", files.calls)
	}
}

// TestFileProbeRunsAfterLoading: Fresh probes the literal path even after the index
// has completed, so an unindexed/truncated exact file can still be opened.
func TestFileProbeRunsAfterLoading(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: false}}
	files.probe = func(context.Context, string) ([]string, error) { return []string{"a.go"}, nil }
	p := New(Options{Query: "a.go", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if files.calls != 1 {
		t.Fatalf("probe calls after the index completed = %d, want 1", files.calls)
	}
	if len(p.rows) == 0 || p.rows[0].kind != rowFile || p.rows[0].name != "a.go" {
		t.Fatalf("rows = %+v, want the completed-index probe result", p.rows)
	}
}

// TestFileProbeCacheAvoidsRepeatedStatForSamePath: a :line change keeps the same
// literal path, so the bounded cache serves the probe and no second Stat runs. It also
// covers a multi-word literal path and a mode-prefix escaped path.
func TestFileProbeCacheAvoidsRepeatedStatForSamePath(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: false}}
	files.probe = func(_ context.Context, query string) ([]string, error) {
		return []string{query}, nil
	}
	p := New(Options{Query: "a.go:12:3", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if files.calls != 1 {
		t.Fatalf("first probe calls = %d, want 1", files.calls)
	}
	if p.fileLine != 12 || p.fileCol != 3 {
		t.Fatalf("line target = %d:%d, want 12:3", p.fileLine, p.fileCol)
	}
	// Typing another column digit keeps the path a.go; the cached answer is reused.
	cmd := p.update(typed("4"))
	if cmd == nil {
		t.Fatal("typing the column produced no ranking command")
	}
	p.update(cmd())
	if files.calls != 1 {
		t.Fatalf("probe calls after a :line change = %d, want cached 1", files.calls)
	}
	if p.fileCol != 34 {
		t.Fatalf("line target column = %d, want 34", p.fileCol)
	}

	// A literal path containing spaces reaches the probe unchanged.
	p = New(Options{Query: "my file.txt", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if files.calls != 2 {
		t.Fatalf("multi-word probe calls = %d, want 2 total", files.calls)
	}
	if got := p.rows[0].name; got != "my file.txt" {
		t.Fatalf("multi-word row = %q, want my file.txt", got)
	}

	// A mode-prefix-leading path is probed unescaped under the file-mode escape.
	p = New(Options{Query: "./>notes.txt", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if files.calls != 3 {
		t.Fatalf("mode-prefix probe calls = %d, want 3 total", files.calls)
	}
	if got := p.input.Value(); got != "./>notes.txt" {
		t.Fatalf("mode-prefix query = %q", got)
	}
	if len(p.rows) == 0 || p.rows[0].kind != rowFile || p.rows[0].name != ">notes.txt" {
		t.Fatalf("mode-prefix rows = %+v, want >notes.txt", p.rows)
	}
}

// TestProbeCacheIsBounded: the palette keeps at most probeCacheSize answers.
func TestProbeCacheIsBounded(t *testing.T) {
	p := New(Options{})
	for i := 0; i < probeCacheSize+3; i++ {
		p.rememberProbe(fmt.Sprintf("p%d", i), []string{fmt.Sprintf("p%d", i)})
	}
	if len(p.probeCache) != probeCacheSize {
		t.Fatalf("probe cache size = %d, want %d", len(p.probeCache), probeCacheSize)
	}
	if _, ok := p.cachedProbe("p0"); ok {
		t.Fatal("oldest probe cache entry was not evicted")
	}
	if _, ok := p.cachedProbe(fmt.Sprintf("p%d", probeCacheSize+2)); !ok {
		t.Fatal("newest probe cache entry is missing")
	}
}

// TestPaletteDismissCancelsTheProbe: the root's Esc/cancel path calls Dismiss, which
// must cancel an in-flight literal-path probe.
func TestPaletteDismissCancelsTheProbe(t *testing.T) {
	files := &blockingProbeFiles{
		fakeFiles: fakeFiles{loading: true},
		started:   make(chan struct{}),
		canceled:  make(chan struct{}),
	}
	p := New(Options{Query: "a", Files: files, Ranker: &fakeRanker{}})
	cmd := p.Init()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-files.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the probe did not start")
	}
	p.Dismiss()
	select {
	case <-files.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("Dismiss did not cancel the probe")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled probe command did not return")
	}
}

// TestFileResultWithTheWrongTextIsDropped: even with the current seq, a result whose
// text is not the palette's current file text is not displayed or cached.
func TestFileResultWithTheWrongTextIsDropped(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: true}}
	p := New(Options{Query: "a.go", Files: files, Ranker: &fakeRanker{}})
	if cmd := p.update(FileRankedMsg{Seq: p.seq, Text: "other.go", Matches: []Ranked{{Path: "a.go"}}, Loading: false, Probed: []string{"a.go"}, ProbeDone: true}); cmd != nil {
		t.Fatalf("wrong-text result returned a command: %v", cmd)
	}
	for _, r := range p.rows {
		if r.kind == rowFile {
			t.Fatalf("wrong-text result showed a file row: %+v", p.rows)
		}
	}
	if _, ok := p.cachedProbe("a.go"); ok {
		t.Fatal("wrong-text result populated the probe cache")
	}
}

// TestFileProbeMergesPrefixResults: the parent-prefix probe can return several files;
// all are merged, deduplicated and ranked through the normal path.
func TestFileProbeMergesPrefixResults(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: true}}
	files.probe = func(context.Context, string) ([]string, error) {
		return []string{"save_file.rs", "save_other.rs"}, nil
	}
	p := New(Options{Query: "save", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if paths, ok := p.cachedProbe("save"); !ok || len(paths) != 2 {
		t.Fatalf("probe cache = %v, %v; want two prefix paths", paths, ok)
	}
	got := make([]string, 0, len(p.rows))
	for _, r := range p.rows {
		if r.kind == rowFile {
			got = append(got, r.name)
		}
	}
	if len(got) != 2 || got[0] != "save_file.rs" || got[1] != "save_other.rs" {
		t.Fatalf("rows = %v, want the two probed prefix paths", got)
	}
}

// TestFileProbeCachesANegativeAnswer: a completed index and a missing exact path are
// cached too, so a poll or a :line change for the same path does not Stat again.
func TestFileProbeCachesANegativeAnswer(t *testing.T) {
	files := &probeFiles{fakeFiles: fakeFiles{loading: false}}
	files.probe = func(context.Context, string) ([]string, error) { return nil, nil }
	p := New(Options{Query: "missing.go:4", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if files.calls != 1 {
		t.Fatalf("first negative probe calls = %d, want 1", files.calls)
	}
	cmd := p.update(fileTickMsg{Seq: p.seq})
	if cmd == nil {
		t.Fatal("a rank tick returned no command")
	}
	p.update(cmd())
	if files.calls != 1 {
		t.Fatalf("negative probe calls after a rank tick = %d, want cached 1", files.calls)
	}
	if paths, ok := p.cachedProbe("missing.go"); !ok || len(paths) != 0 {
		t.Fatalf("negative cache = %v, %v; want a cached empty answer", paths, ok)
	}
}

// generatedProbeFiles is probeFiles with a file-index generation, like finder.Index.
type generatedProbeFiles struct {
	*probeFiles
	gen uint64
}

func (f *generatedProbeFiles) Generation() uint64 { return f.gen }

// TestFileProbeCacheInvalidatesOnIndexGeneration: a refresh (Restart) bumps the index
// generation; the stale negative exact-path answer is dropped, so a file created by
// the refreshed index is found without reopening the palette. The same generation
// still reuses the cache, and the probe still runs only in the command.
func TestFileProbeCacheInvalidatesOnIndexGeneration(t *testing.T) {
	files := &generatedProbeFiles{
		probeFiles: &probeFiles{fakeFiles: fakeFiles{loading: false}},
		gen:        1,
	}
	files.probe = func(context.Context, string) ([]string, error) {
		if files.gen >= 2 {
			return []string{"new.go"}, nil
		}
		return nil, nil
	}
	p := New(Options{Query: "new.go", Files: files, Ranker: &fakeRanker{}})
	p.update(p.Init()())
	if files.calls != 1 {
		t.Fatalf("first probe calls = %d, want 1", files.calls)
	}
	if len(p.rows) != 0 {
		t.Fatalf("rows before refresh = %+v, want none", p.rows)
	}
	// A re-rank in the same generation still reuses the negative answer.
	tick := p.update(fileTickMsg{Seq: p.seq})
	if tick == nil {
		t.Fatal("same-generation rank tick returned no command")
	}
	p.update(tick())
	if files.calls != 1 {
		t.Fatalf("same-generation probe calls = %d, want cached 1", files.calls)
	}
	// Simulate finder.Index.Restart: generation changes and the exact file now exists.
	files.gen = 2
	cmd := p.update(fileTickMsg{Seq: p.seq})
	if cmd == nil {
		t.Fatal("post-refresh rank tick returned no command")
	}
	if files.calls != 1 {
		t.Fatalf("probe ran synchronously in Update: calls = %d", files.calls)
	}
	p.update(cmd())
	if files.calls != 2 {
		t.Fatalf("post-refresh probe calls = %d, want a fresh 2", files.calls)
	}
	if len(p.rows) == 0 || p.rows[0].kind != rowFile || p.rows[0].name != "new.go" {
		t.Fatalf("post-refresh rows = %+v, want new.go", p.rows)
	}
}

// hiddenSetterFiles records the explorer Show Hidden Files setting the palette forwards
// to the local literal-path probe.
type hiddenSetterFiles struct {
	*probeFiles
	hidden bool
	sets   int
}

func (f *hiddenSetterFiles) SetProbeShowHidden(show bool) {
	f.hidden = show
	f.sets++
}

// TestPalettePassesTheShowHiddenSetting: the palette applies the explorer setting to the
// probe port at construction (no I/O in the probe itself).
func TestPalettePassesTheShowHiddenSetting(t *testing.T) {
	files := &hiddenSetterFiles{probeFiles: &probeFiles{fakeFiles: fakeFiles{loading: false}}}
	New(Options{Query: "a", Files: files, Ranker: &fakeRanker{}, ShowHidden: true})
	if files.sets != 1 || !files.hidden {
		t.Fatalf("show-hidden setting = %v after %d sets, want true after one", files.hidden, files.sets)
	}
	files2 := &hiddenSetterFiles{probeFiles: &probeFiles{fakeFiles: fakeFiles{loading: false}}}
	New(Options{Query: "a", Files: files2, Ranker: &fakeRanker{}})
	if files2.sets != 1 || files2.hidden {
		t.Fatalf("default show-hidden = %v after %d sets, want false", files2.hidden, files2.sets)
	}
}

// TestPageKeysMoveOneVisiblePage covers Fresh's prompt_page_up/prompt_page_down: the
// step is one laid-out page of rows, clamped to the first/last match, and PageUp at the
// top or PageDown at the end stays put. No command/query work is started.
func TestPageKeysMoveOneVisiblePage(t *testing.T) {
	p := New(Options{Query: ">Cmd", Commands: fakeCommands(100)})
	draw(p, 100, 30)
	rows := layoutForMode(0, p.height, p.layoutMode()).rows
	if rows <= 0 || rows >= len(p.rows) {
		t.Fatalf("page rows = %d of %d, want a full page inside the list", rows, len(p.rows))
	}
	if got := p.nav.Selected(); got != 0 {
		t.Fatalf("initial selection = %d, want 0", got)
	}
	if cmd := p.update(key(tea.KeyPgDown)); cmd != nil {
		t.Fatalf("PageDown started a command: %v", cmd)
	}
	if got := p.nav.Selected(); got != rows {
		t.Fatalf("selection after PageDown = %d, want one page %d", got, rows)
	}
	p.update(key(tea.KeyPgUp))
	if got := p.nav.Selected(); got != 0 {
		t.Fatalf("selection after PageUp = %d, want 0", got)
	}
	p.update(key(tea.KeyPgUp))
	if got := p.nav.Selected(); got != 0 {
		t.Fatalf("PageUp at the top moved to %d, want 0", got)
	}
	for range 10 {
		p.update(key(tea.KeyPgDown))
	}
	if got, want := p.nav.Selected(), len(p.rows)-1; got != want {
		t.Fatalf("selection after paging past the end = %d, want last %d", got, want)
	}
}

// TestPageKeysBeforeLayoutDoNothing: Fresh's pager has no page until the list was laid
// out, so the keys are no-ops on a palette that has not been drawn.
func TestPageKeysBeforeLayoutDoNothing(t *testing.T) {
	p := New(Options{Query: ">Cmd", Commands: fakeCommands(100)})
	p.update(key(tea.KeyPgDown))
	if got := p.nav.Selected(); got != 0 {
		t.Fatalf("selection before layout = %d, want 0", got)
	}
}

// ctrlF is Fresh's prompt_confirm chord in a palette query.
func ctrlF() tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}) }

// TestCtrlFConfirmsLikeEnter checks Fresh's prompt_confirm binding in the file, command
// and Live Grep modes: Ctrl+F runs/opens the selected row instead of moving the caret.
func TestCtrlFConfirmsLikeEnter(t *testing.T) {
	t.Parallel()
	t.Run("file", func(t *testing.T) {
		r := &fakeRanker{}
		p := New(Options{Root: "/work", Files: &fakeFiles{files: []string{"src/a.go", "b.txt"}}, Ranker: r})
		draw(p, 100, 30)
		settle(t, p, p.Init(), 4)
		for _, c := range "b" {
			settle(t, p, p.update(typed(string(c))), 4)
		}
		if got := runCmd(p.update(ctrlF())); got != (OpenFileMsg{Path: "/work/b.txt", Rel: "b.txt"}) {
			t.Fatalf("Ctrl+F emitted %#v, want the selected file", got)
		}
		if p.Active() {
			t.Fatal("palette stayed open after Ctrl+F")
		}
	})
	t.Run("command", func(t *testing.T) {
		st := &commands.State{TextBuffer: true, Editable: true, HasPath: true, Modified: true}
		p := New(commandOptions(">Save File", st))
		draw(p, 100, 30)
		if p.rows[0].action != "save" {
			t.Fatalf("first row action = %q, want save", p.rows[0].action)
		}
		if got := runCmd(p.update(ctrlF())); got != (RunCommandMsg{Action: "save"}) {
			t.Fatalf("Ctrl+F emitted %#v, want save", got)
		}
	})
	t.Run("live grep", func(t *testing.T) {
		p := New(liveOptions("todo", &fakeFiles{}, &fakeGrepper{}))
		draw(p, 100, 30)
		p.update(GrepResultMsg{Seq: p.seq, Text: "todo", Hits: []GrepHit{{Rel: "a.go", Line: 2, Col: 1, Text: "todo"}}})
		if got := runCmd(p.update(ctrlF())); got != (OpenLineMsg{Path: "/w/a.go", Line: 2, Col: 1}) {
			t.Fatalf("Ctrl+F emitted %#v, want the selected Live Grep row", got)
		}
	})
}
