package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// virtualEditor returns an editor with one regular untitled tab and one virtual tab.
func virtualEditor(t *testing.T, o VirtualOptions, opts ...Options) (*Editor, DocID) {
	t.Helper()
	var op Options
	if len(opts) > 0 {
		op = opts[0]
	}
	e := New(op)
	return e, e.NewVirtual(o)
}

func TestNewVirtual(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		opts     VirtualOptions
		wantName string
	}{
		{"plain", VirtualOptions{Name: "outline", Text: "a\nb"}, "*outline*"},
		{"trimmed", VirtualOptions{Name: "  spaced  "}, "*spaced*"},
		{"control characters become blanks", VirtualOptions{Name: "a\x1b[31mb"}, "*a [31mb*"},
		{"empty name gets a default", VirtualOptions{Name: " \t"}, "*virtual*"},
		{"long name is cut", VirtualOptions{Name: strings.Repeat("é", 200)}, "*" + strings.Repeat("é", maxVirtualName) + "*"},
		{"crlf text is lf", VirtualOptions{Name: "x", Text: "a\r\nb"}, "*x*"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			e, id := virtualEditor(t, tt.opts)
			d, i, ok := e.DocByID(id)
			if !ok {
				t.Fatal("the new document is not open")
			}
			if !d.Virtual() || d.Path != "" || d.Name() != tt.wantName {
				t.Fatalf("virtual=%v path=%q name=%q, want a virtual document named %q", d.Virtual(), d.Path, d.Name(), tt.wantName)
			}
			if i != 1 || e.Active() == d {
				t.Fatalf("tab %d, active=%v: want the last tab, not activated", i, e.Active() == d)
			}
			if tabs := e.Tabs(); tabs[i].Name != tt.wantName || tabs[i].Modified || tabs[i].Path != "" {
				t.Fatalf("tab = %+v", tabs[i])
			}
			if strings.Contains(d.Buffer.Text(), "\r") {
				t.Fatalf("text %q still has a carriage return", d.Buffer.Text())
			}
			if d.ReadOnly() != tt.opts.ReadOnly || d.Mode() != tt.opts.Mode {
				t.Fatalf("readonly=%v mode=%q", d.ReadOnly(), d.Mode())
			}
		})
	}
}

func TestVirtualCount(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if e.VirtualCount() != 0 {
		t.Fatal("a new editor has virtual documents")
	}
	a := e.NewVirtual(VirtualOptions{Name: "a"})
	e.NewVirtual(VirtualOptions{Name: "b"})
	if n := e.VirtualCount(); n != 2 {
		t.Fatalf("VirtualCount = %d, want 2", n)
	}
	if err := e.CloseID(a, false); err != nil {
		t.Fatal(err)
	}
	if n := e.VirtualCount(); n != 1 {
		t.Fatalf("VirtualCount after a close = %d, want 1", n)
	}
}

