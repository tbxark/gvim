package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// plumbingSnapshot is a small page with the new row kinds: a page row, an
// information row, and a page for the language fields.
func plumbingSnapshot() SettingsSnapshot {
	return SettingsSnapshot{
		UserFile:        "/cfg/config.json",
		ProjectFile:     "/ws/.gvim/config.json",
		ProjectWritable: true,
		Sections: []SettingsSection{
			{Name: "Language Servers", Items: []SettingsItem{
				{Key: "lsp", Name: "Language Servers", Section: "Language Servers", Kind: "page", Value: "2 servers"},
			}},
			{Name: "Languages", Items: []SettingsItem{
				{Key: "languages.go", Name: "go", Section: "Languages", Kind: "page", Value: "1 override"},
				{Key: "lsp.go", Name: "go servers", Section: "Languages", Kind: "page", Value: "1 server"},
			}},
			{Name: "Terminal", Items: []SettingsItem{
				{Key: "terminal.shell.args", Name: "Shell Arguments", Section: "Terminal", Kind: "page", Value: "0 arguments"},
			}},
			{Name: "Keybindings", Items: []SettingsItem{
				{Key: "keybindings", Name: "Keybindings", Section: "Keybindings", Kind: "page", Value: "2 bindings"},
			}},
			{Name: "Environment", Items: []SettingsItem{
				{Key: "env.shell", Name: "Shell", Section: "Environment", Kind: "info", Value: "zsh", Note: "from $SHELL"},
			}},
		},
	}
}

// plumbingStructures are the sub-pages the new row kinds use.
func plumbingStructures() map[string]SettingsStructure {
	return map[string]SettingsStructure{
		"lsp": {Key: "lsp", Title: "LSP", Kind: "fields", Rows: []SettingsStructureRow{
			{Name: "go", Value: "go", Action: "act:lsp-restart"},
		}},
		"languages.go": {Key: "languages.go", Title: "Languages · go", Kind: "fields", Rows: []SettingsStructureRow{
			{Path: []string{"languages", "go", "tab_size"}, Name: "tab_size", Kind: "int", Value: "8", Inherited: true, Note: "builtin"},
			{Path: []string{"languages", "go", "use_tabs"}, Name: "use_tabs", Kind: "bool", Value: "on", Locked: true, Note: "user layer only"},
		}},
		"lsp.go": {Key: "lsp.go", Title: "Servers · go", Kind: "fields", Rows: []SettingsStructureRow{
			{Path: []string{"lsp", "go"}, Name: "servers (JSON)", Kind: "json", Value: "[]"},
		}},
		"terminal.shell.args": {Key: "terminal.shell.args", Title: "Shell Arguments", Kind: "list",
			Path: []string{"terminal", "shell", "args"}},
		"keybindings": {Key: "keybindings", Title: "Keybindings", Kind: "map", Filter: true, Rows: []SettingsStructureRow{
			{Path: []string{"keybindings", "Ctrl+S"}, Name: "Ctrl+S", Value: "save", Note: "preset"},
			{Path: []string{"keybindings", "Ctrl+X Ctrl+S"}, Name: "Ctrl+X Ctrl+S", Value: "save_all", Note: "user"},
			{Path: []string{"keybindings", "Alt+G"}, Name: "Alt+G", Value: "plugin:git.status", Note: "plugin", Locked: true},
		}},
	}
}

// openPlumbingPage opens the main page over the plumbing fixtures.
func openPlumbingPage(t *testing.T) (*Model, *settingsPage, *fakeSettingsPort) {
	t.Helper()
	port := &fakeSettingsPort{snap: plumbingSnapshot(), structures: plumbingStructures()}
	port.saveChange = SettingsChange{Snapshot: plumbingSnapshot()}
	m, p := openSettingsPage(t, port)
	return m, p, port
}

// TestSettingsInfoRowIsReadOnly checks the new information rows: they show the fact
// and its note, and Enter/Del explain they are read-only instead of editing.
func TestSettingsInfoRowIsReadOnly(t *testing.T) {
	t.Parallel()
	m, p, _ := openPlumbingPage(t)
	p.sel = settingsRowIndex(t, p, "env.shell")
	press(m, special(tea.KeyEnter))
	if !strings.Contains(m.notice, "read-only") || !strings.Contains(m.notice, "from $SHELL") {
		t.Fatalf("Enter on an info row said %q", m.notice)
	}
	press(m, special(tea.KeyDelete))
	if !strings.Contains(m.notice, "read-only") {
		t.Fatalf("Del on an info row said %q", m.notice)
	}
	if p.dirty() {
		t.Error("information rows made the page dirty")
	}
}

// TestStructureInheritedAndLockedRows checks the row states: an inherited field
// writes an override on Enter, a locked row refuses with its reason, and Del is a
// no-op notice on an inherited value.
func TestStructureInheritedAndLockedRows(t *testing.T) {
	t.Parallel()
	m, p, _ := openPlumbingPage(t)
	sub := requestStructurePage(t, m, p, "languages.go")
	// The inherited tab_size shows its effective value and an override on Enter.
	if row, ok := sub.itemAt(0); !ok || !row.item.Inherited || row.item.Value != "8" {
		t.Fatalf("first row = %+v, want the inherited tab_size", row)
	}
	sub.sel = 0
	acceptStructureOverlay(t, m, press(m, special(tea.KeyEnter)))
	done := typeAndSubmit(t, m, "4")
	m.Update(done)
	if raw, ok := p.pending[pathID([]string{"languages", "go", "tab_size"})]; !ok || string(raw.raw) != "4" {
		t.Fatalf("pending tab_size = %+v, want 4", p.pending)
	}
	// Del on an inherited row is a notice, not a removal.
	sub.sel = 0
	press(m, special(tea.KeyDelete))
	if !strings.Contains(m.notice, "inherits") {
		t.Fatalf("Del on an inherited row said %q", m.notice)
	}
	// The locked row explains itself.
	sub.sel = 1
	press(m, special(tea.KeyEnter))
	if !strings.Contains(m.notice, "read-only") || !strings.Contains(m.notice, "user layer only") {
		t.Fatalf("locked row said %q", m.notice)
	}
	if _, ok := p.pending[pathID([]string{"languages", "go", "use_tabs"})]; ok {
		t.Error("the locked row recorded a pending change")
	}
}

