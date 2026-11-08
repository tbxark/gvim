package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/theme"
)

// structuredSnapshot is the page fixture with the structured page rows.
func structuredSnapshot() SettingsSnapshot {
	return SettingsSnapshot{
		UserFile:        "/cfg/config.json",
		ProjectFile:     "/ws/.gvim/config.json",
		ProjectWritable: true,
		Sections: []SettingsSection{
			{Name: "Display", Items: []SettingsItem{
				{Key: "editor.show_tab_bar", Path: []string{"editor", "show_tab_bar"}, Name: "show_tab_bar",
					Section: "Display", Kind: "bool", Value: "on", Apply: "live", Layers: []string{"user", "project"}},
			}},
			{Name: "Languages", Items: []SettingsItem{
				{Key: "languages", Path: []string{"languages"}, Name: "languages", Section: "Languages",
					Kind: "page", Value: "1 entry", Apply: "live", Layers: []string{"user", "project"}},
			}},
			{Name: "Formatters", Items: []SettingsItem{
				{Key: "formatters", Path: []string{"formatters"}, Name: "formatters", Section: "Formatters",
					Kind: "page", Value: "2 commands", Apply: "live", Layers: []string{"user", "project"}},
			}},
			{Name: "Status Bar", Items: []SettingsItem{
				{Key: "editor.status_bar.left", Path: []string{"editor", "status_bar", "left"}, Name: "left",
					Section: "Status Bar", Kind: "page", Value: "2 elements", Apply: "live", Layers: []string{"user", "project"}},
				{Key: "editor.status_bar.right", Path: []string{"editor", "status_bar", "right"}, Name: "right",
					Section: "Status Bar", Kind: "page", Value: "1 element", Apply: "live", Layers: []string{"user", "project"}},
			}},
			{Name: "Terminal", Items: []SettingsItem{
				{Key: "terminal.shell.args", Path: []string{"terminal", "shell", "args"}, Name: "args",
					Section: "Terminal", Kind: "page", Value: "2 arguments", Apply: "restart", Layers: []string{"user"}},
			}},
		},
	}
}

// structuredStructures is the fixture structures of structuredSnapshot.
func structuredStructures() map[string]SettingsStructure {
	return map[string]SettingsStructure{
		"formatters": {
			Key: "formatters", Title: "Formatters", Kind: "map", AddLabel: "Add formatter…",
			Rows: []SettingsStructureRow{
				{Path: []string{"formatters", "go"}, Name: "go", Value: "gofmt"},
				{Path: []string{"formatters", ".go"}, Name: ".go", Value: "gofmt"},
			},
		},
		"languages": {
			Key: "languages", Title: "Languages", Kind: "languages", AddLabel: "Add language…",
			Rows: []SettingsStructureRow{
				{Path: []string{"languages", "go"}, Name: "go", Value: "2 overrides", Child: "languages.go"},
			},
			Available: []string{"python", "rust"},
		},
		"languages.go": {
			Key: "languages.go", Title: "Languages · go", Kind: "fields",
			Rows: []SettingsStructureRow{
				{Path: []string{"languages", "go", "tab_size"}, Name: "tab_size", Kind: "int", Value: "8"},
				{Path: []string{"languages", "go", "use_tabs"}, Name: "use_tabs", Kind: "bool", Value: "on"},
				{Path: []string{"languages", "go", "auto_indent"}, Name: "auto_indent", Kind: "bool"},
			},
		},
		"languages.python": {
			Key: "languages.python", Title: "Languages · python", Kind: "fields",
			Rows: []SettingsStructureRow{
				{Path: []string{"languages", "python", "tab_size"}, Name: "tab_size", Kind: "int"},
			},
		},
		"editor.status_bar.left": {
			Key: "editor.status_bar.left", Title: "Status Bar", Kind: "dual",
			Rows: []SettingsStructureRow{
				{Path: []string{"editor", "status_bar", "left"}, Group: "left", Name: "{filename}"},
				{Path: []string{"editor", "status_bar", "left"}, Group: "left", Name: "{cursor}"},
				{Path: []string{"editor", "status_bar", "right"}, Group: "right", Name: "{trust}"},
			},
			Available: []string{"{clock}", "{palette}"},
		},
		"editor.status_bar.right": {
			Key: "editor.status_bar.right", Title: "Status Bar", Kind: "dual",
			Rows: []SettingsStructureRow{
				{Path: []string{"editor", "status_bar", "left"}, Group: "left", Name: "{filename}"},
				{Path: []string{"editor", "status_bar", "right"}, Group: "right", Name: "{trust}"},
			},
			Available: []string{"{clock}"},
		},
		"terminal.shell.args": {
			Key: "terminal.shell.args", Title: "Shell Arguments", Kind: "list", AddLabel: "Add argument…",
			Rows: []SettingsStructureRow{
				{Path: []string{"terminal", "shell", "args"}, Name: "-l", Value: "-l"},
				{Path: []string{"terminal", "shell", "args"}, Name: "-i", Value: "-i"},
			},
		},
	}
}