// A virtual document is never modified, whatever is done to its text: closing it asks
// nothing, a quit does not list it, and no save, auto-save, recovery copy or disk check
// takes it (FINAL X10).
func TestVirtualDocumentIsExcludedEverywhere(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*Editor, *Document, DocID) {
		e, id := virtualEditor(t, VirtualOptions{Name: "v", Text: "one"}, Options{AutoSaveInterval: time.Second})
		// the user types into it: an editable virtual buffer
		if err := e.ApplyEdits(id, []LSPEdit{lspEdit(0, 3, 0, 3, " two")}, EditOptions{}); err != nil {
			t.Fatal(err)
		}
		d, _, _ := e.DocByID(id)
		if !d.Buffer.Modified() {
			t.Fatal("test setup: the edit did not reach the buffer")
		}
		return e, d, id
	}
	tests := []struct {
		Name  string
		check func(t *testing.T, e *Editor, d *Document, id DocID)
	}{
		{"Modified and the tab", func(t *testing.T, e *Editor, d *Document, _ DocID) {
			if d.Modified() {
				t.Error("Modified() = true")
			}
			for _, tab := range e.Tabs() {
				if tab.Modified {
					t.Errorf("tab %q is modified", tab.Name)
				}
			}
		}},
		{"quit dirty list", func(t *testing.T, e *Editor, _ *Document, _ DocID) {
			if dirty := e.Quit(false); len(dirty) != 0 {
				t.Errorf("Quit lists %v", dirty)
			}
			if !e.Quitting() {
				t.Error("the quit was refused")
			}
		}},
		{"close asks nothing", func(t *testing.T, e *Editor, _ *Document, id DocID) {
			if err := e.CloseID(id, false); err != nil {
				t.Errorf("CloseID = %v", err)
			}
			if _, _, ok := e.DocByID(id); ok {
				t.Error("the tab is still open")
			}
		}},
		{"recovery copy", func(t *testing.T, e *Editor, _ *Document, _ DocID) {
			if docs := e.UnsavedDocs(); len(docs) != 0 {
				t.Errorf("UnsavedDocs = %d documents", len(docs))
			}
		}},
		{"auto-save", func(t *testing.T, e *Editor, _ *Document, _ DocID) {
			if jobs := e.PlanAutoSave(time.Now()); len(jobs) != 0 {
				t.Errorf("PlanAutoSave planned %d jobs", len(jobs))
			}
		}},
		{"save", func(t *testing.T, e *Editor, d *Document, id DocID) {
			if err := e.ActivateID(id); err != nil {
				t.Fatal(err)
			}
			if _, err := e.PlanSave(); !errors.Is(err, ErrVirtual) {
				t.Errorf("PlanSave = %v, want ErrVirtual", err)
			}
			if err := e.Save(); !errors.Is(err, ErrVirtual) {
				t.Errorf("Save = %v, want ErrVirtual", err)
			}
		}},
		{"save as", func(t *testing.T, e *Editor, _ *Document, id DocID) {
			if err := e.ActivateID(id); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "out.txt")
			if _, err := e.PlanSaveAs(target, false); !errors.Is(err, ErrVirtual) {
				t.Errorf("PlanSaveAs = %v, want ErrVirtual", err)
			}
			if err := e.SaveAs(target); !errors.Is(err, ErrVirtual) {
				t.Errorf("SaveAs = %v, want ErrVirtual", err)
			}
			if _, err := os.Stat(target); err == nil {
				t.Error("Save As wrote the file")
			}
		}},
		{"save all", func(t *testing.T, e *Editor, _ *Document, _ DocID) {
			p, err := e.PlanSaveAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(p.jobs) != 0 || p.untitled != 0 || p.readOnly != 0 {
				t.Errorf("PlanSaveAll counted the virtual tab: %d jobs, %d untitled, %d read-only", len(p.jobs), p.untitled, p.readOnly)
			}
		}},
		{"disk check", func(t *testing.T, e *Editor, _ *Document, _ DocID) {
			if changes := e.PlanDiskCheck()(); len(changes) != 0 {
				t.Errorf("PlanDiskCheck covers %v", changes)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			e, d, id := setup(t)
			tt.check(t, e, d, id)
		})
	}
}

// A document with a file in it that is open next to a virtual one is still modified and
// listed: the exclusion is the virtual document's, not a blanket.
func TestRegularDocumentsStillCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(Options{AutoSaveInterval: time.Second})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	e.NewVirtual(VirtualOptions{Name: "v"})
	e.Do("insert_char", map[string]any{"text": "y"})
	if docs := e.UnsavedDocs(); len(docs) != 1 || docs[0].Path != path {
		t.Fatalf("UnsavedDocs = %v, want the file", docs)
	}
	if dirty := e.Quit(false); len(dirty) != 1 {
		t.Fatalf("Quit lists %d, want the file", len(dirty))
	}
	if jobs := e.PlanAutoSave(time.Now()); len(jobs) != 1 {
		t.Fatalf("PlanAutoSave planned %d jobs, want 1", len(jobs))
	}
}

// A virtual document never takes the place of the untouched placeholder, and an open does
// not replace an empty virtual document.
func TestOpenDoesNotReplaceEmptyVirtualDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(Options{Empty: true})
	id := e.NewVirtual(VirtualOptions{Name: "v"})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := e.DocByID(id); !ok {
		t.Fatal("opening a file replaced the empty virtual document")
	}
	if n := len(e.Tabs()); n != 2 {
		t.Fatalf("%d tabs, want the virtual one and the file", n)
	}
}

