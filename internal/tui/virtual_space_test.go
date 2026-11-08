package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/theme"
)

// virtualSpaceFakeEditor is fakeEditor with the optional virtual-space port: it says
// the feature is on and records the virtual column a press parks the caret at.
type virtualSpaceFakeEditor struct {
	*fakeEditor
	virtual int
}

func (v *virtualSpaceFakeEditor) VirtualSpaceEnabled() bool { return true }
func (v *virtualSpaceFakeEditor) SetVirtualColumns(n int)   { v.virtual = n }

// TestMouseClickPastTheLineEndParksTheVirtualCaret checks the mouse half of
// virtual space: a press in the blank after a line's last cell puts the caret on
// the line end and asks the editor for the virtual column the pane computed.
func TestMouseClickPastTheLineEndParksTheVirtualCaret(t *testing.T) {
	t.Parallel()
	ed := &virtualSpaceFakeEditor{fakeEditor: newFakeEditor("abc")}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.view.lineWrap = false
	fp := m.layout().FocusedPane()
	// Two cells past the end of "abc".
	m.Update(click(fp.TextX+5, fp.Y, tea.MouseLeft))
	if got := ed.doc.cursor; got != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("caret = %+v after the press, want the line end", got)
	}
	if ed.virtual != 2 {
		t.Fatalf("virtual column = %d, want 2 from the press", ed.virtual)
	}
}

// TestVirtualCaretNotParkedWhenTheEditorHasNoPort checks that a press past the
// line end is harmless when the editor does not implement the port (the editor's
// own clamping already put the caret on the line end).
func TestVirtualCaretNotParkedWhenTheEditorHasNoPort(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("abc")
	m := newTestModel(t, ed, nil, 80, 24)
	m.view.lineWrap = false
	fp := m.layout().FocusedPane()
	m.Update(click(fp.TextX+5, fp.Y, tea.MouseLeft))
	if got := ed.doc.cursor; got != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("caret = %+v, want the line end", got)
	}
}

// TestInlineDiagnosticsOptionReachesThePane checks the view option plumbing of
// editor.diagnostics_inline_text.
func TestInlineDiagnosticsOptionReachesThePane(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("abc")
	m := newTestModel(t, ed, nil, 80, 24)
	if m.pane(ed.doc).InlineDiagnostics {
		t.Fatal("inline diagnostics are on by default")
	}
	m.view.opts.DiagnosticsInlineText = true
	if !m.pane(ed.doc).InlineDiagnostics {
		t.Fatal("the pane does not carry editor.diagnostics_inline_text")
	}
}

// TestDiagnosticMessageReachesThePane checks that the message of a diagnostic is
// plumbed from the root's LSP state to the pane span.
func TestDiagnosticMessageReachesThePane(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("abc")
	ed.doc.lines = []string{"abc"}
	m := newTestModel(t, ed, nil, 80, 24)
	doc := m.ed.Active()
	m.lsp.diags = map[string][]Diagnostic{"": {
		{Start: buffer.Pos{}, End: buffer.Pos{Col: 3}, Severity: editorpane.SeverityError, Message: "bad thing"},
	}}
	spansOf := m.lspSpans(doc)
	if spansOf == nil {
		t.Fatal("the diagnostics did not reach the pane")
	}
	spans := spansOf(0)
	if len(spans) != 1 || spans[0].Message != "bad thing" || spans[0].Severity != editorpane.SeverityError {
		t.Fatalf("spans = %+v, want the message and severity of the diagnostic", spans)
	}
}
