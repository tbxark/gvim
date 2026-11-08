package tui

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
)

// clockTickMsg asks for the next frame, so a configured {clock} element shows the
// new minute. It carries nothing: the time comes from the model's clock.
type clockTickMsg struct{}

// clockFormat is Fresh's {clock} text: 24-hour local time, zero-padded hours and
// minutes (chrono "{:02}:{:02}" in render.rs).
const clockFormat = "15:04"

// statusFilename is Fresh's display name for doc: its path relative to the
// workspace root while it is under it, its absolute path otherwise, and the
// document's own name ("[No Name]", "Untitled-N", "*virtual*") when it has no path.
// Fresh computes this once, at open, from the working directory (display_name_for_path);
// gvim keeps the tree root as the working directory and computes it per frame.
func (m *Model) statusFilename(doc Document) string {
	if doc == nil {
		return ""
	}
	pe, ok := doc.(pathEditor)
	if !ok || pe.FilePath() == "" {
		return doc.Name()
	}
	path := pe.FilePath()
	root := m.rootDir()
	if root == "" {
		return path
	}
	// Fresh anchors a relative path to the working directory before making it
	// relative to it, so a relative FilePath behaves the same way.
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	// Resolve both spellings before comparing: on macOS a workspace root is
	// canonicalised (/private/var/...) while the open document keeps the spelling
	// it was opened with (/var/...), and a lexical filepath.Rel would call them
	// unrelated. The fallback keeps the document's original spelling, which is what
	// Fresh shows for a file outside the working directory.
	rel, err := filepath.Rel(canonicalFilePath(root), canonicalFilePath(path))
	if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return pe.FilePath()
	}
	return rel
}

// statusChord is the {chord} element's text: the pending keys of a multi-key
// sequence in Fresh's bracket form, in gvim's one label style ("[Ctrl+X]").
// It is "" while no sequence is pending, which hides the element.
func (m *Model) statusChord() string {
	if len(m.chord) == 0 {
		return ""
	}
	return "[" + chordLabel(m.chord) + "]"
}

// clockCmd arms the next {clock} tick when the configured status row names the
// element and the row is shown, and returns nil otherwise. The tick lands on the
// next minute boundary of the model's clock, so no wall-clock time is read while
// building it (a test clock works the same way); each tick asks for one frame.
func (m *Model) clockCmd() tea.Cmd {
	m.clockArmed = false
	if !m.clockConfigured() || m.statusHidden() {
		return nil
	}
	m.clockArmed = true
	now := m.now()
	wait := time.Minute - time.Duration(now.Second())*time.Second - time.Duration(now.Nanosecond())
	if wait <= 0 {
		wait = time.Minute
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return clockTickMsg{} })
}

// clockConfigured reports whether the configured status row names {clock} and the
// row is visible: a palette or file browser hides it, so its clock needs no ticks.
func (m *Model) clockConfigured() bool {
	if m.statusHidden() {
		return false
	}
	row := m.statusRow()
	has := func(els []frame.StatusElement) bool {
		for _, e := range els {
			if e.Kind == frame.StatusElementClock {
				return true
			}
		}
		return false
	}
	return has(row.Left) || has(row.Right)
}
