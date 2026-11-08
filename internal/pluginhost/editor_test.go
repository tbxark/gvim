package pluginhost

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/search"
)

// pluginEditorOver returns the plugin port over a new editor with one untitled tab for each
// text, the first active, and the ids of the tabs.
func pluginEditorOver(t *testing.T, texts ...string) (*pluginEditor, *editor.Editor, []pluginapi.BufferID) {
	t.Helper()
	ed := editor.New(editor.Options{})
	for i, text := range texts {
		if i > 0 {
			ed.Do("new", nil)
		}
		insertText(t, ed, text)
	}
	ed.ActivateTab(0)
	pe := newPluginEditor(ed, func(*editor.Document) {})
	var ids []pluginapi.BufferID
	for _, b := range pe.ListBuffers() {
		ids = append(ids, b.ID)
	}
	return pe, ed, ids
}

// insertText puts text at the start of the active document, through the buffer, and leaves
// the caret after it, as typing it would.
func insertText(t *testing.T, ed *editor.Editor, text string) {
	t.Helper()
	end := ed.Active().Buffer.Insert(buffer.Pos{}, text)
	ed.SetCursor(end, false)
}

func apos(l, c int) pluginapi.Pos { return pluginapi.Pos{Line: l, Col: c} }

func arange(l1, c1, l2, c2 int) pluginapi.Range {
	return pluginapi.Range{From: apos(l1, c1), To: apos(l2, c2)}
}

