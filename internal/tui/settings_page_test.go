package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

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
	acts          []fakeSettingsAct
	actMessage    string
	actErr        error
}

type fakeSettingsAct struct{ action, value string }

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

// Act records the structure action the page ran.
func (f *fakeSettingsPort) Act(action, value string) (string, error) {
	f.acts = append(f.acts, fakeSettingsAct{action: action, value: value})
	return f.actMessage, f.actErr
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

// TestSettingsHelpKey: Fresh's settings context binds ? to settings_help. gvim now opens a
// key-help panel; any key closes the panel, and Esc then closes the page itself.
func TestSettingsHelpKey(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, _ := openSettingsPage(t, port)
	m.Update(typed("?"))
	page := m.overlays[len(m.overlays)-1].(*settingsPage)
	if !page.help {
		t.Fatal("? did not open the Settings keys help")
	}
	if body := ansi.Strip(m.View().Content); !strings.Contains(body, "Settings keys") {
		t.Fatalf("help panel is not painted:\n%s", body)
	}
	m.Update(special(tea.KeyEscape))
	if page.help {
		t.Fatal("Esc did not close the help panel")
	}
	if len(m.overlays) == 0 {
		t.Fatal("Esc closed the Settings page together with the help panel")
	}
	m.Update(special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("second Esc left %d overlay(s), want the Settings page closed", len(m.overlays))
	}
}

// TestSettingsSearchMode: Fresh's settings_search on / starts a search mode over the
// settings rows. Typing filters, Enter jumps/leaves search, and Esc cancels the search
// without closing the page; a second Esc closes Settings.
func TestSettingsSearchMode(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, _ := openSettingsPage(t, port)
	m.Update(typed("/"))
	page := m.overlays[len(m.overlays)-1].(*settingsPage)
	if !page.searching || len(page.filter) != 0 {
		t.Fatalf("search mode = %v filter %q, want started empty", page.searching, string(page.filter))
	}
	if body := ansi.Strip(m.View().Content); !strings.Contains(body, "Search: ") {
		t.Fatalf("search mode is not painted:\n%s", body)
	}
	for _, c := range "theme" {
		m.Update(typed(string(c)))
	}
	if got := string(page.filter); got != "theme" {
		t.Fatalf("filter = %q, want theme", got)
	}
	var items []settingsRow
	for _, row := range page.rows {
		if !row.header {
			items = append(items, row)
		}
	}
	if len(items) != 1 || items[0].item.Key != "theme" {
		t.Fatalf("filtered items = %+v, want only theme", items)
	}
	m.Update(special(tea.KeyEscape))
	if page.searching || len(page.filter) != 0 {
		t.Fatalf("Esc did not cancel search: searching %v filter %q", page.searching, string(page.filter))
	}
	if len(m.overlays) == 0 {
		t.Fatal("Esc closed Settings together with the search")
	}
	m.Update(special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("second Esc left %d overlay(s), want Settings closed", len(m.overlays))
	}
}

// TestSettingsSearchEnterEndsSearch: Enter leaves search mode on the current filtered
// selection without editing it; the page stays open and the filter remains applied.
func TestSettingsSearchEnterEndsSearch(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, _ := openSettingsPage(t, port)
	m.Update(typed("/"))
	page := m.overlays[len(m.overlays)-1].(*settingsPage)
	for _, c := range "theme" {
		m.Update(typed(string(c)))
	}
	m.Update(special(tea.KeyEnter))
	if page.searching {
		t.Fatal("Enter did not end search mode")
	}
	if got := string(page.filter); got != "theme" {
		t.Fatalf("Enter changed the filter to %q", got)
	}
	if len(page.rows) < 2 || page.rows[page.sel].item.Key != "theme" {
		t.Fatalf("Enter lost the filtered selection: rows %d sel %d", len(page.rows), page.sel)
	}
	if len(m.overlays) == 0 {
		t.Fatal("Enter closed the Settings page")
	}
}

// TestSettingsPageRefreshesWhenSnapshotChanges: an open page schedules a non-blocking
// snapshot command, applies the new snapshot and schedules the next tick. This is how the
// remote environment "warming" row changes to the real value when Warm finishes without
// the page being reopened.
func TestSettingsPageRefreshesWhenSnapshotChanges(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	port.snap.Sections[0].Items[0].Value = "warming"
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, SettingsPort: port})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	p := newSettingsPage(m, port)
	p.refreshSeq = 1
	updated := settingsSnapshot()
	updated.Sections[0].Items[0].Value = "light"
	port.snap = updated
	_, cmd := p.Update(settingsRefreshTickMsg{page: p, seq: 1})
	if cmd == nil {
		t.Fatal("the refresh tick produced no snapshot command")
	}
	msg := cmd()
	if _, ok := msg.(settingsSnapshotMsg); !ok {
		t.Fatalf("snapshot command returned %T", msg)
	}
	_, next := p.Update(msg)
	if next == nil {
		t.Fatal("applying the refreshed snapshot did not schedule the next tick")
	}
	if got := p.snap.Sections[0].Items[0].Value; got != "light" {
		t.Fatalf("page snapshot value = %q, want refreshed light", got)
	}
}

