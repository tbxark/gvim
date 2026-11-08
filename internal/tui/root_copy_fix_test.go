package tui

import (
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
)

// clipboardWrites returns the text of every clipboard write that cmd makes. A write is a
// string message, which is what tea.SetClipboard produces.
func clipboardWrites(cmd tea.Cmd) []string {
	var out []string
	for _, msg := range collect(cmd) {
		if v := reflect.ValueOf(msg); v.IsValid() && v.Kind() == reflect.String {
			out = append(out, v.String())
		}
	}
	return out
}

// copyEditor is a document editor whose actions answer with text and err, as the editor's
// copy actions do: text is the path the editor worked out, and err is its refusal.
type copyEditor struct {
	docEditor
	text string
	err  error
}

func (c *copyEditor) Do(a editor.Action, args map[string]any) editor.Result {
	c.docEditor.Do(a, args)
	return editor.Result{Text: c.text, Err: c.err}
}

// TestCopyPathCopiesTheTextTheEditorGives checks that the copy commands run the editor's
// action and copy the text it gives back. The relative form is worked out by the editor from
// its own root, so the UI copies that text as it is.
func TestCopyPathCopiesTheTextTheEditorGives(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string
		text   string
	}{
		{name: "absolute path", action: "copy_file_path", text: "/ws/src/main.go"},
		{name: "relative path from the editor's root", action: "copy_relative_file_path", text: "src/main.go"},
		{name: "the relative text is the editor's, not the UI's", action: "copy_relative_file_path", text: "from-the-editor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := &copyEditor{docEditor: newDocEditor("/ws/src/main.go", false), text: tt.text}
			m := newRevertModel(t, ed)
			cmd := rootPaletteRun(m, tt.action)
			if !slices.Contains(ed.actions, editor.Action(tt.action)) {
				t.Fatalf("the editor ran %q, want %q", ed.actions, tt.action)
			}
			if writes := clipboardWrites(cmd); !reflect.DeepEqual(writes, []string{tt.text}) {
				t.Fatalf("clipboard writes = %q, want [%q]", writes, tt.text)
			}
			if m.notice != "Copied path: "+tt.text {
				t.Fatalf("notice = %q, want the copied path", m.notice)
			}
		})
	}
}

// TestCopyPathShowsTheEditorsRefusal checks that a refusal of the editor is the notice and
// that nothing is copied. A buffer with no file is refused by the editor, and no tab is
// answered by the root, before the editor is asked.
func TestCopyPathShowsTheEditorsRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ed     EditorState
		notice string
	}{
		{
			name:   "a buffer with no file is refused by the editor",
			ed:     &copyEditor{docEditor: newDocEditor("", false), err: editor.ErrNoPath},
			notice: editor.ErrNoPath.Error(),
		},
		{name: "no tab", ed: docEditor{fakeEditor: newFakeEditor("x")}, notice: noDocumentNotice},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newRevertModel(t, tt.ed)
			cmd := rootPaletteRun(m, "copy_file_path")
			if writes := clipboardWrites(cmd); len(writes) != 0 {
				t.Fatalf("clipboard writes = %q, want none", writes)
			}
			if m.notice != tt.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.notice)
			}
		})
	}
}

// TestCopyPathStatusUsesTheActiveLocale checks that the extracted copied-path notice goes
// through the translator. It is not parallel: it changes the process locale and restores it.
func TestCopyPathStatusUsesTheActiveLocale(t *testing.T) {
	i18n.Register("xx", map[string]string{"clipboard.copied_path": "kopierad %{path}"})
	i18n.SetLocale("xx")
	t.Cleanup(func() {
		i18n.SetLocale("en")
		i18n.Register("xx", nil)
	})
	ed := &copyEditor{docEditor: newDocEditor("/ws/src/main.go", false), text: "/ws/a.go"}
	m := newRevertModel(t, ed)
	m.copyPath("copy_file_path")
	if got, want := m.notice, "kopierad /ws/a.go"; got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

// TestCopyPathNoticeUsesTheEmbeddedGermanCatalogue checks the shipped de catalogue at
// the root notice: copying a path renders Fresh's German clipboard.copied_path text.
// Not parallel: it changes and restores the process locale.
func TestCopyPathNoticeUsesTheEmbeddedGermanCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	ed := &copyEditor{docEditor: newDocEditor("/ws/src/main.go", false), text: "/ws/src/main.go"}
	m := newRevertModel(t, ed)
	rootPaletteRun(m, "copy_file_path")
	if want := "Pfad kopiert: /ws/src/main.go"; m.notice != want {
		t.Fatalf("de notice = %q, want %q", m.notice, want)
	}
}