// FindBuffer matches files the way the editor does when it opens a path, so a plugin that
// opens a file the editor already has under another spelling (a symlink) gets that buffer.
func TestPluginEditorFindsBufferBySpelling(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	apptest.Must(t, os.Mkdir(real, 0o755))
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	apptest.Must(t, os.WriteFile(filepath.Join(real, "a.txt"), []byte("a\n"), 0o644))
	apptest.Must(t, os.WriteFile(filepath.Join(real, "b.txt"), []byte("b\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(filepath.Join(real, "a.txt")))
	apptest.Must(t, ed.OpenFile(filepath.Join(link, "b.txt")))
	pe := newPluginEditor(ed, func(*editor.Document) {})
	ids := map[string]pluginapi.BufferID{}
	for _, b := range pe.ListBuffers() {
		ids[filepath.Base(b.Path)] = b.ID
	}
	for name, spelling := range map[string]string{
		"a.txt": filepath.Join(link, "a.txt"),
		"b.txt": filepath.Join(real, "b.txt"),
	} {
		if got := pe.FindBuffer(spelling); got == 0 || got != ids[name] {
			t.Errorf("FindBuffer(%s) = %d, want %d", spelling, got, ids[name])
		}
	}
	if got := pe.FindBuffer(filepath.Join(link, "none.txt")); got != 0 {
		t.Errorf("FindBuffer(missing) = %d, want 0", got)
	}
}

func TestPluginEditorListsBuffers(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	apptest.Must(t, os.WriteFile(goFile, []byte("package main\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(goFile))
	ed.Do("new", nil)
	pe := newPluginEditor(ed, func(*editor.Document) {})

	list := pe.ListBuffers()
	if len(list) != 2 {
		t.Fatalf("buffers = %d, want 2", len(list))
	}
	g, u := list[0], list[1]
	if g.Path != goFile || g.Name != "main.go" || g.Language != "go" || g.Modified || g.Active || g.LineCount != 2 || g.Version.IsZero() {
		t.Errorf("file buffer = %+v", g)
	}
	if u.Path != "" || !u.Active || u.Language != "" || u.Large || u.ReadOnly {
		t.Errorf("untitled buffer = %+v", u)
	}
	if got := pe.ActiveBuffer(); got != u.ID {
		t.Errorf("ActiveBuffer = %d, want %d", got, u.ID)
	}
	if got := pe.FindBuffer(goFile); got != g.ID {
		t.Errorf("FindBuffer(%s) = %d, want %d", goFile, got, g.ID)
	}
	if got := pe.FindBuffer(filepath.Join(dir, "none.go")); got != 0 {
		t.Errorf("FindBuffer(missing) = %d, want 0", got)
	}
	if _, ok := pe.Buffer(999); ok {
		t.Error("Buffer(999) found a buffer")
	}

	// The id is kept by a tab move; the version changes with an edit but not its generation.
	before := g.Version
	apptest.Must(t, pe.Edit(g.ID, []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "// x\n"}}, pluginapi.EditOptions{}))
	after, _ := pe.Buffer(g.ID)
	if !after.Modified || after.Version == before || after.Version.Gen != before.Gen || after.LineCount != 3 {
		t.Errorf("after an edit: %+v (version before %v)", after, before)
	}
	apptest.Must(t, pe.MoveTab(g.ID, 1))
	moved := pe.ListBuffers()
	if moved[0].ID != u.ID || moved[1].ID != g.ID {
		t.Errorf("after MoveTab ids = %d, %d, want %d, %d", moved[0].ID, moved[1].ID, u.ID, g.ID)
	}

	// Language: set, read back, and refuse an unknown one.
	if err := pe.SetLanguage(g.ID, "no-such-language"); codeOf(err) != pluginapi.CodeInvalid {
		t.Errorf("SetLanguage(unknown) = %v, want invalid", err)
	}
	apptest.Must(t, pe.SetLanguage(g.ID, "python"))
	if info, _ := pe.Buffer(g.ID); info.Language != "python" {
		t.Errorf("language = %q, want python", info.Language)
	}
	apptest.Must(t, pe.SetLanguage(g.ID, ""))
	if info, _ := pe.Buffer(g.ID); info.Language != "go" {
		t.Errorf("language after clearing = %q, want go from the file name", info.Language)
	}
}

// TestPluginEditorRangeRules checks the position rules of AMENDMENTS C24 on text with a
// multibyte character: columns are bytes, a column inside a character rounds down, a column
// past the line end clamps to the line end, and a line past the end clamps to the end of the
// document.
func TestPluginEditorRangeRules(t *testing.T) {
	pe, _, ids := pluginEditorOver(t, "héllo\nwörld\nlast")
	id := ids[0]
	tests := []struct {
		name string
		r    pluginapi.Range
		want string
	}{
		{"within a line", arange(0, 0, 0, 3), "hé"},
		{"column inside a character rounds down", arange(0, 0, 0, 2), "h"},
		{"both ends inside characters", arange(0, 2, 1, 2), "éllo\nw"},
		{"to the next line start keeps the newline", arange(0, 0, 1, 0), "héllo\n"},
		{"column past the line end clamps", arange(0, 3, 0, 99), "llo"},
		{"column past the end of the line before the newline", arange(0, 0, 0, 1000), "héllo"},
		{"line past the document clamps to the end", arange(1, 0, 50, 0), "wörld\nlast"},
		{"to the line after the last clamps", arange(2, 0, 3, 0), "last"},
		{"negative positions clamp to the start", arange(-5, -5, 0, 1), "h"},
		{"a reversed range is taken in order", arange(0, 3, 0, 0), "hé"},
		{"empty", arange(1, 1, 1, 1), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pe.Slice(id, tt.r)
			if err != nil || got != tt.want {
				t.Fatalf("Slice(%v) = %q, %v, want %q", tt.r, got, err, tt.want)
			}
		})
	}

	// Offsets and positions.
	text := "héllo\nwörld\nlast"
	if n, err := pe.ByteLength(id); err != nil || n != len(text) {
		t.Errorf("ByteLength = %d, %v, want %d", n, err, len(text))
	}
	offsets := []struct {
		pos  pluginapi.Pos
		want int
	}{{apos(0, 0), 0}, {apos(0, 3), 3}, {apos(0, 2), 1}, {apos(1, 0), 7}, {apos(2, 4), len(text)}, {apos(9, 9), len(text)}, {apos(0, 99), 6}}
	for _, o := range offsets {
		if got, err := pe.OffsetOf(id, o.pos); err != nil || got != o.want {
			t.Errorf("OffsetOf(%v) = %d, %v, want %d", o.pos, got, err, o.want)
		}
	}
	for off := 0; off <= len(text); off++ {
		if !utf8StartAt(text, off) {
			continue
		}
		pos, err := pe.PosOf(id, off)
		if err != nil {
			t.Fatal(err)
		}
		if back, _ := pe.OffsetOf(id, pos); back != off {
			t.Errorf("OffsetOf(PosOf(%d)) = %d", off, back)
		}
	}
	if pos, _ := pe.PosOf(id, -4); pos != apos(0, 0) {
		t.Errorf("PosOf(-4) = %v", pos)
	}
	if pos, _ := pe.PosOf(id, 1<<20); pos != apos(2, 4) {
		t.Errorf("PosOf(past the end) = %v", pos)
	}

	// Lines.
	for _, c := range []struct {
		first, count int
		want         []string
	}{
		{0, 2, []string{"héllo", "wörld"}},
		{1, -1, []string{"wörld", "last"}},
		{2, 10, []string{"last"}},
		{3, 5, nil},
		{-2, 1, []string{"héllo"}},
	} {
		if got, err := pe.Lines(id, c.first, c.count); err != nil || !slices.Equal(got, c.want) {
			t.Errorf("Lines(%d, %d) = %q, %v, want %q", c.first, c.count, got, err, c.want)
		}
	}
	if _, err := pe.Slice(777, arange(0, 0, 0, 1)); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("Slice on a closed buffer: %v, want closed", err)
	}
}

func utf8StartAt(s string, i int) bool { return i == len(s) || (s[i]&0xC0) != 0x80 }

func TestPluginEditorEdit(t *testing.T) {
	type row struct {
		name   string
		text   string
		edits  []pluginapi.Edit
		opts   pluginapi.EditOptions
		want   string
		code   pluginapi.Code
		cursor buffer.Pos // the primary caret afterwards, when check is set
		check  bool
	}
	rows := []row{
		{name: "insert", text: "abc", edits: []pluginapi.Edit{{Range: arange(0, 1, 0, 1), Text: "X"}}, want: "aXbc"},
		{name: "replace in pre-edit coordinates, unsorted", text: "one\ntwo\nthree",
			edits: []pluginapi.Edit{{Range: arange(2, 0, 2, 5), Text: "3"}, {Range: arange(0, 0, 0, 3), Text: "1"}}, want: "1\ntwo\n3"},
		{name: "a column inside a character rounds down", text: "héllo", edits: []pluginapi.Edit{{Range: arange(0, 2, 0, 2), Text: "|"}}, want: "h|éllo"},
		{name: "delete across lines", text: "ab\ncd\nef", edits: []pluginapi.Edit{{Range: arange(0, 1, 2, 1), Text: ""}}, want: "af"},
		{name: "range past the end clamps", text: "ab\ncd", edits: []pluginapi.Edit{{Range: arange(1, 1, 99, 99), Text: "!"}}, want: "ab\nc!"},
		{name: "line breaks in the text are LF", text: "ab", edits: []pluginapi.Edit{{Range: arange(0, 1, 0, 1), Text: "\r\n-\r-"}}, want: "a\n-\n-b"},
		{name: "touching edits are not overlapping", text: "abcd",
			edits: []pluginapi.Edit{{Range: arange(0, 0, 0, 2), Text: "X"}, {Range: arange(0, 2, 0, 4), Text: "Y"}}, want: "XY"},
		{name: "overlapping edits", text: "abcd",
			edits: []pluginapi.Edit{{Range: arange(0, 0, 0, 3), Text: "X"}, {Range: arange(0, 2, 0, 4), Text: "Y"}}, want: "abcd", code: pluginapi.CodeInvalid},
		{name: "no edits", text: "abcd", want: "abcd"},
		{name: "cursor mode end", text: "abcd", edits: []pluginapi.Edit{{Range: arange(0, 0, 0, 1), Text: "XYZ"}}, opts: pluginapi.EditOptions{Cursor: pluginapi.CursorEnd},
			want: "XYZbcd", cursor: buffer.Pos{Col: 3}, check: true},
		{name: "cursor mode keep", text: "abcd", edits: []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "XYZ"}}, opts: pluginapi.EditOptions{Cursor: pluginapi.CursorKeep},
			want: "XYZabcd", cursor: buffer.Pos{Col: 1}, check: true},
		{name: "cursor mode map moves the caret with the text", text: "abcd", edits: []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "XYZ"}},
			want: "XYZabcd", cursor: buffer.Pos{Col: 7}, check: true},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			pe, ed, ids := pluginEditorOver(t, tt.text)
			if tt.check && tt.opts.Cursor == pluginapi.CursorKeep {
				// "keep" leaves the caret in place; put it in the middle first.
				ed.SetCursor(buffer.Pos{Col: 1}, false)
			}
			err := pe.Edit(ids[0], tt.edits, tt.opts)
			if codeOf(err) != tt.code {
				t.Fatalf("Edit error = %v, want code %q", err, tt.code)
			}
			if got := ed.Active().Buffer.Text(); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if tt.check && ed.Active().Cursor != tt.cursor {
				t.Fatalf("caret = %v, want %v", ed.Active().Cursor, tt.cursor)
			}
		})
	}
}