// openStructuredPage opens the main page over a fake port with the structured
// fixtures.
func openStructuredPage(t *testing.T) (*Model, *settingsPage, *fakeSettingsPort) {
	t.Helper()
	port := &fakeSettingsPort{snap: structuredSnapshot(), structures: structuredStructures()}
	port.saveChange = SettingsChange{Snapshot: structuredSnapshot()}
	m, p := openSettingsPage(t, port)
	return m, p, port
}

// requestStructurePage sends the Enter on a page row through the model and
// returns the sub-page the port's structure opened.
func requestStructurePage(t *testing.T, m *Model, p *settingsPage, key string) *settingsStructurePage {
	t.Helper()
	p.sel = settingsRowIndex(t, p, key)
	cmd := press(m, special(tea.KeyEnter))
	m.Update(firstOf[settingsEditMsg](t, collect(cmd)))
	_, cmd = m.Update(settingsEditMsg{page: p, key: key})
	m.Update(firstOf[settingsStructureMsg](t, collect(cmd)))
	top := m.overlays[len(m.overlays)-1]
	sub, ok := top.(*settingsStructurePage)
	if !ok {
		t.Fatalf("the top overlay is %T, want a structure page", top)
	}
	return sub
}

// typeAndSubmit types text into the top prompt and submits it, then routes the
// answer to the structure page.
func typeAndSubmit(t *testing.T, m *Model, text string) prompt.Done {
	t.Helper()
	for _, r := range text {
		press(m, typed(string(r)))
	}
	cmd := press(m, special(tea.KeyEnter))
	return firstOf[prompt.Done](t, collect(cmd))
}

// acceptStructureOverlay pushes the overlay a structure page asked for.
func acceptStructureOverlay(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	m.Update(firstOf[structureOverlayMsg](t, collect(cmd)))
}

func TestStructureMapEditAddRemoveAndSave(t *testing.T) {
	t.Parallel()
	m, p, port := openStructuredPage(t)
	sub := requestStructurePage(t, m, p, "formatters")
	if sub.st.Kind != "map" {
		t.Fatalf("kind = %q, want map", sub.st.Kind)
	}

	// Edit the command of the go entry.
	acceptStructureOverlay(t, m, press(m, special(tea.KeyEnter)))
	done := typeAndSubmit(t, m, "gofmt2")
	m.Update(done)
	if raw, ok := p.pending[pathID([]string{"formatters", "go"})]; !ok || string(raw.raw) != `"gofmt2"` {
		t.Fatalf("pending go = %+v, want the edited command", p.pending)
	}

	// Add an entry: Ctrl+N asks for the key, then the value.
	acceptStructureOverlay(t, m, press(m, ctrl('n')))
	done = typeAndSubmit(t, m, "python")
	_, cmd := m.Update(done) // asks for the value
	acceptStructureOverlay(t, m, cmd)
	done = typeAndSubmit(t, m, "black")
	m.Update(done)
	if raw, ok := p.pending[pathID([]string{"formatters", "python"})]; !ok || string(raw.raw) != `"black"` {
		t.Fatalf("pending python = %+v, want the added command", p.pending)
	}

	// Remove the .go entry: Del records a reset.
	sub.sel = 1
	press(m, special(tea.KeyDelete))
	if _, ok := p.pending[pathID([]string{"formatters", ".go"})]; !ok {
		t.Fatalf("no removal pending for .go: %+v", p.pending)
	}

	// Ctrl+S closes the sub-page and saves the parent page.
	cmd = press(m, ctrl('s'))
	if sub.Active() {
		t.Error("Ctrl+S left the sub-page open")
	}
	msgs := collect(cmd)
	change, ok := firstSettingsChange(msgs)
	if !ok {
		t.Fatal("Ctrl+S produced no settings change message")
	}
	if len(port.saves) != 1 {
		t.Fatalf("saves = %d, want one", len(port.saves))
	}
	saved := port.saves[0].edits
	var edited, removed bool
	for _, edit := range saved {
		if slices.Equal(edit.Path, []string{"formatters", "python"}) && string(edit.Raw) == `"black"` {
			edited = true
		}
		if slices.Equal(edit.Path, []string{"formatters", ".go"}) && edit.Remove {
			removed = true
		}
	}
	if !edited || !removed {
		t.Fatalf("saved edits = %+v, want the added python and the removed .go", saved)
	}
	m.Update(change)
	if p.dirty() {
		t.Error("the page is still dirty after a successful save")
	}
}

