package explorer

import (
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// testGeometry is the explorer geometry the selection tests share: a body of
// three rows in opsTree, with a scrollbar column that never shows.
var testGeometry = Geometry{Width: 16, Bottom: 9, Focused: true}

// doAction runs one explorer action on p. It returns the panel the action leaves
// and the message its command sends, and fails the test when the action is not
// handled.
func doAction(t *testing.T, p Panel, action string) (Panel, tea.Msg) {
	t.Helper()
	next, cmd, ok := p.Action(action, testGeometry)
	if !ok {
		t.Fatalf("%s not handled", action)
	}
	return next, run(cmd)
}

// baseNames returns the last element of each path, so tests can name rows.
func baseNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

// pendingThing names the first transient state that is still set, in the order
// Esc clears them. It is "" when nothing is pending.
func pendingThing(p Panel) string {
	s := p.state()
	switch {
	case len(s.cut) > 0:
		return "cut"
	case len(s.picked) > 0:
		return "picked"
	case s.query != "":
		return "query"
	}
	return ""
}

func TestClearTransientClearsOneThingPerEsc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, p Panel) Panel
		want  []string // what each Esc clears in turn; "" is the Esc that finds nothing
	}{
		{
			name:  "nothing pending",
			setup: func(t *testing.T, p Panel) Panel { return p },
			want:  []string{""},
		},
		{
			name: "search text alone",
			setup: func(t *testing.T, p Panel) Panel {
				p.TypeSearch('x', testGeometry)
				return p
			},
			want: []string{"query", ""},
		},
		{
			name: "picked rows before search text",
			setup: func(t *testing.T, p Panel) Panel {
				p, _ = doAction(t, p, "file_explorer_toggle_select")
				p.TypeSearch('x', testGeometry)
				return p
			},
			want: []string{"picked", "query", ""},
		},
		{
			name: "cut before picked rows before search text",
			setup: func(t *testing.T, p Panel) Panel {
				p, _ = doAction(t, p, "file_explorer_toggle_select")
				p, _ = doAction(t, p, "cut")
				p.TypeSearch('x', testGeometry)
				return p
			},
			want: []string{"cut", "picked", "query", ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.setup(t, New(opsTree(1), style.Styles{}, nil))
			for i, want := range tt.want {
				if got := pendingThing(p); got != want {
					t.Fatalf("Esc %d: pending %q, want %q", i+1, got, want)
				}
				if cleared := p.ClearTransient(); cleared != (want != "") {
					t.Fatalf("Esc %d: ClearTransient = %v, want %v", i+1, cleared, want != "")
				}
			}
			if got := pendingThing(p); got != "" {
				t.Errorf("still pending %q after every Esc", got)
			}
		})
	}
}