func TestPluginEditorEditIsOneUndoStep(t *testing.T) {
	pe, ed, ids := pluginEditorOver(t, "one\ntwo\nthree")
	id := ids[0]
	apptest.Must(t, pe.Edit(id, []pluginapi.Edit{
		{Range: arange(0, 0, 0, 3), Text: "1"}, {Range: arange(1, 0, 1, 3), Text: "2"}, {Range: arange(2, 0, 2, 5), Text: "3"},
	}, pluginapi.EditOptions{}))
	if got := ed.Active().Buffer.Text(); got != "1\n2\n3" {
		t.Fatalf("text = %q", got)
	}
	apptest.Must(t, pe.Undo(id))
	if got := ed.Active().Buffer.Text(); got != "one\ntwo\nthree" {
		t.Fatalf("text after one Undo = %q, want the original", got)
	}
	apptest.Must(t, pe.Redo(id))
	if got := ed.Active().Buffer.Text(); got != "1\n2\n3" {
		t.Fatalf("text after Redo = %q", got)
	}
	// Nothing left to redo.
	if err := pe.Redo(id); codeOf(err) != pluginapi.CodeNotFound {
		t.Errorf("Redo with nothing to redo = %v, want not_found", err)
	}

	// UndoGroup makes the edits of several calls one step.
	err := pe.UndoGroup(id, func() error {
		apptest.Must(t, pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "a"}}, pluginapi.EditOptions{}))
		return pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 1, 0, 1), Text: "b"}}, pluginapi.EditOptions{})
	})
	apptest.Must(t, err)
	if got := ed.Active().Buffer.Text(); got != "ab1\n2\n3" {
		t.Fatalf("text after the group = %q", got)
	}
	apptest.Must(t, pe.Undo(id))
	if got := ed.Active().Buffer.Text(); got != "1\n2\n3" {
		t.Fatalf("text after undoing the group = %q, want both edits undone", got)
	}
	// The error of the body comes back as it is.
	want := pluginapi.Errf(pluginapi.CodeBusy, "body failed")
	if err := pe.UndoGroup(id, func() error { return want }); err != want {
		t.Errorf("UndoGroup error = %v, want %v", err, want)
	}
	if err := pe.UndoGroup(404, func() error { return nil }); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("UndoGroup on a closed buffer = %v", err)
	}
}