// Large file mode is a property of a file on disk; the text of a virtual document is a
// plain buffer however long it is, up to its cap.
func TestVirtualDocumentIsNeverLarge(t *testing.T) {
	t.Parallel()
	e := New(Options{LargeFileMode: true, LargeFileThresholdBytes: 64})
	id := e.NewVirtual(VirtualOptions{Name: "v", Text: strings.Repeat("0123456789\n", 100)})
	d, _, _ := e.DocByID(id)
	if d.Large() {
		t.Fatal("a virtual document is in large file mode")
	}
	if err := e.SetVirtualText(id, strings.Repeat("0123456789\n", 1000)); err != nil {
		t.Fatal(err)
	}
	if d.Large() || d.Buffer.Lines() != 1001 {
		t.Fatalf("large=%v lines=%d after SetVirtualText", d.Large(), d.Buffer.Lines())
	}
}

func TestSetVirtualText(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	id := e.NewVirtual(VirtualOptions{Name: "v", Text: "one\ntwo\nthree\nfour"})
	d, _, _ := e.DocByID(id)
	if err := e.ActivateID(id); err != nil {
		t.Fatal(err)
	}
	d.Cursor = buffer.Pos{Line: 2, Col: 3}
	d.Top = 1
	d.Sel = &buffer.Selection{Anchor: buffer.Pos{Line: 1}, Head: d.Cursor}
	before := d.Version()
	oldBuffer := d.Buffer

	if err := e.SetVirtualText(id, "A\r\nBB\nCCCC\nD\nE"); err != nil {
		t.Fatal(err)
	}
	if got := d.Buffer.Text(); got != "A\nBB\nCCCC\nD\nE" {
		t.Fatalf("text = %q", got)
	}
	if d.Buffer == oldBuffer || d.Version() == before || d.Version().Gen == before.Gen {
		t.Fatalf("version %v after %v: want a new generation", d.Version(), before)
	}
	if d.Modified() || d.Buffer.Modified() {
		t.Fatal("SetVirtualText marked the document modified")
	}
	if d.Cursor != (buffer.Pos{Line: 2, Col: 3}) {
		t.Fatalf("cursor = %v, want the same line and column", d.Cursor)
	}
	if d.Sel != nil {
		t.Fatalf("selection = %v, want none", d.Sel)
	}
	if d.Top != 1 {
		t.Fatalf("top = %d, want 1", d.Top)
	}
	if ok, _ := e.UndoID(id); ok {
		t.Fatal("undo went back past SetVirtualText")
	}

	// a shorter text pulls the caret and the scroll position in
	if err := e.SetVirtualText(id, "only"); err != nil {
		t.Fatal(err)
	}
	if d.Cursor != (buffer.Pos{Line: 0, Col: 3}) || d.Top != 0 {
		t.Fatalf("cursor = %v top = %d, want (0,3) and 0", d.Cursor, d.Top)
	}
	if err := e.SetVirtualText(id, ""); err != nil || d.Buffer.Text() != "" {
		t.Fatalf("empty text: %v %q", err, d.Buffer.Text())
	}
}

func TestSetVirtualTextRefusals(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	regular := e.ActiveID()
	id := e.NewVirtual(VirtualOptions{Name: "v"})
	tests := []struct {
		Name string
		Id   DocID
		Text string
		Want error
	}{
		{"not virtual", regular, "x", ErrNotVirtual},
		{"not open", 999, "x", ErrNotOpen},
		{"over the cap", id, strings.Repeat("x", MaxVirtualText+1), buffer.ErrOverCap},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if err := e.SetVirtualText(tt.Id, tt.Text); !errors.Is(err, tt.Want) {
				t.Fatalf("SetVirtualText = %v, want %v", err, tt.Want)
			}
		})
	}
	if d, _, _ := e.DocByID(regular); d.Buffer.Text() != "" {
		t.Fatal("a refused call changed the regular document")
	}
}

