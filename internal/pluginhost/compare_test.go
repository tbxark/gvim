package pluginhost

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginEditorDiffSource checks the read compare_buffers builds from: the lines,
// the byte starts in the document's one-byte-per-break model, the length and the
// version.
func TestPluginEditorDiffSource(t *testing.T) {
	apptest.IsolateConfig(t)
	pe, _, ids := pluginEditorOver(t, "a\nbb\nc")
	src, err := pe.DiffSource(ids[0])
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	if !slices.Equal(src.Lines, []string{"a", "bb", "c"}) {
		t.Fatalf("lines = %q", src.Lines)
	}
	if !slices.Equal(src.Starts, []int{0, 2, 5}) || src.Len != 6 {
		t.Fatalf("starts = %v len = %d, want [0 2 5] and 6", src.Starts, src.Len)
	}
	if src.Version.IsZero() {
		t.Fatal("DiffSource returned the zero version")
	}
	if _, err := pe.DiffSource(pluginapi.BufferID(9999)); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("missing buffer error = %v, want CodeClosed", err)
	}
}

// TestPluginEditorDiffSourceRefusesALargeFile checks the large-file refusal: the
// line index alone answers no, without reading the text.
func TestPluginEditorDiffSourceRefusesALargeFile(t *testing.T) {
	pe, _, id := largeIndexedPluginEditor(t, 1_500_000)
	if _, err := pe.DiffSource(id); pluginapi.CodeOf(err) != pluginapi.CodeTooLarge {
		t.Fatalf("DiffSource error = %v, want CodeTooLarge", err)
	}
}

// TestPluginEditorDiffSourceRefusesABinary checks the binary refusal.
func TestPluginEditorDiffSourceRefusesABinary(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	apptest.Must(t, os.WriteFile(path, []byte("a\x00b\x00c"), 0o644))
	ed := editor.New(editor.Options{Empty: true})
	apptest.Must(t, ed.OpenFile(path))
	pe := newPluginEditor(ed, func(*editor.Document) {})
	id := pe.ListBuffers()[0].ID
	src, err := pe.DiffSource(id)
	if pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("DiffSource error = %v (src %+v), want CodeInvalid", err, src)
	}
	if ok, err := pe.ReplaceRange(id, 0, 1, "x", nil); ok || err != nil {
		t.Fatalf("ReplaceRange on a binary = %v, %v; want false, nil", ok, err)
	}
}

// TestPluginEditorReplaceRange checks replace_range: one edit and one undo step, the
// version check, the range validation and the EOL normalisation.
func TestPluginEditorReplaceRange(t *testing.T) {
	apptest.IsolateConfig(t)
	pe, ed, ids := pluginEditorOver(t, "a\nb\nc")
	id := ids[0]
	text, version, err := pe.FullText(id)
	if err != nil || text != "a\nb\nc" {
		t.Fatalf("FullText = %q, %v", text, err)
	}
	ok, err := pe.ReplaceRange(id, 2, 3, "X", &version)
	if err != nil || !ok {
		t.Fatalf("ReplaceRange = %v, %v; want true", ok, err)
	}
	got := func() string {
		s, _, err := pe.FullText(id)
		if err != nil {
			t.Fatalf("FullText: %v", err)
		}
		return s
	}
	if got := got(); got != "a\nX\nc" {
		t.Fatalf("text = %q, want the replaced byte", got)
	}
	// One undo step takes the replacement back.
	if err := pe.Undo(id); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := got(); got != "a\nb\nc" {
		t.Fatalf("after undo = %q, want the original", got)
	}
	// A stale expected version is refused, not an error.
	_, staleVersion, _ := pe.FullText(id)
	insertText(t, ed, "Q")
	if ok, err := pe.ReplaceRange(id, 0, 1, "x", &staleVersion); ok || err != nil {
		t.Fatalf("stale replace = %v, %v; want false, nil", ok, err)
	}
	// A bad range is refused too.
	for _, r := range [][2]int{{-1, 1}, {0, 99}, {2, 0}} {
		if ok, err := pe.ReplaceRange(id, r[0], r[1], "x", nil); ok || err != nil {
			t.Fatalf("range %v = %v, %v; want false, nil", r, ok, err)
		}
	}
	// A read-only document is refused.
	ed.Active().SetReadOnly(true)
	if ok, err := pe.ReplaceRange(id, 0, 1, "x", nil); ok || err != nil {
		t.Fatalf("read-only replace = %v, %v; want false, nil", ok, err)
	}
	ed.Active().SetReadOnly(false)
}

// TestPluginEditorReplaceRangeNormalisesEOL checks the inserted text gets the
// document's line breaks: a CRLF string into a LF document is inserted with LF.
func TestPluginEditorReplaceRangeNormalisesEOL(t *testing.T) {
	apptest.IsolateConfig(t)
	pe, _, ids := pluginEditorOver(t, "a\nb")
	id := ids[0]
	text, _, err := pe.FullText(id)
	if err != nil || text != "a\nb" {
		t.Fatalf("FullText = %q, %v", text, err)
	}
	ok, err := pe.ReplaceRange(id, 0, 3, "x\r\ny", nil)
	if err != nil || !ok {
		t.Fatalf("ReplaceRange = %v, %v", ok, err)
	}
	got, _, _ := pe.FullText(id)
	if got != "x\ny" {
		t.Fatalf("text = %q, want the EOL-normalised replacement", got)
	}
}