func TestPluginEditorEditRefusals(t *testing.T) {
	t.Run("read-only", func(t *testing.T) {
		pe, ed, ids := pluginEditorOver(t, "abc")
		ed.Do("toggle_read_only", nil)
		err := pe.Edit(ids[0], []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "x"}}, pluginapi.EditOptions{})
		if codeOf(err) != pluginapi.CodeReadOnly {
			t.Fatalf("Edit on a read-only buffer = %v", err)
		}
		if err := pe.Undo(ids[0]); codeOf(err) != pluginapi.CodeReadOnly {
			t.Fatalf("Undo on a read-only buffer = %v", err)
		}
	})
	t.Run("stale version", func(t *testing.T) {
		pe, ed, ids := pluginEditorOver(t, "abc")
		info, _ := pe.Buffer(ids[0])
		v := info.Version
		edit := []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "x"}}
		apptest.Must(t, pe.Edit(ids[0], edit, pluginapi.EditOptions{Expect: &v}))
		err := pe.Edit(ids[0], edit, pluginapi.EditOptions{Expect: &v})
		if codeOf(err) != pluginapi.CodeStale || !strings.Contains(err.Error(), v.String()) {
			t.Fatalf("Edit with an old version = %v, want stale naming the version", err)
		}
		if got := ed.Active().Buffer.Text(); got != "xabc" {
			t.Fatalf("text = %q, the stale edit changed it", got)
		}
		// Undo returns the same version token as before the edit.
		apptest.Must(t, pe.Undo(ids[0]))
		back, _ := pe.Buffer(ids[0])
		if back.Version != v {
			t.Fatalf("version after undo = %v, want %v", back.Version, v)
		}
		// A version of another generation never matches.
		other := pluginapi.Version{Gen: v.Gen + 100, Ver: v.Ver}
		if err := pe.Edit(ids[0], edit, pluginapi.EditOptions{Expect: &other}); codeOf(err) != pluginapi.CodeStale {
			t.Fatalf("Edit with another generation = %v", err)
		}
	})
	t.Run("too many edits", func(t *testing.T) {
		pe, ed, ids := pluginEditorOver(t, "abc")
		edits := make([]pluginapi.Edit, pluginMaxEdits+1)
		err := pe.Edit(ids[0], edits, pluginapi.EditOptions{})
		if codeOf(err) != pluginapi.CodeLimit {
			t.Fatalf("Edit with %d edits = %v, want limit", len(edits), err)
		}
		if ed.Active().Buffer.Text() != "abc" {
			t.Fatal("text changed")
		}
	})
	t.Run("closed", func(t *testing.T) {
		pe, _, _ := pluginEditorOver(t, "abc")
		if err := pe.Edit(99, nil, pluginapi.EditOptions{}); codeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("Edit on a closed buffer = %v", err)
		}
	})
}

// TestPluginEditorEditByteCap checks both halves of the 8 MiB cap of AMENDMENTS C18: the
// bytes an edit inserts and the bytes it removes.
func TestPluginEditorEditByteCap(t *testing.T) {
	big := strings.Repeat("x", pluginMaxEditBytes/2+1) // half the cap plus one byte
	pe, ed, ids := pluginEditorOver(t, "")
	ed.Active().Buffer.Insert(buffer.Pos{}, big+"\n"+big+"\nend")
	id := ids[0]
	before := ed.Active().Buffer.Version()

	tests := []struct {
		name  string
		edits []pluginapi.Edit
		code  pluginapi.Code
	}{
		{"removing more than the cap", []pluginapi.Edit{{Range: arange(0, 0, 2, 0), Text: ""}}, pluginapi.CodeLimit},
		{"removing a range past the end counts the clamped text", []pluginapi.Edit{{Range: arange(0, 0, 1<<30, 0), Text: ""}}, pluginapi.CodeLimit},
		{"inserting more than the cap", []pluginapi.Edit{{Range: arange(2, 0, 2, 0), Text: strings.Repeat("y", pluginMaxEditBytes+1)}}, pluginapi.CodeLimit},
		{"insert plus remove over the cap", []pluginapi.Edit{
			{Range: arange(0, 0, 1, 0), Text: ""}, {Range: arange(1, 0, 1, 0), Text: strings.Repeat("y", pluginMaxEditBytes/2)}}, pluginapi.CodeLimit},
		{"many small removals add up", []pluginapi.Edit{
			{Range: arange(0, 0, 0, pluginMaxEditBytes/2+1), Text: ""}, {Range: arange(1, 0, 1, pluginMaxEditBytes/2+1), Text: ""}}, pluginapi.CodeLimit},
		{"within the cap", []pluginapi.Edit{{Range: arange(0, 0, 0, 100), Text: ""}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pe.Edit(id, tt.edits, pluginapi.EditOptions{})
			if codeOf(err) != tt.code {
				t.Fatalf("Edit = %v, want code %q", err, tt.code)
			}
			if tt.code != "" && ed.Active().Buffer.Version() != before {
				t.Fatal("a refused edit changed the buffer")
			}
		})
	}
}