func TestSetVirtualTextTracksChanges(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	id := e.NewVirtual(VirtualOptions{Name: "v", Text: "old"})
	e.TrackChanges("test", true)
	e.TakeChanges(id, "test")
	if err := e.SetVirtualText(id, "new"); err != nil {
		t.Fatal(err)
	}
	cs, overflow := e.TakeChanges(id, "test")
	if !overflow && (len(cs) != 1 || !cs[0].Coarse) {
		t.Fatalf("changes = %v overflow = %v, want one coarse change", cs, overflow)
	}
}

func TestVirtualProps(t *testing.T) {
	t.Parallel()
	// "head\n" + "item one\n" + "item two" + "\n" + "tail"
	const text = "head\nitem one\nitem two\ntail"
	spans := []PropSpan{
		{FromLine: 1, FromCol: 0, ToLine: 2, ToCol: 0, Props: map[string]any{"n": 1}},
		{FromLine: 2, FromCol: 0, ToLine: 3, ToCol: 0, Props: map[string]any{"n": 2}},
		{FromLine: 3, FromCol: 0, ToLine: 3, ToCol: 4, Props: map[string]any{"n": 3}},
	}
	tests := []struct {
		Name      string
		Line, Col int
		Want      any // the "n" prop, or nil
	}{
		{"before every span", 0, 2, nil},
		{"end of the head line", 0, 4, nil},
		{"start of the first span", 1, 0, 1},
		{"inside the first span", 1, 5, 1},
		{"end of its line", 1, 8, 1},
		{"start of the second span", 2, 0, 2},
		{"end of the second span's line", 2, 8, 2},
		{"start of the last span", 3, 0, 3},
		{"inside the last span", 3, 2, 3},
		{"the end of the document belongs to the last span", 3, 4, 3},
		{"past the end of the document", 9, 9, 3},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			e := New(Options{})
			id := e.NewVirtual(VirtualOptions{Name: "v", Text: text})
			if err := e.SetVirtualProps(id, spans); err != nil {
				t.Fatal(err)
			}
			got, err := e.VirtualProps(id, tt.Line, tt.Col)
			if err != nil {
				t.Fatal(err)
			}
			if tt.Want == nil {
				if got != nil {
					t.Fatalf("props = %v, want none", got)
				}
				return
			}
			if got["n"] != tt.Want {
				t.Fatalf("props = %v, want n=%v", got, tt.Want)
			}
		})
	}
}

// Props describe one content state: an edit, a replaced text or an undo that leaves the
// state changes the answer, and an undo that returns to it brings the props back.
func TestVirtualPropsDieWithTheirText(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	id := e.NewVirtual(VirtualOptions{Name: "v", Text: "abc"})
	span := []PropSpan{{FromLine: 0, ToLine: 0, ToCol: 3, Props: map[string]any{"k": "v"}}}
	if err := e.SetVirtualProps(id, span); err != nil {
		t.Fatal(err)
	}
	has := func() bool { p, _ := e.VirtualProps(id, 0, 1); return p != nil }
	if !has() {
		t.Fatal("props missing right after SetVirtualProps")
	}
	if err := e.ApplyEdits(id, []LSPEdit{lspEdit(0, 3, 0, 3, "d")}, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Fatal("props survived an edit")
	}
	if ok, err := e.UndoID(id); err != nil || !ok {
		t.Fatalf("undo = %v, %v", ok, err)
	}
	if !has() {
		t.Fatal("props did not come back when the undo restored their text")
	}
	if err := e.SetVirtualText(id, "abc"); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Fatal("props survived SetVirtualText with the same text")
	}
	if _, err := e.VirtualProps(e.ActiveID(), 0, 0); err != nil {
		// the active document is the regular placeholder, not the virtual one
		if !errors.Is(err, ErrNotVirtual) {
			t.Fatalf("VirtualProps on a regular document = %v", err)
		}
	}
}

