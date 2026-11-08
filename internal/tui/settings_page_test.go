package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/theme"
)

// fakeSettingsPort is an in-memory SettingsPort for the page tests.
type fakeSettingsPort struct {
	snap          SettingsSnapshot
	saveChange    SettingsChange
	reload        SettingsChange
	saves         []fakeSettingsSave
	applied       []SettingsChange
	validErr      error
	structures    map[string]SettingsStructure
	structureKeys []string
}

type fakeSettingsSave struct {
	edits []SettingEdit
	layer string
}

func (f *fakeSettingsPort) Snapshot() SettingsSnapshot { return f.snap }

func (f *fakeSettingsPort) Validate(string, string) error { return f.validErr }

func (f *fakeSettingsPort) Save(edits []SettingEdit, layer string) (SettingsChange, error) {
	f.saves = append(f.saves, fakeSettingsSave{edits: edits, layer: layer})
	return f.saveChange, nil
}

func (f *fakeSettingsPort) Reload() (SettingsChange, error) { return f.reload, nil }

// Apply records the changes the root pushed into the editor.
func (f *fakeSettingsPort) Apply(change SettingsChange) { f.applied = append(f.applied, change) }

// Structure returns a fixture structure by key.
func (f *fakeSettingsPort) Structure(key, layer string) (SettingsStructure, error) {
	f.structureKeys = append(f.structureKeys, key+"@"+layer)
	if st, ok := f.structures[key]; ok {
		return st, nil
	}
	return SettingsStructure{Key: key, Title: key}, nil
}

// settingsSnapshot is the page fixture: three sections, an enum with choices, a
// bool, an int and a restart bool, one user-only setting.
func settingsSnapshot() SettingsSnapshot {
	return SettingsSnapshot{
		UserFile:        "/cfg/config.json",
		ProjectFile:     "/ws/.gvim/config.json",
		ProjectWritable: true,
		Sections: []SettingsSection{
			{Name: "Interface", Items: []SettingsItem{
				{Key: "theme", Name: "theme", Section: "Interface", Kind: "enum", Value: "dark",
					Choices: []string{"dark", "light"}, Apply: "live", Layers: []string{"user", "project"}},
				{Key: "keymap", Name: "keymap", Section: "Interface", Kind: "enum", Value: "default",
					Choices: []string{"default", "emacs"}, Apply: "live", Layers: []string{"user"}},
			}},
			{Name: "Display", Items: []SettingsItem{
				{Key: "editor.show_tab_bar", Name: "show_tab_bar", Section: "Display", Kind: "bool",
					Value: "on", Apply: "live", Layers: []string{"user", "project"}},
			}},
			{Name: "Editing", Items: []SettingsItem{
				{Key: "editor.tab_size", Name: "tab_size", Section: "Editing", Kind: "int", Value: "4",
					Min: 1, Max: 16, Apply: "live", Layers: []string{"user", "project"}},
				{Key: "editor.hot_exit", Name: "hot_exit", Section: "Editing", Kind: "bool",
					Value: "on", Apply: "restart", Layers: []string{"user", "project"}},
			}},
		},
	}
}

// openSettingsPage builds a model over port and opens the Settings page.
func openSettingsPage(t *testing.T, port SettingsPort) (*Model, *settingsPage) {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, SettingsPort: port})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(palette.RunCommandMsg{Action: "open_settings"})
	if len(m.overlays) == 0 {
		t.Fatal("open_settings opened no overlay")
	}
	p, ok := m.overlays[len(m.overlays)-1].(*settingsPage)
	if !ok {
		t.Fatalf("the top overlay is %T, want the Settings page", m.overlays[len(m.overlays)-1])
	}
	return m, p
}

// settingsRowIndex returns the index of the row of key in the page's rows.
func settingsRowIndex(t *testing.T, p *settingsPage, key string) int {
	t.Helper()
	for i, row := range p.rows {
		if !row.header && row.item.Key == key {
			return i
		}
	}
	t.Fatalf("setting %q is not on the page", key)
	return 0
}