// TestPluginEditNonActiveDocMarksSpansStale: an edit through the plugin port marks the
// spans of the document it changed stale, also when that document is not the active tab,
// and leaves the spans of the other open documents alone.
func TestPluginEditNonActiveDocMarksSpansStale(t *testing.T) {
	ed := editor.New(editor.Options{})
	insertText(t, ed, "first")
	ed.Do("new", nil)
	insertText(t, ed, "second")
	var edited []*editor.Document
	pe := newPluginEditor(ed, func(d *editor.Document) { edited = append(edited, d) })
	other := ed.DocAt(0)
	if ed.Active() == other {
		t.Fatal("the first tab is active")
	}
	marked := func(d *editor.Document) bool {
		for _, e := range edited {
			if e == d {
				return true
			}
		}
		return false
	}

	id := pluginapi.BufferID(other.ID())
	edited = nil
	apptest.Must(t, pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "x"}}, pluginapi.EditOptions{}))
	if !marked(other) {
		t.Error("the edited document's spans are not stale")
	}
	if marked(ed.Active()) {
		t.Error("the active document was marked stale by an edit to another tab")
	}

	// An edit to the active document marks that document.
	edited = nil
	active := ed.Active()
	apptest.Must(t, pe.Edit(pluginapi.BufferID(active.ID()), []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "y"}}, pluginapi.EditOptions{}))
	if !marked(active) {
		t.Error("the active document is not marked stale after an edit to it")
	}

	// The other mutating calls mark the document too (in this order: Redo needs the
	// Undo before it).
	for _, c := range []struct {
		name string
		call func() error
	}{
		{"Undo", func() error { return pe.Undo(id) }},
		{"Redo", func() error { return pe.Redo(id) }},
		{"SetLanguage", func() error { return pe.SetLanguage(id, "go") }},
		{"UndoGroup", func() error { return pe.UndoGroup(id, func() error { return nil }) }},
	} {
		edited = nil
		apptest.Must(t, c.call())
		if !marked(other) {
			t.Errorf("%s left the edited document's spans fresh", c.name)
		}
	}
}

func TestPluginEditorCarets(t *testing.T) {
	pe, ed, ids := pluginEditorOver(t, "héllo\nwörld\nlast")
	id := ids[0]
	caret := func() pluginapi.Caret {
		cs, err := pe.Cursors(id)
		if err != nil || len(cs) == 0 {
			t.Fatalf("Cursors = %v, %v", cs, err)
		}
		return cs[0]
	}
	// A byte column is converted to a rune column.
	apptest.Must(t, pe.SetCursor(id, apos(0, 3), false, false, false))
	if ed.Active().Cursor != (buffer.Pos{Line: 0, Col: 2}) {
		t.Errorf("cursor = %v, want rune column 2", ed.Active().Cursor)
	}
	if c := caret(); c.Pos != apos(0, 3) || c.Anchor != nil {
		t.Errorf("caret = %+v, want 0:3 without anchor", c)
	}
	// Extend grows a selection from the old position.
	apptest.Must(t, pe.SetCursor(id, apos(1, 4), true, false, false))
	if c := caret(); c.Pos != apos(1, 4) || c.Anchor == nil || *c.Anchor != apos(0, 3) {
		t.Errorf("caret after extend = %+v, want anchor 0:3 head 1:4", c)
	}
	// SetSelection puts the caret at the end.
	apptest.Must(t, pe.SetSelection(id, arange(0, 0, 1, 3)))
	if c := caret(); c.Pos != apos(1, 3) || c.Anchor == nil || *c.Anchor != apos(0, 0) {
		t.Errorf("caret after SetSelection = %+v", c)
	}
	// A position outside the text is clamped, not refused.
	apptest.Must(t, pe.SetCursor(id, apos(99, 99), false, false, false))
	if c := caret(); c.Pos != apos(2, 4) {
		t.Errorf("caret after SetCursor(99,99) = %+v, want the end of the text", c)
	}
	// Extra carets follow the primary one, in byte columns.
	ed.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
	ed.Do("add_cursor_below", nil)
	cs, _ := pe.Cursors(id)
	if len(cs) != 2 || cs[0].Pos.Line+cs[1].Pos.Line != 1 || cs[0].Pos.Line == cs[1].Pos.Line {
		t.Errorf("carets = %+v, want one on line 0 and one on line 1, the primary first", cs)
	}
	if err := pe.SetCursor(88, apos(0, 0), false, false, false); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("SetCursor on a closed buffer = %v", err)
	}
}

