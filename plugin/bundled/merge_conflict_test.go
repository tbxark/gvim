package bundled_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// mcEnv is the merge_conflict plugin on a fake host with one buffer, /w/a.txt.
type mcEnv struct {
	h   *plugintest.Harness
	buf pluginapi.BufferID
	b   *plugintest.FakeBuffer
}

func newMC(t *testing.T, text string) *mcEnv {
	t.Helper()
	src, err := fs.ReadFile(bundled.FS, "plugins/merge_conflict/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("/w/a.txt", text)
	h := plugintest.New(t, plugintest.Files{"merge_conflict/init.lua": string(src)}, plugintest.Options{Host: host, Timeout: testSliceBudget, EventBudget: testSliceBudget})
	h.Start()
	return &mcEnv{h: h, buf: id, b: host.Buffers[0]}
}

// open raises after_file_open for the buffer, as the ui does when a file is loaded.
func (e *mcEnv) open() {
	e.h.T.Helper()
	e.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": e.buf, "path": "/w/a.txt"}})
}

func (e *mcEnv) text() string { return strings.Join(e.b.Lines, "\n") }

func (e *mcEnv) setCaret(line int) {
	e.h.T.Helper()
	if err := e.h.Host.SetCursor(e.buf, pluginapi.Pos{Line: line}, false, false, false); err != nil {
		e.h.T.Fatal(err)
	}
}

func (e *mcEnv) caretLine() int {
	cs, _ := e.h.Host.Cursors(e.buf)
	return cs[0].Pos.Line
}

// decor summarises the decorations of the plugin on the buffer in the order it set them:
// "line N", "hl A-B" (whole lines A up to B), "scroll N".
func (e *mcEnv) decor() []string {
	var out []string
	for _, d := range e.h.Host.Decor().Items("merge_conflict", "conflicts", e.buf) {
		switch d.Kind {
		case pluginapi.DecorLine:
			out = append(out, fmt.Sprintf("line %d", d.Line))
		case pluginapi.DecorHighlight:
			out = append(out, fmt.Sprintf("hl %d-%d", d.Range.From.Line, d.Range.To.Line))
		case pluginapi.DecorScroll:
			out = append(out, fmt.Sprintf("scroll %d", d.Line))
		default:
			out = append(out, fmt.Sprintf("other %v", d.Kind))
		}
	}
	return out
}

// markerLines are the lines carrying a "line" decoration, sorted.
func (e *mcEnv) markerLines() []int {
	var out []int
	for _, d := range e.h.Host.Decor().Items("merge_conflict", "conflicts", e.buf) {
		if d.Kind == pluginapi.DecorLine {
			out = append(out, d.Line)
		}
	}
	slices.Sort(out)
	return out
}

func (e *mcEnv) lastNotice() string {
	n := e.h.Notices()
	if len(n) == 0 {
		return ""
	}
	return n[len(n)-1].Msg
}

func (e *mcEnv) findLinesCalls() []pluginapi.FindLinesQuery {
	var out []pluginapi.FindLinesQuery
	for _, c := range e.h.Host.Calls() {
		if c.Method == "FindLines" {
			out = append(out, c.Args[1].(pluginapi.FindLinesQuery))
		}
	}
	return out
}

// linesRead counts the lines the plugin asked the host for.
func (e *mcEnv) linesRead() int {
	n := 0
	for _, c := range e.h.Host.Calls() {
		if c.Method != "Lines" {
			continue
		}
		first, count := c.Args[1].(int), c.Args[2].(int)
		if count < 0 {
			count = max(len(e.b.Lines)-first, 0)
		}
		n += count
	}
	return n
}

func (e *mcEnv) warnings() []string {
	var out []string
	for _, l := range e.h.LogText("merge_conflict") {
		if strings.HasPrefix(l, "warn") || strings.HasPrefix(l, "error") {
			out = append(out, l)
		}
	}
	return out
}

const mcTwo = "head\n" + // 0
	"<<<<<<< HEAD\n" + // 1
	"ours1\n" + // 2
	"ours2\n" + // 3
	"=======\n" + // 4
	"theirs1\n" + // 5
	">>>>>>> branch\n" + // 6
	"middle\n" + // 7
	"<<<<<<< HEAD\n" + // 8
	"a\n" + // 9
	"||||||| base\n" + // 10
	"b\n" + // 11
	"=======\n" + // 12
	"c\n" + // 13
	">>>>>>> other\n" + // 14
	"tail" // 15

func TestMergeConflictRegistersItsCommands(t *testing.T) {
	t.Parallel()
	e := newMC(t, "x")
	var got []string
	for _, c := range e.h.Host.Commands["merge_conflict"] {
		if !c.Hidden {
			got = append(got, c.ID+"="+c.Title)
		}
	}
	want := []string{
		"merge_conflict.next=Merge: Next Conflict",
		"merge_conflict.prev=Merge: Previous Conflict",
		"merge_conflict.resolve=Merge: Resolve Conflict...",
		"merge_conflict.start=Merge: Start Resolution",
		"merge_conflict.take_theirs=Merge: Take Theirs",
		"merge_conflict.use_both=Merge: Use Both",
		"merge_conflict.use_ours=Merge: Use Ours",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	data, err := fs.ReadFile(bundled.FS, "plugins/merge_conflict/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	// merge_conflict is off by default after the idle-heap gate decision (round 9); it is
	// still available when enabled.
	var manifest struct {
		DefaultEnabled *bool `json:"default_enabled"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DefaultEnabled == nil || *manifest.DefaultEnabled {
		t.Fatalf("plugin.json = %q: merge_conflict must ship default_enabled false", data)
	}
}

// TestMergeConflictDetection: which texts are conflicts, and what is decorated.

func TestMergeConflictKeymapBindings(t *testing.T) {
	t.Parallel()
	e := newMC(t, "x")
	bindings := e.h.Host.Bindings["merge_conflict"]
	chords := map[string]string{}
	for _, b := range bindings {
		chords[b.Chord] = b.Action
	}
	if chords["alt+m"] != "merge_conflict.start" {
		t.Errorf("alt+m = %q, want merge_conflict.start", chords["alt+m"])
	}
	if chords["ctrl+alt+c"] != "merge_conflict.next" {
		t.Errorf("ctrl+alt+c = %q, want merge_conflict.next", chords["ctrl+alt+c"])
	}
}

func TestMergeConflictDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "a plain and a diff3 conflict",
			text: mcTwo,
			want: []string{
				"line 1", "line 4", "line 6", "hl 2-4", "hl 5-6", "scroll 1",
				"line 8", "line 12", "line 14", "line 10", "hl 9-10", "hl 13-14", "scroll 8",
			},
		},
		{name: "no markers", text: "a\nb\nc", want: nil},
		{
			name: "look-alikes are not markers",
			text: "========\n<<<<<<<<\n <<<<<<< x\n=======  \n>>>>>>>>\n<<<<<<<x\n||||||||\n",
			want: nil,
		},
		{name: "a block that never closes", text: "<<<<<<< a\nx\n=======\ny", want: nil},
		{name: "a block without a separator", text: "<<<<<<< a\nx\n>>>>>>> b", want: nil},
		{
			name: "a stray separator before the start",
			text: "=======\n<<<<<<< a\nx\n=======\ny\n>>>>>>> b",
			want: []string{"line 1", "line 3", "line 5", "hl 2-3", "hl 4-5", "scroll 1"},
		},
		{
			name: "a second start restarts the block",
			text: "<<<<<<< a\nx\n<<<<<<< b\ny\n=======\nz\n>>>>>>> c",
			want: []string{"line 2", "line 4", "line 6", "hl 3-4", "hl 5-6", "scroll 2"},
		},
		{
			name: "empty sides have no highlight and a label is optional",
			text: "<<<<<<<\n=======\n>>>>>>>",
			want: []string{"line 0", "line 1", "line 2", "scroll 0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newMC(t, tt.text)
			e.open()
			if got := e.decor(); !slices.Equal(got, tt.want) {
				t.Errorf("decorations = %v, want %v", got, tt.want)
			}
			if tt.want == nil && e.h.Host.CallCount("DecorSet") != 0 {
				t.Error("a file without conflicts was decorated")
			}
			if w := e.warnings(); len(w) != 0 {
				t.Errorf("log: %v", w)
			}
		})
	}
}

func TestMergeConflictDecorationStyles(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	for _, d := range e.h.Host.Decor().Items("merge_conflict", "conflicts", e.buf) {
		switch {
		case d.Kind == pluginapi.DecorHighlight && d.Range.From.Line == 2:
			if d.Style.BG.Key != "editor.diff_add_bg" || !d.Style.Extend {
				t.Errorf("ours highlight style = %+v", d.Style)
			}
		case d.Kind == pluginapi.DecorHighlight && d.Range.From.Line == 5:
			if d.Style.BG.Key != "editor.diff_remove_bg" || !d.Style.Extend {
				t.Errorf("theirs highlight style = %+v", d.Style)
			}
		case d.Kind == pluginapi.DecorScroll:
			if d.Color.Key != "warning" {
				t.Errorf("scroll color = %+v", d.Color)
			}
		}
	}
}

// Replay and the three load events can all arrive for one buffer: the scan happens once.
func TestMergeConflictIsIdempotent(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	e.open()
	e.h.Emit(pluginapi.Event{Name: "buffer_activated", Data: map[string]any{"buf": e.buf}})
	if n := len(e.findLinesCalls()); n != 1 {
		t.Errorf("FindLines calls = %d, want 1", n)
	}
	if n := e.h.Host.CallCount("DecorSet"); n != 1 {
		t.Errorf("DecorSet calls = %d, want 1", n)
	}
}

func TestMergeConflictRevertRedecorates(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	// The host drops decorations when a buffer is replaced as a whole.
	e.h.Host.DecorClear("merge_conflict", "conflicts", e.buf)
	e.b.Lines = strings.Split("<<<<<<< a\nx\n=======\ny\n>>>>>>> b", "\n")
	e.b.Info.Version.Gen++
	e.b.Info.LineCount = len(e.b.Lines)
	e.h.Emit(pluginapi.Event{Name: "after_file_revert", Data: map[string]any{"buf": e.buf, "path": "/w/a.txt"}})
	want := []string{"line 0", "line 2", "line 4", "hl 1-2", "hl 3-4", "scroll 0"}
	if got := e.decor(); !slices.Equal(got, want) {
		t.Errorf("decorations = %v, want %v", got, want)
	}
	// Reverting to the very same conflicts still sets them again.
	e.h.Host.DecorClear("merge_conflict", "conflicts", e.buf)
	e.b.Info.Version.Gen++
	e.h.Emit(pluginapi.Event{Name: "after_file_revert", Data: map[string]any{"buf": e.buf, "path": "/w/a.txt"}})
	if got := e.decor(); !slices.Equal(got, want) {
		t.Errorf("decorations after a second revert = %v, want %v", got, want)
	}
}

func TestMergeConflictSkipsLargeAndVirtualBuffers(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]func(*pluginapi.BufferInfo){
		"large":   func(i *pluginapi.BufferInfo) { i.Large = true },
		"virtual": func(i *pluginapi.BufferInfo) { i.Virtual = true },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newMC(t, mcTwo)
			set(&e.b.Info)
			e.open()
			if n := len(e.findLinesCalls()); n != 0 {
				t.Errorf("FindLines calls = %d, want none", n)
			}
			if len(e.decor()) != 0 {
				t.Error("decorated")
			}
		})
	}
}

func TestMergeConflictUnloadLeavesNothing(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	if len(e.decor()) == 0 {
		t.Fatal("nothing decorated")
	}
	e.h.Unload("merge_conflict")
	for _, r := range e.h.Host.Resources() {
		t.Errorf("resource left: %s", r)
	}
}

func TestMergeConflictNavigation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		caret   int
		command string
		line    int
		notice  string
	}{
		{"next from before", mcTwo, 0, "next", 1, "Conflict 1 of 2"},
		{"next from the start marker", mcTwo, 1, "next", 8, "Conflict 2 of 2"},
		{"next from inside a conflict", mcTwo, 3, "next", 8, "Conflict 2 of 2"},
		{"next from between", mcTwo, 7, "next", 8, "Conflict 2 of 2"},
		{"next wraps from the last conflict", mcTwo, 12, "next", 1, "Conflict 1 of 2"},
		{"next wraps from after the last", mcTwo, 15, "next", 1, "Conflict 1 of 2"},
		{"prev from after", mcTwo, 15, "prev", 8, "Conflict 2 of 2"},
		{"prev from between", mcTwo, 7, "prev", 1, "Conflict 1 of 2"},
		{"prev from inside the second", mcTwo, 9, "prev", 1, "Conflict 1 of 2"},
		{"prev wraps from the first", mcTwo, 2, "prev", 8, "Conflict 2 of 2"},
		{"prev wraps from before", mcTwo, 0, "prev", 8, "Conflict 2 of 2"},
		{"a single conflict stays", "<<<<<<< a\nx\n=======\ny\n>>>>>>> b", 3, "next", 0, "Conflict 1 of 1"},
		{"no conflicts", "a\nb", 1, "next", 1, "No merge conflicts in this file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newMC(t, tt.text)
			e.open()
			e.setCaret(tt.caret)
			if err := e.h.Run("merge_conflict."+tt.command, nil); err != nil {
				t.Fatal(err)
			}
			if got := e.caretLine(); got != tt.line {
				t.Errorf("caret line = %d, want %d", got, tt.line)
			}
			if got := e.lastNotice(); got != tt.notice {
				t.Errorf("notice = %q, want %q", got, tt.notice)
			}
		})
	}
}

// TestMergeConflictResolutions: each choice is one edit that leaves exactly the chosen
// lines, whatever the position of the block in the file; one undo brings the markers back.
func TestMergeConflictResolutions(t *testing.T) {
	t.Parallel()
	plain := "a\n<<<<<<< H\nours\n=======\ntheirs\n>>>>>>> b\nz"
	diff3 := "a\n<<<<<<< H\nours\n||||||| base\nbase\n=======\ntheirs\n>>>>>>> b\nz"
	tests := []struct {
		name               string
		text               string
		caret              int
		ours, theirs, both string
	}{
		{"in the middle", plain, 2, "a\nours\nz", "a\ntheirs\nz", "a\nours\ntheirs\nz"},
		{"diff3 drops the base", diff3, 4, "a\nours\nz", "a\ntheirs\nz", "a\nours\ntheirs\nz"},
		{"at the end of the file",
			"a\n<<<<<<< H\nours\n=======\ntheirs\n>>>>>>> b", 1,
			"a\nours", "a\ntheirs", "a\nours\ntheirs"},
		{"at the start of the file",
			"<<<<<<< H\nours\n=======\ntheirs\n>>>>>>> b\nz", 0,
			"ours\nz", "theirs\nz", "ours\ntheirs\nz"},
		{"the whole file",
			"<<<<<<< H\nours\n=======\ntheirs\n>>>>>>> b", 0,
			"ours", "theirs", "ours\ntheirs"},
		{"empty ours",
			"a\n<<<<<<< H\n=======\ntheirs\n>>>>>>> b\nz", 1,
			"a\nz", "a\ntheirs\nz", "a\ntheirs\nz"},
		{"empty theirs at the end",
			"a\n<<<<<<< H\nours\n=======\n>>>>>>> b", 2,
			"a\nours", "a", "a\nours"},
		{"both empty at the end",
			"a\n<<<<<<<\n=======\n>>>>>>>", 1,
			"a", "a", "a"},
		{"a trailing newline stays",
			"a\n<<<<<<< H\nours\n=======\ntheirs\n>>>>>>> b\n", 3,
			"a\nours\n", "a\ntheirs\n", "a\nours\ntheirs\n"},
	}
	for _, tt := range tests {
		for _, c := range []struct{ command, want string }{
			{"use_ours", tt.ours}, {"take_theirs", tt.theirs}, {"use_both", tt.both},
		} {
			t.Run(tt.name+"/"+c.command, func(t *testing.T) {
				t.Parallel()
				e := newMC(t, tt.text)
				e.open()
				e.setCaret(tt.caret)
				if err := e.h.Run("merge_conflict."+c.command, nil); err != nil {
					t.Fatal(err)
				}
				if got := e.text(); got != c.want {
					t.Errorf("text = %q, want %q", got, c.want)
				}
				if n := e.h.Host.CallCount("Edit"); n != 1 {
					t.Errorf("Edit calls = %d, want one (one undo step)", n)
				}
				if got := e.decor(); len(got) != 0 {
					t.Errorf("decorations after resolving the only conflict = %v", got)
				}
				if got := e.lastNotice(); got != "Merge: all conflicts resolved" {
					t.Errorf("notice = %q", got)
				}
				if err := e.h.Host.Undo(e.buf); err != nil {
					t.Fatal(err)
				}
				if got := e.text(); got != tt.text {
					t.Errorf("text after one undo = %q, want the original", got)
				}
			})
		}
	}
}

func TestMergeConflictResolveTheOneUnderTheCaret(t *testing.T) {
	t.Parallel()
	e := newMC(t, mcTwo)
	e.open()
	e.setCaret(9) // the second conflict
	if err := e.h.Run("merge_conflict.take_theirs", nil); err != nil {
		t.Fatal(err)
	}
	want := "head\n<<<<<<< HEAD\nours1\nours2\n=======\ntheirs1\n>>>>>>> branch\nmiddle\nc\ntail"
	if got := e.text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	// The caret goes to the conflict that is left, which is the next one in the file (wrapped).
	if got := e.caretLine(); got != 1 {
		t.Errorf("caret line = %d, want 1", got)
	}
	if got := e.lastNotice(); got != "Take Theirs: 1 conflict remaining" {
		t.Errorf("notice = %q", got)
	}
	if got, want := e.markerLines(), []int{1, 4, 6}; !slices.Equal(got, want) {
		t.Errorf("marker lines = %v, want %v", got, want)
	}
}

func TestMergeConflictRefusals(t *testing.T) {
	t.Parallel()
	t.Run("the caret is outside a conflict", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, mcTwo)
		e.open()
		e.setCaret(7)
		if err := e.h.Run("merge_conflict.use_ours", nil); err != nil {
			t.Fatal(err)
		}
		if e.text() != mcTwo || e.lastNotice() != "Merge: the caret is not inside a conflict" {
			t.Errorf("text changed or notice = %q", e.lastNotice())
		}
	})
	t.Run("a read-only buffer", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, mcTwo)
		e.open()
		e.b.Info.ReadOnly = true
		e.setCaret(2)
		if err := e.h.Run("merge_conflict.use_ours", nil); err != nil {
			t.Fatal(err)
		}
		if e.text() != mcTwo {
			t.Error("a read-only buffer was edited")
		}
		if n := e.h.Notices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Msg, "Use Ours failed") || n[len(n)-1].Level != pluginapi.LevelWarn {
			t.Errorf("notices = %+v, want a warning that names the failure", n)
		}
	})
}

func TestMergeConflictResolveWithAPicker(t *testing.T) {
	t.Parallel()
	pick := func(t *testing.T, e *mcEnv, answer ...any) {
		t.Helper()
		o := e.h.Host.UI().Last("picker")
		if o == nil {
			t.Fatal("no picker")
		}
		var items []string
		for _, it := range o.Picker.Items {
			items = append(items, it.Text)
		}
		if want := []string{"Use Ours", "Take Theirs", "Use Both"}; !slices.Equal(items, want) {
			t.Fatalf("picker items = %v, want %v", items, want)
		}
		e.h.Resolve(pluginapi.Completion{Token: o.Token, Values: answer})
	}
	const text = "<<<<<<< a\nx\n=======\ny\n>>>>>>> b"
	t.Run("take theirs", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, text)
		e.open()
		if err := e.h.Run("merge_conflict.resolve", nil); err != nil {
			t.Fatal(err)
		}
		pick(t, e, 1)
		if got := e.text(); got != "y" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, text)
		e.open()
		if err := e.h.Run("merge_conflict.resolve", nil); err != nil {
			t.Fatal(err)
		}
		pick(t, e)
		if e.text() != text {
			t.Error("a cancelled picker edited the buffer")
		}
	})
	t.Run("not inside a conflict", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, "a\n"+text)
		e.open()
		if err := e.h.Run("merge_conflict.resolve", nil); err != nil {
			t.Fatal(err)
		}
		if e.h.Host.UI().Last("picker") != nil {
			t.Error("a picker was opened outside a conflict")
		}
	})
}

// TestMergeConflictResolutionMode: Start Resolution enters a key mode that navigates and
// resolves, and leaves it when the last conflict is gone or on q.
func TestMergeConflictResolutionMode(t *testing.T) {
	t.Parallel()
	const text = "<<<<<<< a\nx\n=======\ny\n>>>>>>> b\nmid\n<<<<<<< c\nz\n=======\nw\n>>>>>>> d"
	mode := func(e *mcEnv) pluginapi.ModeInfo {
		ms := e.h.Host.Modes["merge_conflict"]
		if len(ms) != 1 || ms[0].Name != "resolve" {
			e.h.T.Fatalf("modes = %+v", ms)
		}
		return ms[0]
	}
	key := func(e *mcEnv, chord string) {
		e.h.T.Helper()
		action := mode(e).Bindings[chord]
		if action == "" {
			e.h.T.Fatalf("mode has no binding for %q: %v", chord, mode(e).Bindings)
		}
		if err := e.h.Run(action, nil); err != nil {
			e.h.T.Fatal(err)
		}
	}
	start := func(t *testing.T) *mcEnv {
		e := newMC(t, text)
		e.open()
		e.setCaret(5) // between the conflicts
		if err := e.h.Run("merge_conflict.start", nil); err != nil {
			t.Fatal(err)
		}
		if got := e.h.Host.InputMode["merge_conflict"]; got != "resolve" {
			t.Fatalf("input mode = %q", got)
		}
		return e
	}

	m := mode(newMC(t, "x"))
	if m.Text != pluginapi.TextSwallow || !m.Fallthrough || m.Status != "-- MERGE --" {
		t.Errorf("mode = %+v: letters must not type, other keys fall through", m)
	}
	for _, chord := range []string{"n", "p", "]", "[", "u", "t", "b", "q", "escape", "?"} {
		if m.Bindings[chord] == "" {
			t.Errorf("no binding for %q", chord)
		}
	}

	t.Run("start goes to the next conflict", func(t *testing.T) {
		t.Parallel()
		e := start(t)
		if got := e.caretLine(); got != 6 {
			t.Errorf("caret line = %d, want 6", got)
		}
	})
	t.Run("navigate and resolve to the end", func(t *testing.T) {
		t.Parallel()
		e := start(t)
		key(e, "n") // wraps to the first
		if got := e.caretLine(); got != 0 {
			t.Fatalf("caret line = %d", got)
		}
		key(e, "t")
		key(e, "u")
		if got, want := e.text(), "y\nmid\nz"; got != want {
			t.Errorf("text = %q, want %q", got, want)
		}
		if got := e.h.Host.InputMode["merge_conflict"]; got != "" {
			t.Errorf("input mode = %q after the last conflict", got)
		}
	})
	t.Run("both and previous", func(t *testing.T) {
		t.Parallel()
		e := start(t)
		key(e, "p")
		key(e, "b")
		if got, want := e.text(), "x\ny\nmid\n<<<<<<< c\nz\n=======\nw\n>>>>>>> d"; got != want {
			t.Errorf("text = %q, want %q", got, want)
		}
		if got := e.h.Host.InputMode["merge_conflict"]; got != "resolve" {
			t.Errorf("input mode = %q, want the mode to go on while a conflict is left", got)
		}
	})
	for _, chord := range []string{"q", "escape"} {
		t.Run(chord+" leaves", func(t *testing.T) {
			t.Parallel()
			e := start(t)
			key(e, chord)
			if got := e.h.Host.InputMode["merge_conflict"]; got != "" {
				t.Errorf("input mode = %q", got)
			}
		})
	}
	t.Run("no conflicts, no mode", func(t *testing.T) {
		t.Parallel()
		e := newMC(t, "a\nb")
		e.open()
		if err := e.h.Run("merge_conflict.start", nil); err != nil {
			t.Fatal(err)
		}
		if got := e.h.Host.InputMode["merge_conflict"]; got != "" {
			t.Errorf("input mode = %q", got)
		}
		if got := e.lastNotice(); got != "No merge conflicts in this file" {
			t.Errorf("notice = %q", got)
		}
	})
}
