package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// tocDoc has two real headings, a "# comment" in a code block, and a third heading.
const tocDoc = "# Title\n\nintro\n\n## Setup\n\n```sh\n# not a heading\n```\n\n## Usage\n\ntext\n"

// outlineDoc returns the text of the outline's tab, or "" when there is none.
func (s *pluginSession) outlineDoc() (string, bool) {
	for i, tb := range s.rt.ed.Tabs() {
		if tb.Name == "*Outline*" {
			return s.rt.ed.DocAt(i).Buffer.Text(), true
		}
	}
	return "", false
}

// The bundled markdown_toc plugin on the real runtime, editor and ui: Show Outline splits
// the window and lists the headings, Down and Enter jump to one, an edit of the document
// refreshes the list, and q closes the outline and its split.
func TestMarkdownOutlineEndToEnd(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.waitStatus("markdown_toc", "loaded")
	s.openFile("notes.md", tocDoc)
	doc := s.rt.ed.Active()

	s.runCommand("Markdown: Show Outline")
	s.until("the outline", func() bool { _, ok := s.outlineDoc(); return ok && s.screenHas("Usage") })
	if got, _ := s.outlineDoc(); got != "Title\n  Setup\n  Usage" {
		t.Fatalf("outline = %q", got)
	}
	if got := s.rt.ed.Active().Name(); got != "*Outline*" {
		t.Fatalf("the outline does not have the focus: %q", got)
	}

	// Down to Setup, Enter jumps to its line and gives the keyboard back to the document
	s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the jump", func() bool { return s.rt.ed.Active() == doc })
	if doc.Cursor.Line != 4 {
		t.Fatalf("cursor line = %d, want 4 (## Setup)", doc.Cursor.Line)
	}
	if _, ok := s.outlineDoc(); !ok {
		t.Fatal("the outline closed on a jump")
	}

	// breaking the Setup heading and adding another refreshes the outline after the debounce
	s.typeText("x")
	s.press(tea.KeyPressMsg{Code: tea.KeyEnd})
	s.pump(450 * time.Millisecond)
	if got, _ := s.outlineDoc(); got != "Title\n  Usage" {
		t.Fatalf("outline after the edit = %q", got)
	}

	// the command focuses the open outline; q closes it
	s.runCommand("Markdown: Show Outline")
	s.until("the focus on the outline", func() bool { return s.rt.ed.Active().Name() == "*Outline*" })
	s.typeText("q")
	s.until("the outline to close", func() bool { _, ok := s.outlineDoc(); return !ok })
	if s.rt.ed.Active() != doc || strings.Contains(strings.Join(s.rows(), "\n"), "*Outline*") {
		t.Fatalf("after q: active %q", s.rt.ed.Active().Name())
	}
}

// A plugin command refuses a buffer that is not Markdown, with a notice.
func TestMarkdownOutlineNeedsMarkdown(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.waitStatus("markdown_toc", "loaded")
	s.openFile("a.txt", "# not markdown\n")
	s.runCommand("Markdown: Show Outline")
	s.until("the notice", func() bool { return strings.Contains(s.statusRow(), "Not a Markdown buffer") })
	if _, ok := s.outlineDoc(); ok {
		t.Fatal("an outline was made for a text file")
	}
}