func TestPluginEditorReveal(t *testing.T) {
	ed := editor.New(editor.Options{Rows: 10, Cols: 40})
	for range 99 {
		insertText(t, ed, "line\n")
	}
	ed.ScrollTo(0)
	ed.SetCursor(buffer.Pos{}, false)
	pe := newPluginEditor(ed, func(*editor.Document) {})
	id := pluginapi.BufferID(ed.Active().ID())

	apptest.Must(t, pe.SetCursor(id, apos(60, 0), false, false, false))
	if top := ed.Active().Top; top != 0 {
		t.Errorf("top = %d after SetCursor without reveal, want 0", top)
	}
	apptest.Must(t, pe.SetCursor(id, apos(60, 0), false, true, false))
	if d := ed.Active(); d.Cursor.Line < d.Top || d.Cursor.Line >= d.Top+10 {
		t.Errorf("caret line %d is out of view (top %d) after reveal", d.Cursor.Line, d.Top)
	}
	apptest.Must(t, pe.SetCursor(id, apos(80, 0), false, false, true))
	if d := ed.Active(); d.Top != 80-5 {
		t.Errorf("top = %d after center, want %d", d.Top, 80-5)
	}
}

func TestPluginEditorSearch(t *testing.T) {
	pe, _, ids := pluginEditorOver(t, "héllo Foo\nfoo bar\nFOO")
	id := ids[0]
	tests := []struct {
		name string
		q    pluginapi.SearchQuery
		want []pluginapi.Range
		trun bool
		code pluginapi.Code
	}{
		{"case insensitive literal, byte columns", pluginapi.SearchQuery{Text: "foo"},
			[]pluginapi.Range{arange(0, 7, 0, 10), arange(1, 0, 1, 3), arange(2, 0, 2, 3)}, false, ""},
		{"case sensitive", pluginapi.SearchQuery{Text: "foo", CaseSensitive: true}, []pluginapi.Range{arange(1, 0, 1, 3)}, false, ""},
		{"regex", pluginapi.SearchQuery{Text: `h.llo`, Regex: true}, []pluginapi.Range{arange(0, 0, 0, 6)}, false, ""},
		{"whole word", pluginapi.SearchQuery{Text: "fo", WholeWord: true}, nil, false, ""},
		{"max truncates", pluginapi.SearchQuery{Text: "foo", Max: 2}, []pluginapi.Range{arange(0, 7, 0, 10), arange(1, 0, 1, 3)}, true, ""},
		{"within", pluginapi.SearchQuery{Text: "foo", Within: &pluginapi.Range{From: apos(1, 0), To: apos(2, 0)}}, []pluginapi.Range{arange(1, 0, 1, 3)}, false, ""},
		{"bad regex", pluginapi.SearchQuery{Text: "(", Regex: true}, nil, false, pluginapi.CodeInvalid},
		{"empty query", pluginapi.SearchQuery{Text: ""}, nil, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := pe.Search(id, tt.q)
			if codeOf(err) != tt.code {
				t.Fatalf("Search error = %v, want code %q", err, tt.code)
			}
			if !slices.Equal(res.Ranges, tt.want) || res.Truncated != tt.trun {
				t.Fatalf("Search = %+v, want %v (truncated %v)", res, tt.want, tt.trun)
			}
		})
	}

	lines, err := pe.FindLines(id, pluginapi.FindLinesQuery{Pattern: "foo"})
	if err != nil || !slices.Equal(lines.Lines, []int{0, 1, 2}) || lines.Next != -1 {
		t.Errorf("FindLines = %+v, %v", lines, err)
	}
	lines, _ = pe.FindLines(id, pluginapi.FindLinesQuery{Pattern: "foo", CaseSensitive: true, From: 1, To: 3})
	if !slices.Equal(lines.Lines, []int{1}) || lines.Next != -1 {
		t.Errorf("FindLines in [1,3) = %+v", lines)
	}
	lines, _ = pe.FindLines(id, pluginapi.FindLinesQuery{Pattern: "foo", Max: 1})
	if !slices.Equal(lines.Lines, []int{0}) || lines.Next != 1 {
		t.Errorf("FindLines with Max 1 = %+v, want line 0 and Next 1", lines)
	}
	if res, err := pe.FindLines(id, pluginapi.FindLinesQuery{Pattern: "(", Regex: true}); codeOf(err) != pluginapi.CodeInvalid || res.Next != -1 {
		t.Errorf("FindLines with a bad regex = %+v, %v", res, err)
	}
}