func TestSettingsPageOpensWithSections(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, p := openSettingsPage(t, port)
	if !p.Active() {
		t.Fatal("the page is not active")
	}
	if p.layer != SettingsLayerUser {
		t.Errorf("layer = %q, want the user layer by default", p.layer)
	}
	var headers []string
	for _, row := range p.rows {
		if row.header {
			headers = append(headers, row.title)
		}
	}
	if len(headers) != 3 || headers[0] != "Interface" {
		t.Errorf("sections = %v, want Interface, Display, Editing", headers)
	}
	// The page draws without panicking and fills its box.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.paint()
	if p.box.W == 0 {
		t.Error("the page did not lay out")
	}
}

func TestSettingsPageTogglesABoolAndSaves(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	updated := settingsSnapshot()
	updated.Sections[1].Items[0].Value = "off"
	port.saveChange = SettingsChange{Snapshot: updated}
	m, p := openSettingsPage(t, port)

	p.sel = settingsRowIndex(t, p, "editor.show_tab_bar")
	press(m, special(tea.KeyEnter))
	if got := p.pending["editor.show_tab_bar"].value; got != "off" {
		t.Fatalf("pending value = %q, want off", got)
	}
	if !p.dirty() {
		t.Error("the page is not dirty after a change")
	}
	if row := p.rows[settingsRowIndex(t, p, "editor.show_tab_bar")]; row.item.Value != "off" {
		t.Errorf("shown value = %q, want the pending off", row.item.Value)
	}

	cmd := press(m, ctrl('s'))
	msgs := collect(cmd)
	msg, ok := firstSettingsChange(msgs)
	if !ok {
		t.Fatal("Ctrl+S produced no settings change message")
	}
	if len(port.saves) != 1 || port.saves[0].layer != SettingsLayerUser {
		t.Fatalf("saves = %+v, want one save to the user layer", port.saves)
	}
	if len(port.saves[0].edits) != 1 || port.saves[0].edits[0].Key != "editor.show_tab_bar" || port.saves[0].edits[0].Value != "off" {
		t.Fatalf("edits = %+v, want the toggled show_tab_bar", port.saves[0].edits)
	}
	m.Update(msg)
	if p.dirty() {
		t.Error("the page is still dirty after a successful save")
	}
	if !strings.Contains(m.notice, "Settings saved to the user config") {
		t.Errorf("notice = %q, want the save notice", m.notice)
	}
}

// firstSettingsChange returns the settings change message among msgs.
func firstSettingsChange(msgs []tea.Msg) (settingsChangeMsg, bool) {
	for _, msg := range msgs {
		if change, ok := msg.(settingsChangeMsg); ok {
			return change, true
		}
	}
	return settingsChangeMsg{}, false
}

func TestSettingsPageRestartNotice(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	port.saveChange = SettingsChange{
		Snapshot: settingsSnapshot(),
		Restart:  []string{"editor.hot_exit"},
	}
	m, p := openSettingsPage(t, port)
	p.sel = settingsRowIndex(t, p, "editor.hot_exit")
	press(m, special(tea.KeyEnter)) // the restart bool toggles pending
	cmd := press(m, ctrl('s'))
	msg, ok := firstSettingsChange(collect(cmd))
	if !ok {
		t.Fatal("Ctrl+S produced no settings change message")
	}
	m.Update(msg)
	if !strings.Contains(m.notice, "Restart gvim to apply: editor.hot_exit") {
		t.Errorf("notice = %q, want the restart notice", m.notice)
	}
}

func TestSettingsPageFilterAndReset(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, p := openSettingsPage(t, port)
	for _, r := range "tab" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	// "tab" matches show_tab_bar and tab_size, with their two section headers.
	if len(p.rows) != 4 {
		t.Fatalf("rows after filtering = %d, want two headers and the two matching rows", len(p.rows))
	}
	p.sel = settingsRowIndex(t, p, "editor.tab_size")
	cmd := press(m, special(tea.KeyEnter)) // asks for the value editor
	m.Update(firstOf[settingsEditMsg](t, collect(cmd)))
	for _, r := range "8" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	cmd = press(m, special(tea.KeyEnter)) // submits the prompt
	done := firstOf[prompt.Done](t, collect(cmd))
	m.Update(done)
	if got := p.pending["editor.tab_size"].value; got != "8" {
		t.Fatalf("pending tab_size = %q, want 8", got)
	}
	// Ctrl+R drops the pending value.
	press(m, ctrl('r'))
	if p.dirty() {
		t.Error("Ctrl+R did not reset the pending values")
	}
	// Clearing the filter shows every row again.
	for range len(p.filter) {
		press(m, special(tea.KeyBackspace))
	}
	if got := len(p.rows); got != 8 { // 3 headers + 5 items
		t.Fatalf("rows after clearing the filter = %d, want 8", got)
	}
	if !strings.Contains(m.notice, "discarded") {
		t.Errorf("notice = %q, want the discard notice", m.notice)
	}
}