func TestStructureDualListMovesAndWritesBothSides(t *testing.T) {
	t.Parallel()
	m, p, _ := openStructuredPage(t)
	sub := requestStructurePage(t, m, p, "editor.status_bar.left")
	if sub.st.Kind != "dual" {
		t.Fatalf("kind = %q, want dual", sub.st.Kind)
	}
	if sub.side != "left" {
		t.Errorf("side = %q, want left for the left row", sub.side)
	}
	// The first left entry moves to the right side.
	sub.sel = 1 // 0 is the "Left" header
	press(m, special(tea.KeyEnter))
	left, ok := pendingArray(t, p, []string{"editor", "status_bar", "left"})
	if !ok || slices.Contains(left, "{filename}") {
		t.Fatalf("left pending = %v (ok %v), want the entry moved off", left, ok)
	}
	right, ok := pendingArray(t, p, []string{"editor", "status_bar", "right"})
	if !ok || !slices.Contains(right, "{filename}") {
		t.Fatalf("right pending = %v (ok %v), want the entry moved there", right, ok)
	}

	// Tab switches the focused side; an Available element joins it.
	press(m, special(tea.KeyTab))
	if sub.side != "right" {
		t.Fatalf("side = %q, want right after Tab", sub.side)
	}
	for i, row := range sub.rows {
		if row.group == "available" && !row.header {
			sub.sel = i
			break
		}
	}
	press(m, special(tea.KeyEnter))
	right, _ = pendingArray(t, p, []string{"editor", "status_bar", "right"})
	if !slices.Contains(right, "{clock}") {
		t.Fatalf("right pending = %v, want the added {clock}", right)
	}

	// The separator is never touched by the dual list writes.
	for _, edit := range pendingEdits(p) {
		if slices.Equal(edit.Path, []string{"editor", "status_bar", "separator"}) {
			t.Fatal("the dual list wrote the separator")
		}
	}
}

// pendingArray decodes a pending JSON array at path.
func pendingArray(t *testing.T, p *settingsPage, path []string) ([]string, bool) {
	t.Helper()
	pending, ok := p.pending[pathID(path)]
	if !ok || pending.raw == nil {
		return nil, false
	}
	var list []string
	if err := json.Unmarshal(pending.raw, &list); err != nil {
		t.Fatalf("pending %v is not a string array: %v", path, err)
	}
	return list, true
}

// pendingEdits renders the parent's pending changes as edits.
func pendingEdits(p *settingsPage) []SettingEdit {
	var out []SettingEdit
	for _, pending := range p.pending {
		out = append(out, SettingEdit{Path: pending.path, Raw: pending.raw, Remove: pending.remove})
	}
	return out
}