func TestPluginEditorTakeChanges(t *testing.T) {
	pe, _, ids := pluginEditorOver(t, "héllo\nworld")
	id := ids[0]
	pe.TrackChanges("events", true)
	pe.TrackChanges("decor", true)
	t.Cleanup(func() { pe.TrackChanges("events", false); pe.TrackChanges("decor", false) })

	apptest.Must(t, pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 3, 0, 4), Text: "L"}, {Range: arange(1, 0, 1, 0), Text: "+"}}, pluginapi.EditOptions{}))
	ch := pe.TakeChanges(id, "events")
	if ch.Overflow || ch.Undo || ch.Version.IsZero() {
		t.Fatalf("changes = %+v", ch)
	}
	// Applying the records first to last to a mirror of the text gives the buffer's text.
	mirror := "héllo\nworld"
	for _, r := range ch.Edits {
		mirror = applyRec(t, mirror, r)
	}
	if mirror != "héLlo\n+world" {
		t.Fatalf("mirror after the records = %q (records %+v)", mirror, ch.Edits)
	}
	// The consumers have their own cursors.
	if again := pe.TakeChanges(id, "events"); len(again.Edits) != 0 || again.Overflow {
		t.Errorf("second take = %+v, want nothing", again)
	}
	if other := pe.TakeChanges(id, "decor"); len(other.Edits) != len(ch.Edits) {
		t.Errorf("the decor consumer got %d records, want %d", len(other.Edits), len(ch.Edits))
	}
	apptest.Must(t, pe.Undo(id))
	if un := pe.TakeChanges(id, "events"); !un.Undo || len(un.Edits) == 0 {
		t.Errorf("changes after an undo = %+v, want Undo set", un)
	}
	if got := pe.TakeChanges(404, "events"); len(got.Edits) != 0 || got.Overflow {
		t.Errorf("changes of an unknown buffer = %+v", got)
	}
}

// applyRec applies one change record, in byte coordinates, to text.
func applyRec(t *testing.T, text string, r pluginapi.ChangeRec) string {
	t.Helper()
	off := func(p pluginapi.Pos) int {
		lines := strings.Split(text, "\n")
		n := 0
		for i := 0; i < p.Line; i++ {
			n += len(lines[i]) + 1
		}
		return n + p.Col
	}
	from, to := off(r.Range.From), off(r.Range.To)
	if r.Removed != "" && text[from:to] != r.Removed {
		t.Fatalf("record removes %q but the text there is %q", r.Removed, text[from:to])
	}
	return text[:from] + r.Text + text[to:]
}

func TestPluginEditorTabs(t *testing.T) {
	pe, ed, ids := pluginEditorOver(t, "a", "b", "c")
	if len(ids) != 3 {
		t.Fatalf("ids = %v", ids)
	}
	apptest.Must(t, pe.Activate(ids[2]))
	if pe.ActiveBuffer() != ids[2] {
		t.Errorf("active = %d, want %d", pe.ActiveBuffer(), ids[2])
	}
	if err := pe.Activate(55); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("Activate(unknown) = %v", err)
	}
	// A modified tab is a conflict without force, and no dialog opens. The refusal does not
	// write to the status message of the active tab.
	status := ed.Status()
	if err := pe.Close(ids[1], false); codeOf(err) != pluginapi.CodeConflict {
		t.Errorf("Close of a modified tab = %v, want conflict", err)
	}
	if got := ed.Status(); got != status {
		t.Errorf("status = %q after the refused close, was %q", got, status)
	}
	if len(ed.Tabs()) != 3 {
		t.Errorf("tabs = %d after the refused close", len(ed.Tabs()))
	}
	apptest.Must(t, pe.Close(ids[1], true))
	if got := len(ed.Tabs()); got != 2 {
		t.Errorf("tabs = %d after the forced close", got)
	}
	if err := pe.Close(ids[1], true); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("Close of a closed tab = %v", err)
	}
	// Ids of the others did not change.
	if _, ok := pe.Buffer(ids[0]); !ok {
		t.Error("the id of the first tab is gone")
	}
	apptest.Must(t, pe.MoveTab(ids[2], -10))
	if first := pe.ListBuffers()[0].ID; first != ids[2] {
		t.Errorf("first tab = %d after MoveTab, want %d", first, ids[2])
	}
	if err := pe.MoveTab(77, 1); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("MoveTab(unknown) = %v", err)
	}
}