// TestSettingsRefreshCrossesChildOverlay: refresh ticks/snapshots are routed to their
// Settings page even while a child overlay (here a prompt stand-in) is on top; the child
// must not swallow them, and a tick for a closed page must not renew.
func TestSettingsRefreshCrossesChildOverlay(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	port.snap.Sections[0].Items[0].Value = "warming"
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, SettingsPort: port})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.openSettings()
	page := m.topSettingsPage()
	if page == nil {
		t.Fatal("open_settings did not push the Settings page")
	}
	page.refreshSeq = 1
	child := &fakeOverlay{open: true}
	m.pushOverlay(child)
	updated := settingsSnapshot()
	updated.Sections[0].Items[0].Value = "light"
	port.snap = updated
	_, cmd := m.Update(settingsRefreshTickMsg{page: page, seq: 1})
	if cmd == nil {
		t.Fatal("refresh tick was swallowed by the child overlay")
	}
	msg := cmd()
	_, next := m.Update(msg)
	if next == nil {
		t.Fatal("refreshed snapshot did not schedule the next tick")
	}
	if got := page.snap.Sections[0].Items[0].Value; got != "light" {
		t.Fatalf("Settings row = %q, want the refreshed light value", got)
	}
	if len(m.overlays) < 2 || m.overlays[len(m.overlays)-1] != Overlay(child) {
		t.Fatal("the child overlay was popped by the refresh")
	}
	if len(child.keys) != 0 {
		t.Fatalf("the child overlay received the settings refresh: %v", child.keys)
	}
	m.popOverlay() // child
	m.popOverlay() // page
	page.Close()
	if _, old := m.Update(settingsRefreshTickMsg{page: page, seq: page.refreshSeq}); old != nil {
		t.Fatal("a tick for a closed Settings page renewed the refresh")
	}
}

// TestSettingsStaleSnapshotAfterSaveIsDropped: a refresh snapshot that started before a
// Save must not overwrite the applied new values, and the save must restart the refresh
// loop for the next snapshot.
func TestSettingsStaleSnapshotAfterSaveIsDropped(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, page := openSettingsPage(t, port)
	if page.refreshSeq == 0 {
		page.refreshSeq = 1
	}
	_, tickCmd := m.Update(settingsRefreshTickMsg{page: page, seq: page.refreshSeq})
	if tickCmd == nil {
		t.Fatal("no snapshot command for the old tick")
	}
	oldMsg := tickCmd()

	updated := settingsSnapshot()
	updated.Sections[0].Items[0].Value = "light"
	port.saveChange = SettingsChange{Snapshot: updated}
	_, saveCmd := m.Update(settingsChangeMsg{change: port.saveChange})
	if saveCmd == nil {
		t.Fatal("applying a save did not restart the refresh loop")
	}
	if got := page.snap.Sections[0].Items[0].Value; got != "light" {
		t.Fatalf("applied snapshot value = %q, want light", got)
	}
	if _, staleCmd := m.Update(oldMsg); staleCmd != nil {
		t.Fatal("the stale snapshot scheduled another refresh")
	}
	if got := page.snap.Sections[0].Items[0].Value; got != "light" {
		t.Fatalf("stale snapshot overwrote the saved value: %q", got)
	}
}

// TestSettingsClosedPageDropsInFlightSnapshot: closing the Settings page while a
// snapshot read is in flight must drop the completed snapshot message: the page
// stays closed, no refresh tick is scheduled, and no closed-overlay update runs.
func TestSettingsClosedPageDropsInFlightSnapshot(t *testing.T) {
	port := &fakeSettingsPort{snap: settingsSnapshot()}
	m, page := openSettingsPage(t, port)
	if page.refreshSeq == 0 {
		page.refreshSeq = 1
	}
	_, tickCmd := m.Update(settingsRefreshTickMsg{page: page, seq: page.refreshSeq})
	if tickCmd == nil {
		t.Fatal("no snapshot command for tick")
	}
	snapshotMsg := tickCmd()

	updated := settingsSnapshot()
	updated.Sections[0].Items[0].Value = "dark-custom"
	port.snap = updated

	m.popOverlay()
	page.Close()
	if page.Active() {
		t.Fatal("page is still active after Close")
	}

	_, cmd := m.Update(snapshotMsg)
	if cmd != nil {
		t.Fatalf("in-flight snapshot on closed page returned a command: %v", cmd)
	}
	if got := page.snap.Sections[0].Items[0].Value; got == "dark-custom" {
		t.Fatal("closed page applied the in-flight snapshot")
	}
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %v, want empty after pop", m.overlays)
	}
}
