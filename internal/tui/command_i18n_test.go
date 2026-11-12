package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// TestTranslateCommands checks the UI-layer palette title resolution: Chinese uses the
// embedded Fresh translations, gvim-only and renamed rows fall back to English, an
// unknown key falls back to the English name (never the key), and the routing fields
// survive. Not parallel: it changes and restores the process locale.
func TestTranslateCommands(t *testing.T) {
	entries := commands.Commands(nil)
	find := func(list []commands.Entry, action string) commands.Entry {
		t.Helper()
		for _, e := range list {
			if e.Action == action {
				return e
			}
		}
		t.Fatalf("no command %q", action)
		return commands.Entry{}
	}
	if got := find(translateCommands(entries), "close_tab").Name; got != "Close Tab" {
		t.Fatalf("English close_tab = %q, want the generated title", got)
	}
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	zh := translateCommands(entries)
	if got := find(zh, "close_tab").Name; got != "关闭标签页" {
		t.Errorf("Chinese close_tab = %q, want Fresh's translation", got)
	}
	if got := find(zh, "terminal_tab_new").Name; got != "New Terminal Tab" {
		t.Errorf("Chinese terminal_tab_new = %q, want the English fallback", got)
	}
	if got := find(zh, "close_tab").Description; got != "关闭当前分割中的当前标签页" {
		t.Errorf("Chinese close_tab description = %q, want Fresh's translated description", got)
	}
	if got := find(zh, "terminal_tab_new").Description; got != "Start another shell in a new dock tab" {
		t.Errorf("Chinese terminal_tab_new description = %q, want the English fallback", got)
	}
	// The recent terminal-tab move rows are mapped to Fresh's per-plugin tab_actions
	// catalogue: the Chinese title and description come from there. The discovery row is
	// gvim-only (Fresh's import-as-workspace semantics differ), so it keeps English.
	if got := find(zh, "terminal_tab_move_left").Name; got != "向左移动标签页" {
		t.Errorf("Chinese terminal_tab_move_left = %q, want Fresh's tab_actions translation", got)
	}
	if got := find(zh, "terminal_tab_move_left").Description; got != "在当前分屏中将活动标签页向左移动" {
		t.Errorf("Chinese terminal_tab_move_left description = %q, want Fresh's tab_actions translation", got)
	}
	if got := find(zh, "terminal_tab_move_right").Name; got != "向右移动标签页" {
		t.Errorf("Chinese terminal_tab_move_right = %q, want Fresh's tab_actions translation", got)
	}
	if got := find(zh, "discover_agent_sessions").Name; got != "Discover Agent Sessions..." {
		t.Errorf("Chinese discover_agent_sessions = %q, want the English fallback", got)
	}
	if got := find(zh, "discover_agent_sessions").Description; got != "Find tmux sessions and agents running outside gvim and open one in a terminal tab" {
		t.Errorf("Chinese discover_agent_sessions description = %q, want the English fallback", got)
	}
	if got := find(zh, "init_edit").Name; got != "init: Edit init.lua" {
		t.Errorf("Chinese init_edit = %q, want the gvim English fallback", got)
	}
	base := find(entries, "close_tab")
	if got := find(zh, "close_tab"); got.Action != base.Action || got.Keybinding != base.Keybinding || got.Source != base.Source {
		t.Errorf("translation changed routing fields: %+v vs %+v", got, base)
	}
	out := translateCommands([]commands.Entry{{Name: "Custom Row", I18nKey: "gvim.command.does_not_exist", Action: "custom"}})
	if out[0].Name != "Custom Row" {
		t.Errorf("unknown key title = %q, want the English fallback", out[0].Name)
	}
}

// TestPaletteSearchesAndRunsTranslatedTitles checks the end-to-end palette path: in
// Chinese a query in the translated title finds the row and Enter runs the same action
// id; a gvim-only row is found by its English fallback. Not parallel: locale.
func TestPaletteSearchesAndRunsTranslatedTitles(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(2), 120, 30)
	m.openPalette(">关闭标签页")
	if frame := strings.Join(frameText(m), "\n"); !strings.Contains(frame, "关闭标签页") {
		t.Fatalf("the palette does not show the Chinese title:\n%s", frame)
	}
	cmd := press(m, special(tea.KeyEnter))
	run, ok := firstOf[palette.RunCommandMsg](t, collect(cmd)), true
	if !ok || run.Action != "close_tab" {
		t.Fatalf("Enter ran %#v, want the close_tab action", run)
	}
	// The gvim-only terminal-tab row falls back to English and is still searchable.
	m.openPalette(">New Terminal Tab")
	if frame := strings.Join(frameText(m), "\n"); !strings.Contains(frame, "New Terminal Tab") {
		t.Fatalf("the palette does not show the English fallback row:\n%s", frame)
	}
	cmd = press(m, special(tea.KeyEnter))
	run = firstOf[palette.RunCommandMsg](t, collect(cmd))
	if run.Action != "terminal_tab_new" {
		t.Fatalf("Enter ran %#v, want the terminal_tab_new action", run)
	}
	// A recently extracted native row: its Chinese title is searchable and still routes to
	// the same action id; the discovery row is found by its English fallback.
	m.openPalette(">向左移动标签页")
	if frame := strings.Join(frameText(m), "\n"); !strings.Contains(frame, "向左移动标签页") {
		t.Fatalf("the palette does not show the translated move row:\n%s", frame)
	}
	cmd = press(m, special(tea.KeyEnter))
	run = firstOf[palette.RunCommandMsg](t, collect(cmd))
	if run.Action != "terminal_tab_move_left" {
		t.Fatalf("Enter ran %#v, want the terminal_tab_move_left action", run)
	}
	m.openPalette(">Discover Agent Sessions")
	if frame := strings.Join(frameText(m), "\n"); !strings.Contains(frame, "Discover Agent Sessions...") {
		t.Fatalf("the palette does not show the discovery fallback row:\n%s", frame)
	}
	cmd = press(m, special(tea.KeyEnter))
	run = firstOf[palette.RunCommandMsg](t, collect(cmd))
	if run.Action != "discover_agent_sessions" {
		t.Fatalf("Enter ran %#v, want the discover_agent_sessions action", run)
	}
}

// TestExtractedCommandKeysExist checks the CI invariant behind the extraction: every
// command entry that names a title or description catalogue key has that key in the
// English catalogue, so a stale generated table or a missing en.json entry is caught
// before the palette silently falls back to English.
func TestExtractedCommandKeysExist(t *testing.T) {
	t.Parallel()
	for _, e := range commands.Commands(nil) {
		check := func(kind, key string) {
			if key == "" {
				return
			}
			if got := i18n.T("en", key, nil); got == key {
				t.Errorf("%s: %s key %q is not in the English catalogue", e.Action, kind, key)
			}
		}
		check("title", e.I18nKey)
		check("description", e.DescKey)
	}
}