func TestPluginEditorFullTextAndSavedLines(t *testing.T) {
	apptest.IsolateConfig(t)
	path := filepath.Join(t.TempDir(), "f.txt")
	apptest.Must(t, os.WriteFile(path, []byte("one\ntwo\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	ed.Do("new", nil)
	pe := newPluginEditor(ed, func(*editor.Document) {})
	file, untitled := pe.ListBuffers()[0], pe.ListBuffers()[1]

	text, v, err := pe.FullText(file.ID)
	if err != nil || text != "one\ntwo\n" || v != file.Version {
		t.Fatalf("FullText = %q, %v, %v", text, v, err)
	}
	apptest.Must(t, pe.Edit(file.ID, []pluginapi.Edit{{Range: arange(0, 0, 0, 3), Text: "1"}}, pluginapi.EditOptions{}))
	if text, v2, _ := pe.FullText(file.ID); text != "1\ntwo\n" || v2 == v {
		t.Errorf("FullText after an edit = %q, %v", text, v2)
	}
	saved, err := pe.SavedLines(file.ID)
	if err != nil || !slices.Equal(saved, []string{"one", "two", ""}) {
		t.Errorf("SavedLines = %q, %v, want the text at open", saved, err)
	}
	if _, err := pe.SavedLines(untitled.ID); codeOf(err) != pluginapi.CodeNotFound {
		t.Errorf("SavedLines of an untitled buffer = %v, want not_found", err)
	}
	if _, _, err := pe.FullText(404); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("FullText(unknown) = %v", err)
	}
}

func TestEditorErrMapping(t *testing.T) {
	tests := []struct {
		err  error
		want pluginapi.Code
	}{
		{nil, ""},
		{editor.ErrReadOnly, pluginapi.CodeReadOnly},
		{editor.ErrNotOpen, pluginapi.CodeClosed},
		{editor.ErrStale, pluginapi.CodeStale},
		{editor.ErrOverlappingEdits, pluginapi.CodeInvalid},
		{editor.ErrUnsaved, pluginapi.CodeConflict},
		{editor.ErrRestoring, pluginapi.CodeConflict},
		{editor.ErrIndexing, pluginapi.CodeBusy},
		{editor.ErrLargeFile, pluginapi.CodeTooLarge},
		{buffer.ErrLargeBuffer, pluginapi.CodeTooLarge},
		{buffer.ErrOverCap, pluginapi.CodeTooLarge},
		{search.ErrRangeTooLarge, pluginapi.CodeTooLarge},
		{search.ErrTooCostly, pluginapi.CodeLimit},
		{errors.New("something else"), pluginapi.CodeInvalid},
		{pluginapi.Errf(pluginapi.CodeBusy, "already one"), pluginapi.CodeBusy},
	}
	for _, tt := range tests {
		if got := codeOf(editorErr(tt.err)); got != tt.want {
			t.Errorf("editorErr(%v) code = %q, want %q", tt.err, got, tt.want)
		}
	}
}

// chroma's lexers.Get also matches the name against every lexer's file name globs, which
// costs about 0.4 ms per byte; a plugin may pass a name of megabytes. A name longer than any
// lexer name is refused before the lookup, and the error does not echo it.
func TestPluginLanguageNameIsBoundedBeforeLookup(t *testing.T) {
	pe, _, ids := pluginEditorOver(t, "x")
	long := strings.Repeat("x", 200_000)
	start := time.Now()
	err := pe.SetLanguage(ids[0], long)
	_, err2 := pe.CreateBuffer(pluginapi.CreateBuffer{Name: "n", Language: long})
	if took := time.Since(start); took > time.Second {
		t.Errorf("two long language names took %v", took)
	}
	for _, e := range []error{err, err2} {
		if codeOf(e) != pluginapi.CodeInvalid || len(e.Error()) > 200 {
			t.Errorf("error = %.100q (%d bytes), want a short invalid error", e, len(e.Error()))
		}
	}
	apptest.Must(t, pe.SetLanguage(ids[0], "Common Lisp"))
}

// A match converts to byte columns, which costs the length of its line. Converting each of
// 100 000 matches of one long line (a minified file) from the line start took seconds on the
// UI goroutine, so the conversion walks the line once.
func TestPluginEditorSearchOnOneLongLineIsLinear(t *testing.T) {
	const n = 100_000
	for _, unit := range []string{"a", "é"} {
		pe, _, ids := pluginEditorOver(t, strings.Repeat(unit, n))
		start := time.Now()
		res, err := pe.Search(ids[0], pluginapi.SearchQuery{Text: unit, Max: n})
		took := time.Since(start)
		apptest.Must(t, err)
		if len(res.Ranges) != n {
			t.Fatalf("%q: %d matches, want %d", unit, len(res.Ranges), n)
		}
		if took > time.Second {
			t.Errorf("%q: %d matches on one line took %v", unit, n, took)
		}
		w := len(unit)
		last := res.Ranges[n-1]
		if want := (pluginapi.Range{From: pluginapi.Pos{Col: (n - 1) * w}, To: pluginapi.Pos{Col: n * w}}); last != want {
			t.Errorf("%q: last range = %v, want %v", unit, last, want)
		}
	}
}
