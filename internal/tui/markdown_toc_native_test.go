package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/pluginpanel"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/pluginapi"
)

// TestContentsNativeSettingsApplyLive checks the native contents.follow/auto_open/
// rows settings apply to the running section through SettingsChange (the Settings
// page path), not only at startup: auto_open closes/opens the section, follow changes
// the effective mode, rows changes the layout, and a config rows change resets a
// divider drag's session value.
func TestContentsNativeSettingsApplyLive(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "text", "# Two", "more", "# Three")
	m := newTOCModel(t, ed, nil)
	if !m.contents.Open() {
		t.Fatal("the section did not auto-open initially")
	}
	apply := func(follow string, auto bool, rows int, live ...string) {
		m.applySettingsChange(SettingsChange{
			Settings: Settings{ContentsFollow: follow, ContentsAutoOpen: auto, ContentsRows: rows},
			Live:     live,
		})
	}
	apply("scroll", false, 14, "contents.follow", "contents.auto_open", "contents.rows")
	if m.contents.Open() {
		t.Fatal("contents.auto_open=false did not close the auto-opened section")
	}
	if got := m.contents.FollowMode(); got != sidebar.FollowScroll {
		t.Fatalf("contents.follow live = %v, want scroll", got)
	}
	apply("scroll", true, 14, "contents.auto_open")
	if !m.contents.Open() {
		t.Fatal("contents.auto_open=true did not reopen the section for Markdown")
	}
	apply("scroll", true, 20, "contents.rows")
	sec, ok := contentsSection(m.layout())
	if !ok || sec.Rows() != 20 {
		t.Fatalf("contents.rows live = %+v, want 20 rows", sec)
	}
	// A divider drag wins for the session, but an explicit config rows change is the
	// newer instruction and resets it.
	m.contents.SetRows(5)
	apply("scroll", true, 12, "contents.rows")
	sec, _ = contentsSection(m.layout())
	if sec.Rows() != 12 || m.contents.RowsOverridden() {
		t.Fatalf("config rows after a drag = %d overridden %v, want 12/false", sec.Rows(), m.contents.RowsOverridden())
	}
}

// TestContentsFollowCommandSessionOverride checks the follow command is a session
// override that survives a live config change (Fresh's followOverride), while the
// configured value applies when the command was not used.
func TestContentsFollowCommandSessionOverride(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "text", "# Two")
	m := newTOCModel(t, ed, nil)
	apply := func(follow string) {
		m.applySettingsChange(SettingsChange{
			Settings: Settings{ContentsAutoOpen: true, ContentsRows: 10, ContentsFollow: follow},
			Live:     []string{"contents.follow"},
		})
	}
	apply("scroll")
	if got := m.contents.FollowMode(); got != sidebar.FollowScroll {
		t.Fatalf("configured follow = %v, want scroll", got)
	}
	if err := m.Run("markdown_toc_toggle_follow"); err != nil {
		t.Fatal(err)
	}
	if got := m.contents.FollowMode(); got != sidebar.FollowCursor || !m.contents.FollowOverridden() {
		t.Fatalf("follow command = %v overridden %v, want cursor/true", got, m.contents.FollowOverridden())
	}
	apply("scroll") // a live config change must not clear the session override
	if got := m.contents.FollowMode(); got != sidebar.FollowCursor {
		t.Fatalf("live config cleared the follow command override: %v", got)
	}
}

// TestContentsShowOutlineKeepsAnOpenSection checks Show Outline only reveals/focuses
// an already open section: the reader's folds, scroll and selection survive.
func TestContentsShowOutlineKeepsAnOpenSection(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## A", "## B", "# Two")
	m := newTOCModel(t, ed, nil)
	m.contents.SelectPos(0)
	m.contents.ToggleFoldPos(0) // fold One
	m.contents.SetScroll(1)
	rows, selected, scroll := m.contents.Len(), m.contents.SelectedPos(), m.contents.Scroll()
	if err := m.Run("markdown_toc_show"); err != nil {
		t.Fatal(err)
	}
	if got := m.contents.Len(); got != rows {
		t.Fatalf("Show Outline reset the folds: rows %d -> %d", rows, got)
	}
	if got := m.contents.SelectedPos(); got != selected {
		t.Fatalf("Show Outline reset the selection: %d -> %d", selected, got)
	}
	if got := m.contents.Scroll(); got != scroll {
		t.Fatalf("Show Outline reset the scroll: %d -> %d", scroll, got)
	}
	if !m.contentsFocus {
		t.Fatal("Show Outline did not focus the open section")
	}
}