func TestStructureLanguagesOpensFieldsAndEditsThem(t *testing.T) {
	t.Parallel()
	m, p, _ := openStructuredPage(t)
	sub := requestStructurePage(t, m, p, "languages")
	if sub.st.Kind != "languages" {
		t.Fatalf("kind = %q, want languages", sub.st.Kind)
	}
	// Enter on the go entry opens its fields page.
	sub.sel = 0
	cmd := press(m, special(tea.KeyEnter))
	m.Update(firstOf[settingsStructureMsg](t, collect(cmd)))
	fields, ok := m.overlays[len(m.overlays)-1].(*settingsStructurePage)
	if !ok {
		t.Fatalf("the top overlay is %T, want the fields page", m.overlays[len(m.overlays)-1])
	}
	if fields.st.Kind != "fields" {
		t.Fatalf("kind = %q, want fields", fields.st.Kind)
	}
	// The use_tabs bool toggles in place.
	fields.sel = 1
	press(m, special(tea.KeyEnter))
	if raw, ok := p.pending[pathID([]string{"languages", "go", "use_tabs"})]; !ok || string(raw.raw) != "false" {
		t.Fatalf("pending use_tabs = %+v, want false", p.pending)
	}
	// Del on tab_size records a reset of the override.
	fields.sel = 0
	press(m, special(tea.KeyDelete))
	if pending, ok := p.pending[pathID([]string{"languages", "go", "tab_size"})]; !ok || !pending.remove {
		t.Fatalf("pending tab_size = %+v, want a removal", p.pending)
	}
}

func TestStructureLanguagesAddOpensTheNewFieldsPage(t *testing.T) {
	t.Parallel()
	m, p, _ := openStructuredPage(t)
	sub := requestStructurePage(t, m, p, "languages")
	// Ctrl+N opens the picker of the known languages; choosing one asks the port
	// for its fields page.
	acceptStructureOverlay(t, m, press(m, ctrl('n')))
	if len(m.overlays) < 3 {
		t.Fatalf("the add picker did not open (overlays %d)", len(m.overlays))
	}
	press(m, typed("p")) // filter the picker
	cmd := press(m, special(tea.KeyEnter))
	msgs := collect(cmd)
	msg, ok := firstStructureMsg(msgs)
	if !ok {
		t.Fatal("choosing a language did not ask for its fields page")
	}
	m.Update(msg)
	top := m.overlays[len(m.overlays)-1]
	fields, ok := top.(*settingsStructurePage)
	if !ok {
		t.Fatalf("the top overlay is %T, want a fields page", top)
	}
	if fields.st.Key != "languages.python" {
		t.Fatalf("child key = %q, want languages.python", fields.st.Key)
	}
	if sub.Active() == false {
		t.Error("the languages page closed")
	}
}

// firstStructureMsg returns the structure message among msgs.
func firstStructureMsg(msgs []tea.Msg) (settingsStructureMsg, bool) {
	for _, msg := range msgs {
		if change, ok := msg.(settingsStructureMsg); ok {
			return change, true
		}
	}
	return settingsStructureMsg{}, false
}