func TestSettingsPageLayerToggle(t *testing.T) {
	t.Parallel()
	// A trusted workspace offers the project layer; a user-only setting refuses it.
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, p := openSettingsPage(t, port)
	press(m, special(tea.KeyTab))
	if p.layer != SettingsLayerProject {
		t.Fatalf("layer = %q, want the project layer after Tab", p.layer)
	}
	p.sel = settingsRowIndex(t, p, "keymap")
	press(m, special(tea.KeyEnter))
	if p.dirty() {
		t.Error("the page became dirty for a setting the project layer cannot hold")
	}
	if !strings.Contains(m.notice, "can only be set in the user config") {
		t.Errorf("notice = %q, want the user-only notice", m.notice)
	}
	// The project layer is not offered by an untrusted workspace.
	untrusted := settingsSnapshot()
	untrusted.ProjectWritable = false
	m2, p2 := openSettingsPage(t, &fakeSettingsPort{snap: untrusted})
	press(m2, special(tea.KeyTab))
	if p2.layer != SettingsLayerUser {
		t.Errorf("layer = %q, want the user layer for an untrusted workspace", p2.layer)
	}
	if !strings.Contains(m2.notice, "trusted workspace") {
		t.Errorf("notice = %q, want the trusted-workspace notice", m2.notice)
	}
}