func TestTargetsAreTheVisiblePicksInTreeOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sel  int
		keys []string // explorer actions run before the check, in order
		want []string // base names of the targets
	}{
		{
			name: "no pick means the cursor row",
			sel:  2,
			want: []string{"b.txt"},
		},
		{
			name: "picks come out in display order",
			sel:  2,
			keys: []string{"file_explorer_toggle_select", "file_explorer_up", "file_explorer_up", "file_explorer_toggle_select"},
			want: []string{"a", "b.txt"},
		},
		{
			name: "a second toggle drops the row from the picks",
			sel:  1,
			keys: []string{"file_explorer_toggle_select", "file_explorer_down", "file_explorer_toggle_select", "file_explorer_up", "file_explorer_toggle_select"},
			want: []string{"b.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(opsTree(tt.sel), style.Styles{}, nil)
			for _, k := range tt.keys {
				p, _ = doAction(t, p, k)
			}
			if got := baseNames(p.targets()); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("targets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtendSelectionSelectsTheRangeFromTheAnchor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sel  int
		keys []string
		want []string
	}{
		{
			name: "one row down",
			sel:  0,
			keys: []string{"file_explorer_extend_selection_down"},
			want: []string{"a", "x.txt"},
		},
		{
			name: "to the last row",
			sel:  0,
			keys: []string{"file_explorer_extend_selection_down", "file_explorer_extend_selection_down"},
			want: []string{"a", "x.txt", "b.txt"},
		},
		{
			name: "back over the anchor",
			sel:  0,
			keys: []string{"file_explorer_extend_selection_down", "file_explorer_extend_selection_up"},
			want: []string{"a"},
		},
		{
			name: "upward from the bottom",
			sel:  2,
			keys: []string{"file_explorer_extend_selection_up", "file_explorer_extend_selection_up"},
			want: []string{"a", "x.txt", "b.txt"},
		},
		{
			name: "a plain move ends the range",
			sel:  0,
			keys: []string{"file_explorer_extend_selection_down", "file_explorer_down", "file_explorer_extend_selection_down"},
			want: []string{"b.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(opsTree(tt.sel), style.Styles{}, nil)
			for _, k := range tt.keys {
				p, _ = doAction(t, p, k)
			}
			if got := baseNames(p.targets()); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("targets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectAllPicksEveryVisibleRow(t *testing.T) {
	t.Parallel()
	p := New(opsTree(1), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_select_all")
	if got, want := baseNames(p.targets()), []string{"a", "x.txt", "b.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("select all targets = %v, want %v", got, want)
	}
}

// TestCopyAndCutSendOneMessagePerTargetInOrder checks the messages behind a
// multi-item copy or cut: one message carrying every target in display order, so
// the root holds the whole selection, and a cut leaves a pending cut.
func TestCopyAndCutSendOneMessageForEveryTarget(t *testing.T) {
	t.Parallel()
	p := New(opsTree(0), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_extend_selection_down")
	paths := []string{"/r/a", "/r/a/x.txt"}
	if _, got := doAction(t, p, "copy"); !reflect.DeepEqual(got, CopyMsg{Paths: paths}) {
		t.Errorf("copy of two rows sent %#v, want one CopyMsg with both paths", got)
	}
	cut, got := doAction(t, p, "cut")
	if !reflect.DeepEqual(got, CutMsg{Paths: paths}) {
		t.Errorf("cut of two rows sent %#v, want one CutMsg with both paths", got)
	}
	if !cut.CutPending() {
		t.Error("cut of two rows left no pending cut")
	}
}

// TestCutDimsRowsUntilPasteOrCopy checks that a cut dims its rows, that a paste
// leaves the cut to the root, and that a copy ends the cut.
func TestCutDimsRowsUntilPasteOrCopy(t *testing.T) {
	t.Parallel()
	p := New(opsTree(1), style.Styles{}, nil)
	p, _ = doAction(t, p, "cut")
	if !p.CutPending() {
		t.Fatal("cut not pending")
	}
	dst := grid.New(16, 10)
	p.Draw(dst, testGeometry)
	// x.txt is body row 1, drawn at y 3; its name starts at column 6.
	if !dst.At(6, 3).Style.Faint {
		t.Error("cut row x.txt is not dimmed")
	}
	if dst.At(3, 2).Style.Faint {
		t.Error("row a, which is not cut, is dimmed")
	}

	p, _ = doAction(t, p, "paste")
	if !p.CutPending() {
		t.Error("paste ended the cut before the root ran the paste")
	}
	p, _ = doAction(t, p, "cut")
	p, _ = doAction(t, p, "copy")
	if p.CutPending() {
		t.Error("copy left the cut pending")
	}
}

// TestPickedRowsUseTheSelectionBackground checks that a picked row is painted
// with the selection background and the rows around it are not.
func TestPickedRowsUseTheSelectionBackground(t *testing.T) {
	t.Parallel()
	selection := lipgloss.NewStyle().Background(lipgloss.Color("#224466"))
	p := New(opsTree(2), style.Styles{Selection: selection}, nil)
	p, _ = doAction(t, p, "file_explorer_toggle_select")
	dst := grid.New(16, 10)
	p.Draw(dst, testGeometry)
	// b.txt is body row 2, drawn at y 4, its name at column 4.
	want := style.Cell(selection).BG
	if got := dst.At(3, 4).Style.BG; got == nil || got != want {
		t.Errorf("picked row background = %v, want %v", got, want)
	}
	if got := dst.At(3, 2).Style.BG; got == want {
		t.Errorf("unpicked row a has the selection background")
	}
}

// TestArrowKeysPreviewFilesOnly checks that Up and Down send the preview a
// single click sends when they land on a file, and nothing on a directory. A
// range extend does not preview.
func TestArrowKeysPreviewFilesOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sel    int
		action string
		want   tea.Msg
	}{
		{"down onto a file previews it", 0, "file_explorer_down", OpenFileMsg{Path: "/r/a/x.txt", Preview: true}},
		{"down from a file previews the next one", 1, "file_explorer_down", OpenFileMsg{Path: "/r/b.txt", Preview: true}},
		{"up onto a directory does not preview", 1, "file_explorer_up", nil},
		{"up onto a file previews it", 2, "file_explorer_up", OpenFileMsg{Path: "/r/a/x.txt", Preview: true}},
		{"extending the range does not preview", 0, "file_explorer_extend_selection_down", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(opsTree(tt.sel), style.Styles{}, nil)
			_, got := doAction(t, p, tt.action)
			if got != tt.want {
				t.Errorf("%s sent %#v, want %#v", tt.action, got, tt.want)
			}
		})
	}
}

func TestToggleGitignoredFlipsTheTreeAndRescans(t *testing.T) {
	t.Parallel()
	tr := opsTree(0)
	p := New(tr, style.Styles{}, nil)
	p, msg := doAction(t, p, "file_explorer_toggle_gitignored")
	if !tr.ignored || msg != (ReloadMsg{}) {
		t.Fatalf("first toggle: ignored=%v message=%#v, want true and ReloadMsg", tr.ignored, msg)
	}
	_, msg = doAction(t, p, "file_explorer_toggle_gitignored")
	if tr.ignored || msg != (ReloadMsg{}) {
		t.Fatalf("second toggle: ignored=%v message=%#v, want false and ReloadMsg", tr.ignored, msg)
	}
}

func TestToggleGitignoredFlipsTheTreeAndReloads(t *testing.T) {
	t.Parallel()
	tr := opsTree(0)
	p := New(tr, style.Styles{}, nil)
	_, cmd, ok := p.Action("file_explorer_toggle_gitignored", testGeometry)
	if !ok {
		t.Fatal("toggle not handled")
	}
	if !tr.ShowIgnored() {
		t.Error("tree still hides gitignored entries after the toggle")
	}
	if cmd == nil {
		t.Fatal("toggle sent no command")
	}
	if _, ok := cmd().(ReloadMsg); !ok {
		t.Errorf("command = %T, want ReloadMsg", cmd())
	}
}

func TestTypeSearchMovesToTheFirstMatch(t *testing.T) {
	t.Parallel()
	type step struct {
		op    string // "type", "backspace" or "clear"
		r     rune   // the typed rune, for "type"
		want  int    // the selected row after the step
		query string // the search text after the step
	}
	tests := []struct {
		name  string
		sel   int
		steps []step
	}{
		{
			name:  "first match in display order",
			sel:   0,
			steps: []step{{op: "type", r: 'x', want: 1, query: "x"}},
		},
		{
			name:  "letters in order with gaps match",
			sel:   1,
			steps: []step{{op: "type", r: 'b', want: 2, query: "b"}, {op: "type", r: 't', want: 2, query: "bt"}},
		},
		{
			name:  "no match keeps the selection",
			sel:   0,
			steps: []step{{op: "type", r: 'q', want: 0, query: "q"}},
		},
		{
			name: "backspace finds the first match of the shorter text",
			sel:  2,
			steps: []step{
				{op: "type", r: 'x', want: 1, query: "x"},
				{op: "type", r: 'q', want: 1, query: "xq"},
				{op: "backspace", want: 1, query: "x"},
			},
		},
		{
			name:  "backspace on an empty search does nothing",
			sel:   2,
			steps: []step{{op: "backspace", want: 2, query: ""}},
		},
		{
			name:  "clearing the search keeps the selection",
			sel:   2,
			steps: []step{{op: "type", r: 'x', want: 1, query: "x"}, {op: "clear", want: 1, query: ""}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := opsTree(tt.sel)
			p := New(tr, style.Styles{}, nil)
			for i, s := range tt.steps {
				switch s.op {
				case "type":
					p.TypeSearch(s.r, testGeometry)
				case "backspace":
					p.SearchBackspace(testGeometry)
				case "clear":
					p.ClearSearch()
				}
				if tr.sel != s.want || p.state().query != s.query {
					t.Fatalf("step %d (%s): selected row %d, query %q; want row %d, query %q",
						i+1, s.op, tr.sel, p.state().query, s.want, s.query)
				}
			}
		})
	}
}

// TestDropCutTakesThePathAndWhatIsUnderIt checks that DropCut removes a dimmed path
// and the dimmed paths under it, and leaves the other dimmed rows dimmed.
func TestDropCutTakesThePathAndWhatIsUnderIt(t *testing.T) {
	t.Parallel()
	p := New(opsTree(0), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_extend_selection_down")
	p, _ = doAction(t, p, "cut")
	p.DropCut("/r/a")
	if p.CutPending() {
		t.Error("DropCut(/r/a) left /r/a or /r/a/x.txt dimmed")
	}

	p = New(opsTree(2), style.Styles{}, nil)
	p, _ = doAction(t, p, "cut")
	p.DropCut("/r/a")
	if !p.CutPending() {
		t.Error("DropCut(/r/a) removed b.txt, which is not under it")
	}
}

// TestCarryCutKeepsThePendingCutInARebuiltPanel checks that a panel built in place of
// another keeps the other's pending cut, so the dimming survives the rebuild that
// hiding the explorer makes.
func TestCarryCutKeepsThePendingCutInARebuiltPanel(t *testing.T) {
	t.Parallel()
	tr := opsTree(1)
	p := New(tr, style.Styles{}, nil)
	p, _ = doAction(t, p, "cut")
	rebuilt := New(tr, style.Styles{}, nil).CarryCut(p)
	if !rebuilt.CutPending() {
		t.Fatal("rebuilt panel lost the pending cut")
	}
	dst := grid.New(16, 10)
	rebuilt.Draw(dst, testGeometry)
	// x.txt is body row 1, drawn at y 3; its name starts at column 6.
	if !dst.At(6, 3).Style.Faint {
		t.Error("cut row x.txt is not dimmed in the rebuilt panel")
	}
}

// TestCopyWithNoRowKeepsThePendingCut checks that a copy that finds no row to copy
// does not end a pending cut. The root still holds the cut, so its rows stay dimmed.
func TestCopyWithNoRowKeepsThePendingCut(t *testing.T) {
	t.Parallel()
	tr := opsTree(1)
	p := New(tr, style.Styles{}, nil)
	p, _ = doAction(t, p, "cut")
	tr.sel = -1
	p, _ = doAction(t, p, "copy")
	if !p.CutPending() {
		t.Error("copy with no row ended the pending cut")
	}
}

// TestDropPickedTakesThePathWhatIsUnderItAndTheAnchor checks that DropPicked removes a
// picked path, the picked paths under it and a range anchor on it, and leaves the
// other picks alone.
func TestDropPickedTakesThePathWhatIsUnderItAndTheAnchor(t *testing.T) {
	t.Parallel()
	p := New(opsTree(0), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_extend_selection_down")
	p.DropPicked("/r/a")
	if got := pendingThing(p); got != "" {
		t.Errorf("DropPicked(/r/a) left %q pending, want no picks", got)
	}
	if p.state().anchor != "" {
		t.Errorf("DropPicked(/r/a) left anchor %q, want none", p.state().anchor)
	}

	p = New(opsTree(2), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_toggle_select")
	p.DropPicked("/r/a")
	if !reflect.DeepEqual(baseNames(p.targets()), []string{"b.txt"}) || pendingThing(p) != "picked" {
		t.Errorf("DropPicked(/r/a) removed b.txt, which is not under it")
	}
}