func TestStructureListEditsAndReorders(t *testing.T) {
	t.Parallel()
	m, p, _ := openStructuredPage(t)
	sub := requestStructurePage(t, m, p, "terminal.shell.args")
	if sub.st.Kind != "list" {
		t.Fatalf("kind = %q, want list", sub.st.Kind)
	}
	// Alt+Down swaps the two arguments, and the whole array is pending.
	sub.sel = 0
	press(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	args, ok := pendingArray(t, p, []string{"terminal", "shell", "args"})
	if !ok || len(args) != 2 || args[0] != "-i" || args[1] != "-l" {
		t.Fatalf("pending args = %v (ok %v), want -i, -l", args, ok)
	}
	// Enter edits the highlighted item.
	sub.sel = 0
	acceptStructureOverlay(t, m, press(m, special(tea.KeyEnter)))
	done := typeAndSubmit(t, m, "-z")
	m.Update(done)
	args, _ = pendingArray(t, p, []string{"terminal", "shell", "args"})
	if !slices.Contains(args, "-z") || slices.Contains(args, "-i") {
		t.Fatalf("pending args = %v, want -i edited to -z", args)
	}

	// Ctrl+N adds one.
	acceptStructureOverlay(t, m, press(m, ctrl('n')))
	done = typeAndSubmit(t, m, "-x")
	m.Update(done)
	args, _ = pendingArray(t, p, []string{"terminal", "shell", "args"})
	if !slices.Contains(args, "-x") {
		t.Fatalf("pending args = %v, want the added -x", args)
	}
	// Del removes the highlighted entry.
	sub.sel = 0
	press(m, special(tea.KeyDelete))
	args, _ = pendingArray(t, p, []string{"terminal", "shell", "args"})
	if slices.Contains(args, args[0]) && len(args) == 3 {
		t.Fatalf("pending args = %v, want one entry removed", args)
	}
}

func TestMainPageScalarResetToggles(t *testing.T) {
	t.Parallel()
	m, p, port := openStructuredPage(t)
	p.sel = settingsRowIndex(t, p, "editor.show_tab_bar")
	press(m, special(tea.KeyDelete))
	pending, ok := p.pending["editor.show_tab_bar"]
	if !ok || !pending.remove || !slices.Equal(pending.path, []string{"editor", "show_tab_bar"}) {
		t.Fatalf("pending = %+v, want a removal of editor.show_tab_bar", pending)
	}
	if got := p.rows[settingsRowIndex(t, p, "editor.show_tab_bar")].item.Value; got != "(default)" {
		t.Errorf("shown value = %q, want (default)", got)
	}
	// Del again undoes the reset.
	press(m, special(tea.KeyDelete))
	if _, ok := p.pending["editor.show_tab_bar"]; ok {
		t.Fatalf("the reset was not undone: %+v", p.pending)
	}
	// A reset pending saves as a path removal.
	press(m, special(tea.KeyDelete))
	cmd := press(m, ctrl('s'))
	msgs := collect(cmd)
	if _, ok := firstSettingsChange(msgs); !ok {
		t.Fatal("Ctrl+S produced no settings change message")
	}
	if len(port.saves) != 1 || len(port.saves[0].edits) != 1 {
		t.Fatalf("saves = %+v, want one removal edit", port.saves)
	}
	edit := port.saves[0].edits[0]
	if !edit.Remove || !slices.Equal(edit.Path, []string{"editor", "show_tab_bar"}) {
		t.Fatalf("edit = %+v, want a removal of editor.show_tab_bar", edit)
	}
}

func TestStaleStructureMessageIsDropped(t *testing.T) {
	t.Parallel()
	m, p, _ := openStructuredPage(t)
	port := p.port.(*fakeSettingsPort)
	cmd := p.requestStructure("formatters")
	msgs := collect(cmd)
	if len(port.structureKeys) != 1 {
		t.Fatalf("structure requests = %v, want one", port.structureKeys)
	}
	// A structure for an older generation is dropped.
	msg := firstOf[settingsStructureMsg](t, msgs)
	msg.seq--
	m.Update(msg)
	before := len(m.overlays)
	_ = before
	for _, overlay := range m.overlays {
		if _, ok := overlay.(*settingsStructurePage); ok {
			t.Fatal("a stale structure page was pushed")
		}
	}
}

func TestReloadConfigAction(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: structuredSnapshot()}
	port.reload = SettingsChange{Snapshot: structuredSnapshot(), Reload: true}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, SettingsPort: port})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	cmd, ok := m.settingsAction("reload_config", nil)
	if !ok || cmd == nil {
		t.Fatal("reload_config started no command")
	}
	m.Update(firstOf[settingsChangeMsg](t, collect(cmd)))
	if !strings.Contains(m.notice, "Config reloaded") {
		t.Errorf("notice = %q, want the reload notice", m.notice)
	}

	// No changed settings: the no-change notice.
	port2 := &fakeSettingsPort{reload: SettingsChange{Reload: true}}
	m2 := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, SettingsPort: port2})
	cmd, _ = m2.settingsAction("reload_config", nil)
	m2.Update(firstOf[settingsChangeMsg](t, collect(cmd)))
	if !strings.Contains(m2.notice, "Config reloaded: no changes") {
		t.Errorf("notice = %q, want the no-change notice", m2.notice)
	}

	// No port: the action says so.
	m3 := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	if _, ok := m3.settingsAction("reload_config", nil); !ok {
		t.Fatal("reload_config was not taken without a port")
	}
	if !strings.Contains(m3.notice, "unavailable") {
		t.Errorf("notice = %q, want the unavailable notice", m3.notice)
	}
}

func TestStructurePageDualOpensRightSide(t *testing.T) {
	t.Parallel()
	m, p, _ := openStructuredPage(t)
	sub := requestStructurePage(t, m, p, "editor.status_bar.right")
	if sub.side != "right" {
		t.Errorf("side = %q, want right for the right row", sub.side)
	}
	_ = palette.RunCommandMsg{}
}
