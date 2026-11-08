package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// TestTranslateCommands checks the UI-layer palette title resolution: German uses the
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
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	de := translateCommands(entries)
	if got := find(de, "close_tab").Name; got != "Tab schließen" {
		t.Errorf("German close_tab = %q, want Fresh's translation", got)
	}
	if got := find(de, "terminal_tab_new").Name; got != "New Terminal Tab" {
		t.Errorf("German terminal_tab_new = %q, want the English fallback", got)
	}
	if got := find(de, "close_tab").Description; got != "Den aktuellen Tab im aktuellen Split schließen" {
		t.Errorf("German close_tab description = %q, want Fresh's translated description", got)
	}
	if got := find(de, "terminal_tab_new").Description; got != "Start another shell in a new dock tab" {
		t.Errorf("German terminal_tab_new description = %q, want the English fallback", got)
	}
	// The recent terminal-tab move rows are mapped to Fresh's per-plugin tab_actions
	// catalogue: the German title and description come from there. The discovery row is
	// gvim-only (Fresh's import-as-workspace semantics differ), so it keeps English.
	if got := find(de, "terminal_tab_move_left").Name; got != "Tab nach links" {
		t.Errorf("German terminal_tab_move_left = %q, want Fresh's tab_actions translation", got)
	}
	if got := find(de, "terminal_tab_move_left").Description; got != "Aktiven Tab im Split nach links verschieben" {
		t.Errorf("German terminal_tab_move_left description = %q, want Fresh's tab_actions translation", got)
	}
	if got := find(de, "terminal_tab_move_right").Name; got != "Tab nach rechts" {
		t.Errorf("German terminal_tab_move_right = %q, want Fresh's tab_actions translation", got)
	}
	if got := find(de, "discover_agent_sessions").Name; got != "Discover Agent Sessions..." {
		t.Errorf("German discover_agent_sessions = %q, want the English fallback", got)
	}
	if got := find(de, "discover_agent_sessions").Description; got != "Find tmux sessions and agents running outside gvim and open one in a terminal tab" {
		t.Errorf("German discover_agent_sessions description = %q, want the English fallback", got)
	}
	if got := find(de, "init_edit").Name; got != "init: Edit init.lua" {
		t.Errorf("German init_edit = %q, want the gvim English fallback", got)
	}
	base := find(entries, "close_tab")
	if got := find(de, "close_tab"); got.Action != base.Action || got.Keybinding != base.Keybinding || got.Source != base.Source {
		t.Errorf("translation changed routing fields: %+v vs %+v", got, base)
	}
	out := translateCommands([]commands.Entry{{Name: "Custom Row", I18nKey: "gvim.command.does_not_exist", Action: "custom"}})
	if out[0].Name != "Custom Row" {
		t.Errorf("unknown key title = %q, want the English fallback", out[0].Name)
	}
}

// TestPaletteSearchesAndRunsTranslatedTitles checks the end-to-end palette path: in
// German a query in the translated title finds the row and Enter runs the same action
// id; a gvim-only row is found by its English fallback. Not parallel: locale.
func TestPaletteSearchesAndRunsTranslatedTitles(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(2), 120, 30)
	m.openPalette(">Tab schließen")
	if frame := strings.Join(frameText(m), "\n"); !strings.Contains(frame, "Tab schließen") {
		t.Fatalf("the palette does not show the German title:\n%s", frame)
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
	// A recently extracted native row: its German title is searchable and still routes to
	// the same action id; the discovery row is found by its English fallback.
	m.openPalette(">Tab nach links")
	if frame := strings.Join(frameText(m), "\n"); !strings.Contains(frame, "Tab nach links") {
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
