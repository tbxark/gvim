package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/theme"
)

// TestBrowserDetectEncodingOpensChooser: a browser choice with auto-detection off
// opens the encoding list; choosing a row opens the file and reloads it with that
// encoding, then moves to the requested line/column.
func TestBrowserDetectEncodingOpensChooser(t *testing.T) {
	ed := &fileEditorFake{fakeEditor: newFakeEditor("x")}
	m := New(Deps{Editor: ed, Explorer: newFakeTree(1), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(browser.Open{Path: "/w/a.txt", Line: 7, Col: 2, AskEncoding: true})
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the encoding list", len(m.overlays))
	}
	v, ok := m.overlays[0].(*list.View)
	if !ok {
		t.Fatalf("top overlay = %T, want the encoding list", m.overlays[0])
	}
	if v.Title != "Open with Encoding" {
		t.Fatalf("list title = %q, want Open with Encoding", v.Title)
	}
	feed(m, press(m, special(tea.KeyEnter)))
	if !slices.Contains(ed.opened, "/w/a.txt") {
		t.Fatalf("opened = %v, want /w/a.txt", ed.opened)
	}
	if len(ed.actions) == 0 || ed.actions[len(ed.actions)-1] != "reload_with_encoding" {
		t.Fatalf("actions = %v, want reload_with_encoding", ed.actions)
	}
	if name, _ := ed.args[len(ed.args)-1]["name"].(string); name == "" {
		t.Fatalf("reload args = %v, want a chosen encoding name", ed.args[len(ed.args)-1])
	}
	if ed.line != 7 || ed.col != 2 {
		t.Fatalf("caret = %d:%d, want 7:2", ed.line, ed.col)
	}
}

// TestBrowserEncodingKeepsDirtyOpenFile: choosing an encoding for a file that is
// already open with unsaved edits must not reload or discard the buffer; the reload
// refusal is visible in the editor status and no success is reported.
func TestBrowserEncodingKeepsDirtyOpenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("saved\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	if r := ed.Do("insert_char", map[string]any{"text": "X"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	before := ed.Active().Buffer.Text()
	if !ed.Active().Modified() {
		t.Fatal("setup: buffer is not modified")
	}

	m := New(Deps{Editor: realFileEditor{realEditor{ed}}, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(browser.Open{Path: path, AskEncoding: true})
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the encoding list", len(m.overlays))
	}
	feed(m, press(m, special(tea.KeyEnter)))

	if got := ed.Active().Buffer.Text(); got != before || !ed.Active().Modified() {
		t.Fatalf("dirty buffer changed: text %q modified %v, want %q modified", got, ed.Active().Modified(), before)
	}
	status := ed.Status()
	if !strings.Contains(status, "unsaved changes") {
		t.Fatalf("status = %q, want the visible unsaved-changes refusal", status)
	}
	if strings.Contains(status, "Reloaded as") {
		t.Fatalf("status = %q falsely reports a successful reload", status)
	}
}

// largePathEditor marks one path as large-file mode for the browser encoding
// fallback test.
type largePathEditor struct {
	*fileEditorFake
}

func (e *largePathEditor) IsLargePath(string) bool { return true }

// TestBrowserEncodingLargeFileFallsBack: the chooser's large-file branch uses the
// normal windowed open and gives the explicit notice instead of reloading whole.
func TestBrowserEncodingLargeFileFallsBack(t *testing.T) {
	ed := &largePathEditor{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("x")}}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(browser.Open{Path: "/w/big.txt", AskEncoding: true})
	feed(m, press(m, special(tea.KeyEnter)))

	if !slices.Contains(ed.opened, "/w/big.txt") {
		t.Fatalf("opened = %v, want the large file", ed.opened)
	}
	if !strings.Contains(m.notice, "large file") {
		t.Fatalf("notice = %q, want the large-file fallback", m.notice)
	}
	if slices.Contains(ed.actions, editor.Action("reload_with_encoding")) {
		t.Fatalf("actions = %v, want no reload for a large file", ed.actions)
	}
}