func TestVirtualFlagsAndOptions(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	regular := e.ActiveID()
	id := e.NewVirtual(VirtualOptions{Name: "v"})
	d, _, _ := e.DocByID(id)

	if err := e.SetReadOnlyID(id, true); err != nil || !d.ReadOnly() {
		t.Fatalf("SetReadOnlyID = %v, read-only %v", err, d.ReadOnly())
	}
	if err := e.SetReadOnlyID(regular, true); !errors.Is(err, ErrNotVirtual) {
		t.Fatalf("SetReadOnlyID on a regular document = %v, want ErrNotVirtual", err)
	}
	// a read-only virtual document refuses plugin edits like any read-only one
	if err := e.ApplyEdits(id, []LSPEdit{lspEdit(0, 0, 0, 0, "x")}, EditOptions{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("ApplyEdits on a read-only virtual document = %v", err)
	}

	// the mode belongs to any document
	rd, _, _ := e.DocByID(regular)
	if err := e.SetModeID(regular, "vi"); err != nil || rd.Mode() != "vi" {
		t.Fatalf("SetModeID = %v mode %q", err, rd.Mode())
	}
	if err := e.SetModeID(regular, ""); err != nil || rd.Mode() != "" {
		t.Fatalf("clearing the mode: %v %q", err, rd.Mode())
	}
	if err := e.SetModeID(999, "x"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("SetModeID(unknown) = %v", err)
	}

	wrap := false
	if err := e.UpdateViewSettings(id, func(s *ViewSettings) { s.LineWrap = &wrap; s.TabSize = 8 }); err != nil {
		t.Fatal(err)
	}
	if s := d.ViewSettings(); s.LineWrap == nil || *s.LineWrap || s.TabSize != 8 {
		t.Fatalf("view settings = %+v", s)
	}
	if got := e.SettingsFor(d); got.LineWrap || got.TabSize != 8 {
		t.Fatalf("SettingsFor = wrap %v tab %d", got.LineWrap, got.TabSize)
	}
	if err := e.UpdateViewSettings(999, func(*ViewSettings) {}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("UpdateViewSettings(unknown) = %v", err)
	}
}

func TestVirtualUsage(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	a1 := e.NewVirtual(VirtualOptions{Name: "a1", Owner: "a", Text: "12345"})
	e.NewVirtual(VirtualOptions{Name: "a2", Owner: "a", Text: "123"})
	b1 := e.NewVirtual(VirtualOptions{Name: "b1", Owner: "b", Text: "12"})
	check := func(what, owner string, wantN, wantBytes int) {
		t.Helper()
		var n, b int
		if owner == "*" {
			n, b = e.VirtualTotals()
		} else {
			n, b = e.VirtualUsage(owner)
		}
		if n != wantN || b != wantBytes {
			t.Errorf("%s: usage(%q) = %d, %d, want %d, %d", what, owner, n, b, wantN, wantBytes)
		}
	}
	check("made", "a", 2, 8)
	check("made", "b", 1, 2)
	check("made", "nobody", 0, 0)
	check("made", "*", 3, 10)
	if err := e.SetVirtualText(a1, "1"); err != nil {
		t.Fatal(err)
	}
	check("replaced", "a", 2, 4)
	if owner, size, err := e.VirtualOwner(a1); err != nil || owner != "a" || size != 1 {
		t.Errorf("VirtualOwner = %q, %d, %v", owner, size, err)
	}
	if err := e.CloseID(b1, false); err != nil {
		t.Fatal(err)
	}
	check("closed", "b", 0, 0)
	check("closed", "*", 2, 4)
	if _, _, err := e.VirtualOwner(e.ActiveID()); err != nil && !errors.Is(err, ErrNotVirtual) {
		t.Errorf("VirtualOwner of a regular document = %v", err)
	}
}

// Replacing the text gives the Buffer versions no cache has seen, also when the old
// Buffer was still at version 0 (S18-3).
func TestSetVirtualTextChangesTheBufferVersion(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	id := e.NewVirtual(VirtualOptions{Name: "v", Text: "old"})
	d, _, _ := e.DocByID(id)
	seen := map[uint64]bool{d.Buffer.Version(): true}
	for _, text := range []string{"new", "newer", ""} {
		if err := e.SetVirtualText(id, text); err != nil {
			t.Fatal(err)
		}
		v := d.Buffer.Version()
		if seen[v] {
			t.Fatalf("version %d after SetVirtualText(%q) was already used: %v", v, text, seen)
		}
		seen[v] = true
		if d.Modified() {
			t.Fatalf("SetVirtualText(%q) modified the document", text)
		}
	}
}