func TestSettingsPagePromptValidation(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, p := openSettingsPage(t, port)
	p.sel = settingsRowIndex(t, p, "editor.tab_size")
	cmd := press(m, special(tea.KeyEnter))
	m.Update(firstOf[settingsEditMsg](t, collect(cmd)))
	if len(m.overlays) < 2 {
		t.Fatal("the int row did not open a prompt")
	}
	// A rejected value keeps the prompt open and changes nothing.
	port.validErr = errors.New("must be between 1 and 16")
	for _, r := range "0" {
		press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(m, special(tea.KeyEnter))
	if p.dirty() {
		t.Error("a rejected value became pending")
	}
	// A valid value becomes pending when the prompt submits.
	port.validErr = nil
	press(m, special(tea.KeyEnter))
	done := prompt.Done{Tag: settingsEditTag{page: p, key: "editor.tab_size"}, Outcome: prompt.Submitted, Text: "8"}
	m.Update(done)
	if got := p.pending["editor.tab_size"].value; got != "8" {
		t.Fatalf("pending = %q, want 8", got)
	}
}

func TestSettingsPageEscAsksBeforeDiscarding(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, p := openSettingsPage(t, port)
	p.sel = settingsRowIndex(t, p, "editor.show_tab_bar")
	press(m, special(tea.KeyEnter))
	cmd := press(m, special(tea.KeyEscape))
	m.Update(firstOf[settingsCancelMsg](t, collect(cmd)))
	if p.Active() == false {
		t.Fatal("Esc closed the page without asking")
	}
	// The discard answer arrives as the confirm's Done message.
	p.Update(prompt.Done{Tag: settingsDiscardTag{page: p}, Outcome: prompt.Yes})
	if p.Active() {
		t.Error("Yes on the discard question did not close the page")
	}
	// No keeps it open.
	port2 := &fakeSettingsPort{snap: settingsSnapshot()}
	m2, p2 := openSettingsPage(t, port2)
	p2.pending["theme"] = settingsPending{key: "theme", value: "light"}
	cmd2 := press(m2, special(tea.KeyEscape))
	m2.Update(firstOf[settingsCancelMsg](t, collect(cmd2)))
	p2.Update(prompt.Done{Tag: settingsDiscardTag{page: p2}, Outcome: prompt.No})
	if !p2.Active() {
		t.Error("No on the discard question closed the page")
	}
}

func TestSettingsApplyLiveValues(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := DefaultViewOptions()
	view.ShowTabBar = false
	view.RelativeLineNumbers = true
	settings := Settings{Theme: "light", LineNumbers: false, MenuBar: false, ExplorerShowHidden: true}
	m.applySettingsChange(SettingsChange{
		View:     view,
		Settings: settings,
		Theme:    "light",
		Live:     []string{"editor.show_tab_bar", "editor.relative_line_numbers", "editor.line_numbers", "editor.show_menu_bar", "theme"},
	})
	if m.view.tabBar {
		t.Error("show_tab_bar was not applied live")
	}
	if !m.view.opts.RelativeLineNumbers {
		t.Error("relative_line_numbers was not applied live")
	}
	if !m.hideLineNumbers {
		t.Error("line_numbers = off did not hide the gutter live")
	}
	if !m.hideMenuBar {
		t.Error("show_menu_bar = off did not hide the bar live")
	}
	if m.util.settings.Theme != "light" {
		t.Errorf("theme = %q, want the applied light", m.util.settings.Theme)
	}
	if !strings.Contains(m.notice, "Settings reloaded") {
		t.Errorf("notice = %q, want the reload notice", m.notice)
	}
}

func TestSettingsPageEditRawWithoutAFile(t *testing.T) {
	t.Parallel()
	snap := settingsSnapshot()
	snap.UserFile = ""
	port := &fakeSettingsPort{snap: snap}
	m, p := openSettingsPage(t, port)
	press(m, ctrl('e'))
	if p.Active() {
		t.Error("Ctrl+E did not close the page")
	}
	if !strings.Contains(m.notice, "no user config file") {
		t.Errorf("notice = %q, want the missing-file notice", m.notice)
	}
}

// TestSettingsApplyStructuredLiveValues checks the live apply of the new scalar
// sections: the status-bar lists and the file-explorer options.
func TestSettingsApplyStructuredLiveValues(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := DefaultViewOptions()
	view.StatusBar = StatusBarOptions{Left: []string{"{filename}"}, Right: []string{"{trust}"}, Separator: "|"}
	view.Explorer = explorer.Options{
		Width:                     explorer.Width{Mode: explorer.WidthAutomatic, Value: 30},
		Side:                      explorer.SideRight,
		FollowActiveBuffer:        true,
		CompactDirectories:        false,
		AutoOpenOnLastBufferClose: false,
	}
	m.applySettingsChange(SettingsChange{
		View:     view,
		Settings: Settings{},
		Live: []string{
			"editor.status_bar.left", "editor.status_bar.right",
			"file_explorer.width", "file_explorer.side",
			"file_explorer.follow_active_buffer", "file_explorer.auto_open_on_last_buffer_close",
			"file_explorer.compact_directories",
		},
	})
	if !slices.Equal(m.view.opts.StatusBar.Left, []string{"{filename}"}) || m.view.opts.StatusBar.Separator != "|" {
		t.Errorf("status bar = %+v, want the applied lists", m.view.opts.StatusBar)
	}
	if m.explorerSide != explorer.SideRight {
		t.Errorf("explorer side = %v, want right", m.explorerSide)
	}
	if !m.view.opts.Explorer.FollowActiveBuffer {
		t.Error("follow_active_buffer was not applied")
	}
	if m.view.opts.Explorer.CompactDirectories || m.view.opts.Explorer.AutoOpenOnLastBufferClose {
		t.Errorf("explorer options = %+v, want the applied off values", m.view.opts.Explorer)
	}
}

// TestReloadKeepsUnsavedChanges checks that a Reload reaching the open page does
// not drop its pending changes.
func TestReloadKeepsUnsavedChanges(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, p := openSettingsPage(t, port)
	p.sel = settingsRowIndex(t, p, "editor.show_tab_bar")
	press(m, special(tea.KeyEnter))
	if !p.dirty() {
		t.Fatal("the page is not dirty")
	}
	updated := settingsSnapshot()
	updated.Sections[1].Items[0].Value = "on"
	m.Update(settingsChangeMsg{change: SettingsChange{Snapshot: updated, Reload: true}})
	if !p.dirty() {
		t.Error("the Reload dropped the unsaved changes")
	}
	if !strings.Contains(m.notice, "Config reloaded") {
		t.Errorf("notice = %q, want the reload notice", m.notice)
	}
}