// TestContentsToggleAndFocusCommands checks the two Fresh command rows: Toggle Table
// of Contents closes/opens without taking the keyboard, Focus Contents opens and moves
// the keyboard, and a non-Markdown buffer reports the status.
func TestContentsToggleAndFocusCommands(t *testing.T) {
	t.Parallel()
	cfg := DefaultMarkdownTOC()
	cfg.Settings.AutoOpen = false
	ed := newTOCEditor("notes.md", "# One", "# Two")
	m := newTOCModel(t, ed, &cfg)
	if m.contents.Open() {
		t.Fatal("the section opened with auto_open off")
	}
	if err := m.Run("markdown_toc_toggle"); err != nil {
		t.Fatal(err)
	}
	if !m.contents.Open() || m.contentsFocus {
		t.Fatalf("Toggle Table of Contents = open %v focus %v, want open without focus", m.contents.Open(), m.contentsFocus)
	}
	if err := m.Run("markdown_toc_toggle"); err != nil {
		t.Fatal(err)
	}
	if m.contents.Open() {
		t.Fatal("Toggle Table of Contents did not close the open section")
	}
	if err := m.Run("markdown_toc_focus"); err != nil {
		t.Fatal(err)
	}
	if !m.contents.Open() || !m.contentsFocus {
		t.Fatalf("Focus Contents = open %v focus %v, want both", m.contents.Open(), m.contentsFocus)
	}
	// A text buffer reports Fresh's "Not a Markdown buffer" status.
	text := newTOCModel(t, newTOCEditor("a.txt", "# not markdown"), nil)
	text.contentsFocus = false
	if err := text.Run("markdown_toc_toggle"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.notice, "Not a Markdown buffer") {
		t.Fatalf("toggle on a text buffer notice = %q", text.notice)
	}
}

// TestContentsJumpSuppressionAfterScrollModeJump checks the end-to-end guard: after
// Enter jumps to a heading, the jump's own viewport change keeps the selection while
// the heading is visible; a later scroll follows the pane top again.
func TestContentsJumpSuppressionAfterScrollModeJump(t *testing.T) {
	t.Parallel()
	cfg := DefaultMarkdownTOC()
	cfg.Settings.Follow = sidebar.FollowScroll
	ed := newTOCEditor("notes.md", "# One", "text", "# Two", "more", "# Three")
	m := newTOCModel(t, ed, &cfg)
	if err := m.Run("markdown_toc_show"); err != nil {
		t.Fatal(err)
	}
	// Browse to Three and jump to it.
	m.Update(special(tea.KeyDown))
	m.Update(special(tea.KeyDown))
	if got, _ := m.contents.Selected(); got.Title != "Three" {
		t.Fatalf("selected before the jump = %q, want Three", got.Title)
	}
	m.Update(special(tea.KeyEnter))
	if m.contentsFocus {
		t.Fatal("the keyboard stayed in the sidebar after the jump")
	}
	// The jump's own scroll leaves Two at the top; Three is still visible, so the
	// scroll-mode follow must not move the selection to Two.
	ed.doc.top = 2
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got, _ := m.contents.Selected(); got.Title != "Three" {
		t.Fatalf("selected after the jump's viewport change = %q, want Three", got.Title)
	}
	// A later scroll to the top follows the top heading normally.
	ed.doc.top = 0
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got, _ := m.contents.Selected(); got.Title != "One" {
		t.Fatalf("selected after a later scroll = %q, want One", got.Title)
	}
}

// TestContentsDividerDragWinsOverAFloatingPanel checks the divider has explicit mouse
// precedence: a floating plugin panel under the pointer must not swallow its motion
// or release, which would leave divider.active set and mis-resize on a later move.
func TestContentsDividerDragWinsOverAFloatingPanel(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "text", "# Two")
	m := newTOCModel(t, ed, nil)
	if !m.contents.Open() {
		t.Fatal("the Contents section is not open")
	}
	sec, _ := contentsSection(m.layout())
	m.contents.SetRows(10)
	m.divider = dividerState{active: true, section: sec.Index, kind: sidebar.KindContents, startY: 8, startRows: m.contents.Rows()}
	// A floating plugin panel covers the whole frame and would take a pointer event.
	mount := pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})
	ps := &m.plug.panels
	mp := &mountedPanel{id: 77, owner: "p", mount: pluginapi.WidgetMount{Placement: pluginapi.PlacementFloat}, panel: mount}
	mp.rect = pluginpanel.Rect{X: 0, Y: 0, W: 120, H: 40}
	ps.add(mp)
	rowsBefore := m.contents.Rows()
	if _, cmd := m.Update(tea.MouseMotionMsg{X: 10, Y: 5, Button: tea.MouseLeft}); cmd != nil {
		_ = cmd
	}
	if !m.divider.active {
		t.Fatal("the divider drag was lost to the floating panel's motion")
	}
	if got := m.contents.Rows(); got == rowsBefore {
		t.Fatalf("the divider motion did not resize: rows %d -> %d", rowsBefore, got)
	}
	m.Update(tea.MouseReleaseMsg{X: 10, Y: 5, Button: tea.MouseLeft})
	if m.divider.active {
		t.Fatal("the floating panel swallowed the divider release")
	}
}