// TestStructureEmptyListWritesAtItsPath checks the list Path plumbing: a list with
// no rows can still take its first item, because the structure names the path.
func TestStructureEmptyListWritesAtItsPath(t *testing.T) {
	t.Parallel()
	m, p, _ := openPlumbingPage(t)
	sub := requestStructurePage(t, m, p, "terminal.shell.args")
	if len(sub.st.Rows) != 0 {
		t.Fatalf("rows = %+v, want an empty list", sub.st.Rows)
	}
	acceptStructureOverlay(t, m, press(m, ctrl('n')))
	done := typeAndSubmit(t, m, "--login")
	m.Update(done)
	raw, ok := p.pending[pathID([]string{"terminal", "shell", "args"})]
	if !ok {
		t.Fatalf("no pending args: %+v", p.pending)
	}
	var got []string
	if err := json.Unmarshal(raw.raw, &got); err != nil || !slices.Equal(got, []string{"--login"}) {
		t.Fatalf("pending args = %s (%v), want [--login]", raw.raw, err)
	}
}

// TestStructureActionCallsAct checks the action rows: "act:" runs through the port
// and the page refreshes itself with the result.
func TestStructureActionCallsAct(t *testing.T) {
	t.Parallel()
	m, p, port := openPlumbingPage(t)
	port.actMessage = "LSP go restarted"
	sub := requestStructurePage(t, m, p, "lsp")
	requests := len(port.structureKeys)
	cmd := press(m, special(tea.KeyEnter))
	done := firstOf[settingsActDoneMsg](t, collect(cmd))
	if len(port.acts) != 1 || port.acts[0].action != "lsp-restart" || port.acts[0].value != "go" {
		t.Fatalf("acts = %+v, want lsp-restart go", port.acts)
	}
	_, refresh := m.Update(done)
	if m.notice != "LSP go restarted" {
		t.Errorf("notice = %q, want the action's message", m.notice)
	}
	for _, msg := range collect(refresh) {
		m.Update(msg)
	}
	if got := len(port.structureKeys); got <= requests {
		t.Errorf("the page did not refresh after the action: structure requests %d", got)
	}
	if !sub.Active() {
		t.Error("the action closed the page")
	}
}

// TestStructureFilterAndPluginRow checks the filter and the locked plugin binding
// rows of the Keybindings page.
func TestStructureFilterAndPluginRow(t *testing.T) {
	t.Parallel()
	m, p, _ := openPlumbingPage(t)
	sub := requestStructurePage(t, m, p, "keybindings")
	if len(sub.rows) != 3 {
		t.Fatalf("rows = %d, want three bindings", len(sub.rows))
	}
	for _, r := range "preset" {
		press(m, typed(string(r)))
	}
	if len(sub.rows) != 1 || sub.rows[0].item.Name != "Ctrl+S" {
		t.Fatalf("filtered rows = %+v, want Ctrl+S only", sub.rows)
	}
	// The plugin row is locked and says so.
	press(m, special(tea.KeyEscape)) // clears the filter
	sub.sel = 2
	press(m, special(tea.KeyEnter))
	if !strings.Contains(m.notice, "read-only") {
		t.Fatalf("the locked plugin row said %q", m.notice)
	}
}

// TestStructureJSONFieldValidates checks the JSON field kind the LSP page edits:
// a whole-array value is written as JSON, and malformed JSON is refused with a
// notice instead of a pending edit.
func TestStructureJSONFieldValidates(t *testing.T) {
	t.Parallel()
	m, p, _ := openPlumbingPage(t)
	sub := requestStructurePage(t, m, p, "lsp.go")
	sub.sel = 0
	acceptStructureOverlay(t, m, press(m, special(tea.KeyEnter)))
	done := typeAndSubmit(t, m, `[{"command":"gopls"}]`)
	m.Update(done)
	raw, ok := p.pending[pathID([]string{"lsp", "go"})]
	if !ok || string(raw.raw) != `[{"command":"gopls"}]` {
		t.Fatalf("pending lsp.go = %+v, want the JSON array", p.pending)
	}
	// Malformed JSON is refused.
	delete(p.pending, pathID([]string{"lsp", "go"}))
	sub.sel = 0
	acceptStructureOverlay(t, m, press(m, special(tea.KeyEnter)))
	done = typeAndSubmit(t, m, "{not json")
	m.Update(done)
	if _, ok := p.pending[pathID([]string{"lsp", "go"})]; ok {
		t.Error("malformed JSON recorded a pending edit")
	}
	if !strings.Contains(m.notice, "valid JSON") {
		t.Errorf("notice = %q, want the JSON validation error", m.notice)
	}
}
