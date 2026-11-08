package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The native Contents sidebar section, end to end: a real runtime, editor and
// UI. Fresh ships the same section as the bundled plugin markdown_toc.ts; gvim
// builds it into the TUI, with the plugin's config keys (plugins.markdown_toc)
// and its palette entry points, so these tests drive the real config and the
// real palette.

// tocDoc has two real headings, a "# comment" in a fenced block, and a third heading.
const tocDoc = "# Title\n\nintro\n\n## Setup\n\n```sh\n# not a heading\n```\n\n## Usage\n\ntext\n"

// sidebarWidth is the explorer column's default width in a 120-column frame.
const sidebarWidth = 32

// sidebarRows is the painted left column: the explorer and Contents sections
// cut at the sidebar's width. The tests here look for Contents rows, and the
// editor paints the same words as its text, so they read the column only.
func (s *pluginSession) sidebarRows() []string {
	out := make([]string, len(s.rows()))
	for i, row := range s.rows() {
		out[i] = strings.TrimRight(ansi.Cut(row, 0, sidebarWidth), " ")
	}
	return out
}

// sidebarHas reports whether a row of the left column contains sub.
func (s *pluginSession) sidebarHas(sub string) bool {
	for _, row := range s.sidebarRows() {
		if strings.Contains(row, sub) {
			return true
		}
	}
	return false
}

// wake gives the model a message, which is what runs the Contents sync after a
// file was opened without going through a UI message (the harness opens files
// on the editor directly).
func (s *pluginSession) wake() {
	s.t.Helper()
	s.send(tea.WindowSizeMsg{Width: 120, Height: 30})
}

// TestMarkdownContentsSectionEndToEnd drives the native Contents section the
// way the user does: the palette focuses it, the arrows browse it, Enter jumps
// to a heading, an edit refreshes the rows, and the palette closes it.
func TestMarkdownContentsSectionEndToEnd(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.openFile("notes.md", tocDoc)
	s.wake()
	s.until("the Contents section", func() bool {
		return s.sidebarHas("Contents") && s.sidebarHas("Setup") && s.sidebarHas("Usage")
	})
	if s.sidebarHas("not a heading") {
		t.Fatalf("the outline lists a fenced-code comment\n%s", strings.Join(s.sidebarRows(), "\n"))
	}

	// The palette focuses the section, and the arrows browse it without moving
	// the caret: Down goes to Setup, Enter jumps there and gives the keyboard
	// back to the editor.
	doc := s.rt.ed.Active()
	s.runCommand("Markdown: Show Outline")
	s.until("the focused section", func() bool { return s.sidebarHas("▌") })
	s.press(tea.KeyPressMsg{Code: tea.KeyDown})
	s.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.until("the jump to Setup", func() bool { return doc.Cursor.Line == 4 })
	if s.rt.ed.Active() != doc {
		t.Fatalf("the jump left the document: %q", s.rt.ed.Active().Name())
	}

	// Breaking the heading with an edit refreshes the outline after the debounce.
	s.typeText("x")
	s.pump(450 * time.Millisecond)
	s.until("the refreshed rows", func() bool {
		return !s.sidebarHas("Setup") && s.sidebarHas("Usage")
	})

	// The command closes the section again.
	s.runCommand("Markdown: Close Outline")
	s.until("the closed section", func() bool { return !s.sidebarHas("Contents") })
}

// TestMarkdownContentsNeedsMarkdown checks that a buffer that is not Markdown
// has no section and that the command says why.
func TestMarkdownContentsNeedsMarkdown(t *testing.T) {
	isolateConfig(t)
	s := newSession(t, Options{})
	s.openFile("a.txt", "# not markdown\n")
	s.wake()
	if s.sidebarHas("Contents") {
		t.Fatalf("a text buffer has a Contents section\n%s", strings.Join(s.sidebarRows(), "\n"))
	}
	s.runCommand("Markdown: Show Outline")
	s.until("the notice", func() bool { return strings.Contains(s.noticeText(), "Not a Markdown buffer") })
}

// TestMarkdownContentsSettings checks the plugins.markdown_toc.settings wiring
// on the real config: auto_open off keeps the section hidden until the command
// shows it, and max_depth limits the rows the section lists.
func TestMarkdownContentsSettings(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{
  "plugins": {"markdown_toc": {"settings": {"auto_open": false, "max_depth": 1}}}
}`)
	s := newSession(t, Options{})
	s.openFile("notes.md", tocDoc)
	s.wake()
	if s.sidebarHas("Contents") {
		t.Fatalf("auto_open off still opened the section\n%s", strings.Join(s.sidebarRows(), "\n"))
	}
	s.runCommand("Markdown: Show Outline")
	s.until("the section from the palette", func() bool {
		return s.sidebarHas("Contents") && s.sidebarHas("Title")
	})
	if s.sidebarHas("Setup") || s.sidebarHas("Usage") {
		t.Fatalf("max_depth 1 listed a deeper heading\n%s", strings.Join(s.sidebarRows(), "\n"))
	}
}

// TestMarkdownContentsDisabled checks plugins.markdown_toc.enabled: false, the
// key the bundled plugin's manifest used.
func TestMarkdownContentsDisabled(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "config.json", `{"plugins": {"markdown_toc": {"enabled": false}}}`)
	s := newSession(t, Options{})
	s.openFile("notes.md", tocDoc)
	s.wake()
	if s.sidebarHas("Contents") {
		t.Fatalf("a disabled section opened\n%s", strings.Join(s.sidebarRows(), "\n"))
	}
	s.runCommand("Markdown: Show Outline")
	s.until("the disabled notice", func() bool { return strings.Contains(s.noticeText(), "disabled") })
}
