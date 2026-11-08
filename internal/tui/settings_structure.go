package tui

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/space"
)

// settingsStructurePage is the sub-page of one structured setting: the
// formatters map, the language entries, one language's scalar overrides, the
// status-bar element lists (a dual list), or terminal.shell.args. It is generic:
// the port hands it the rows and their config paths, and it edits the parent
// Settings page's pending changes. Nothing is written until the main page is
// saved with Ctrl+S.
type settingsStructurePage struct {
	parent *settingsPage
	st     SettingsStructure
	// Working values, so an edit shows at once. They are written to the parent's
	// pending changes as they change.
	values  map[string]string // map entry and fields values, by path id
	items   []string          // list kind
	entries []string          // languages kind: the language ids shown
	left    []string          // dual kind
	right   []string          // dual kind
	side    string            // dual kind: the focused side
	filter  []rune            // the typed row filter (st.Filter)
	rows    []structureRow
	sel     int
	top     int
	page    int
	seq     int // generation of the child structure request in flight
	active  bool
	box     space.Rect
}

// structureRow is one line of a structure page: a group header, an entry, or the
// add row.
type structureRow struct {
	header bool
	title  string // a group header
	add    bool
	group  string // "left", "right" or "available" for a dual list
	item   SettingsStructureRow
}

var _ Overlay = (*settingsStructurePage)(nil)

// newSettingsStructurePage returns the sub-page of st over the parent page.
func newSettingsStructurePage(parent *settingsPage, st SettingsStructure) *settingsStructurePage {
	p := &settingsStructurePage{
		parent: parent,
		st:     st,
		values: make(map[string]string),
		side:   "left",
		active: true,
	}
	if strings.HasSuffix(st.Key, ".right") {
		p.side = "right"
	}
	p.rebuild()
	return p
}

// Active reports whether the sub-page is still open.
func (p *settingsStructurePage) Active() bool { return p.active }

// Close closes the sub-page.
func (p *settingsStructurePage) Close() { p.active = false }

// TakesEscape reports that the sub-page handles Esc itself: it goes back.
func (p *settingsStructurePage) TakesEscape() bool { return true }

// rebuild fills the working values from the structure and lays out the rows.
func (p *settingsStructurePage) rebuild() {
	p.entries = p.entries[:0]
	p.items = p.items[:0]
	p.left = p.left[:0]
	p.right = p.right[:0]
	for _, row := range p.st.Rows {
		switch p.st.Kind {
		case "map":
			p.values[pathID(row.Path)] = row.Value
		case "fields":
			p.values[pathID(row.Path)] = row.Value
		case "list":
			p.items = append(p.items, row.Name)
		case "languages":
			p.entries = append(p.entries, row.Name)
		case "entries":
			// The rows keep their raw entry JSON for remove/add; nothing to copy.
		case "dual":
			if row.Group == "right" {
				p.right = append(p.right, row.Name)
			} else {
				p.left = append(p.left, row.Name)
			}
		}
	}
	p.layout()
}

// layout turns the working values into the drawn rows.
func (p *settingsStructurePage) layout() {
	selected := p.selectedRowKey()
	var rows []structureRow
	switch p.st.Kind {
	case "map":
		for _, row := range p.st.Rows {
			value := p.values[pathID(row.Path)]
			if pending, ok := p.pendingFor(row.Path); ok && pending.remove {
				value = "(default)"
			}
			row.Value = value
			rows = append(rows, structureRow{item: row})
		}
	case "fields":
		for _, row := range p.st.Rows {
			value := p.values[pathID(row.Path)]
			if pending, ok := p.pendingFor(row.Path); ok && pending.remove {
				value = "(default)"
			}
			row.Value = value
			rows = append(rows, structureRow{item: row})
		}
	case "list":
		path := p.listPath()
		for i, item := range p.items {
			rows = append(rows, structureRow{item: SettingsStructureRow{Path: path, Name: strconv.Itoa(i + 1), Value: item}})
		}
	case "languages":
		for _, name := range p.entries {
			rows = append(rows, structureRow{item: SettingsStructureRow{Path: []string{"languages", name}, Name: name, Value: "configured", Child: "languages." + name}})
		}
	case "entries":
		for _, row := range p.st.Rows {
			rows = append(rows, structureRow{item: row})
		}
	case "dual":
		rows = append(rows, structureRow{header: true, title: "Left", group: "left"})
		for _, name := range p.left {
			rows = append(rows, structureRow{group: "left", item: SettingsStructureRow{Path: []string{"editor", "status_bar", "left"}, Name: name}})
		}
		rows = append(rows, structureRow{header: true, title: "Right", group: "right"})
		for _, name := range p.right {
			rows = append(rows, structureRow{group: "right", item: SettingsStructureRow{Path: []string{"editor", "status_bar", "right"}, Name: name}})
		}
		rows = append(rows, structureRow{header: true, title: "Available", group: "available"})
		for _, name := range p.available() {
			rows = append(rows, structureRow{group: "available", item: SettingsStructureRow{Name: name}})
		}
	}
	if p.st.Filter && len(p.filter) > 0 {
		rows = p.filterRows(rows)
	}
	if p.st.AddLabel != "" && p.st.Kind != "dual" {
		rows = append(rows, structureRow{add: true, item: SettingsStructureRow{Name: p.st.AddLabel}})
	} else if p.st.AddLabel != "" && p.st.Kind == "dual" {
		rows = append(rows, structureRow{add: true, item: SettingsStructureRow{Name: p.st.AddLabel}})
	}
	p.rows = rows
	p.sel = p.firstRow()
	if selected != "" {
		for i, row := range p.rows {
			if !row.header && rowKey(row) == selected {
				p.sel = i
				break
			}
		}
	}
	p.keepVisible()
}

// filterRows narrows the rows to those matching the typed filter, on the name,
// value, note and group. Headers are dropped while a filter is on: the matches are
// what the reader asked for.
func (p *settingsStructurePage) filterRows(rows []structureRow) []structureRow {
	needle := strings.ToLower(string(p.filter))
	out := make([]structureRow, 0, len(rows))
	for _, row := range rows {
		if row.header {
			continue
		}
		text := strings.ToLower(row.item.Name + " " + row.item.Value + " " + row.item.Note + " " + row.item.Group)
		if strings.Contains(text, needle) {
			out = append(out, row)
		}
	}
	return out
}

// rowKey returns a stable identity for the selection across layouts.
func rowKey(row structureRow) string {
	if row.add {
		return "add"
	}
	if row.header {
		return "header:" + row.title
	}
	if row.group != "" {
		return row.group + ":" + row.item.Name
	}
	if len(row.item.Path) > 0 {
		return pathID(row.item.Path) + ":" + row.item.Name
	}
	return row.item.Name
}

// selectedRowKey returns the row key of the highlight.
func (p *settingsStructurePage) selectedRowKey() string {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return ""
	}
	return rowKey(p.rows[p.sel])
}

// pendingFor returns the parent's pending change at path.
func (p *settingsStructurePage) pendingFor(path []string) (settingsPending, bool) {
	pending, ok := p.parent.pending[pathID(path)]
	return pending, ok
}

// available returns the dual list's elements that are on neither side.
func (p *settingsStructurePage) available() []string {
	var out []string
	for _, option := range p.st.Available {
		if slices.Contains(p.left, option) || slices.Contains(p.right, option) {
			continue
		}
		out = append(out, option)
	}
	return out
}

// firstRow returns the index of the first non-header row, or 0.
func (p *settingsStructurePage) firstRow() int {
	for i, row := range p.rows {
		if !row.header {
			return i
		}
	}
	return 0
}

// itemAt returns the row at i.
func (p *settingsStructurePage) itemAt(i int) (structureRow, bool) {
	if i < 0 || i >= len(p.rows) {
		return structureRow{}, false
	}
	return p.rows[i], true
}

// key handles one key press.
func (p *settingsStructurePage) key(msg tea.KeyPressMsg) tea.Cmd {
	switch tea.Key(msg).Keystroke() {
	case "up":
		p.move(-1)
	case "down":
		p.move(1)
	case "pgup":
		p.move(-p.pageCount())
	case "pgdown":
		p.move(p.pageCount())
	case "home":
		p.sel = p.firstRow()
		p.keepVisible()
	case "end":
		p.sel = p.lastRow()
		p.keepVisible()
	case "enter":
		return p.edit()
	case "delete":
		p.remove()
	case "ctrl+n":
		return p.addEntry()
	case "tab":
		if p.st.Kind == "dual" {
			if p.side == "left" {
				p.side = "right"
			} else {
				p.side = "left"
			}
			p.parent.m.notice = "Settings: adding to the " + p.side + " side"
		}
	case "alt+up":
		p.reorder(-1)
	case "alt+down":
		p.reorder(1)
	case "ctrl+s":
		// Save from the sub-page: close it and save the parent page.
		p.Close()
		return p.parent.save()
	case "esc":
		// A filter is cleared before the page closes, as on the main page.
		if len(p.filter) > 0 {
			p.filter = nil
			p.layout()
			return nil
		}
		p.Close()
	case "backspace":
		if n := len(p.filter); n > 0 {
			p.filter = p.filter[:n-1]
			p.layout()
		}
	default:
		if p.st.Filter && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && msg.Text != "" {
			p.filter = append(p.filter, []rune(msg.Text)...)
			p.layout()
		}
	}
	return nil
}

// move shifts the highlight by d rows, stopping at the ends and skipping
// headers.
func (p *settingsStructurePage) move(d int) {
	step, n := 1, d
	if d < 0 {
		step, n = -1, -d
	}
	for range n {
		next := p.sel + step
		for next >= 0 && next < len(p.rows) && p.rows[next].header {
			next += step
		}
		if next < 0 || next >= len(p.rows) {
			break
		}
		p.sel = next
	}
	p.keepVisible()
}

// lastRow returns the index of the last non-header row.
func (p *settingsStructurePage) lastRow() int {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if !p.rows[i].header {
			return i
		}
	}
	return 0
}

// pageCount is the visible row count of the last draw.
func (p *settingsStructurePage) pageCount() int {
	if p.page > 0 {
		return p.page
	}
	return list.DefaultPage
}

// keepVisible scrolls so that the highlight is inside the page.
func (p *settingsStructurePage) keepVisible() {
	page := p.pageCount()
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+page {
		p.top = p.sel - page + 1
	}
	p.top = max(0, min(p.top, len(p.rows)-page))
}

// edit opens the editor of the highlighted row:
//   - dual: a left/right entry moves to the other side, an available element is
//     added to the focused side;
//   - map: the value prompt; list: the item prompt; fields: the scalar editor;
//   - languages: the language's fields sub-page; add: the add flow.
func (p *settingsStructurePage) edit() tea.Cmd {
	row, ok := p.itemAt(p.sel)
	if !ok || row.header {
		return nil
	}
	if row.add {
		return p.addEntry()
	}
	if row.item.Locked {
		why := row.item.Note
		if why == "" {
			why = "this layer cannot change it"
		}
		p.parent.m.notice = "Settings: " + row.item.Name + " is read-only — " + why
		return nil
	}
	if row.item.Action != "" {
		return p.runAction(row.item)
	}
	if p.st.Kind == "dual" {
		p.moveDual(row.group, row.item.Name)
		return nil
	}
	if p.st.Kind == "languages" || row.item.Child != "" {
		return p.requestChild(row.item.Child)
	}
	switch p.st.Kind {
	case "map":
		return p.promptValue(row.item.Path, "Value: ", p.values[pathID(row.item.Path)], "string")
	case "list":
		return p.promptList(row.item.Path, row.item.Value)
	case "fields":
		return p.editField(row.item)
	}
	return nil
}

// moveDual moves an element between the two sides, or from Available into the
// focused side.
func (p *settingsStructurePage) moveDual(group, name string) {
	switch group {
	case "left", "right":
		if group == "left" {
			p.left = removeString(p.left, name)
			p.right = append(p.right, name)
		} else {
			p.right = removeString(p.right, name)
			p.left = append(p.left, name)
		}
	default: // available
		if p.side == "right" {
			p.right = append(p.right, name)
		} else {
			p.left = append(p.left, name)
		}
	}
	p.writeDual()
	p.layout()
}

// commentStyle returns the footer hints of the page.
func (p *settingsStructurePage) footer() string {
	switch p.st.Kind {
	case "map":
		return "Enter edit · ^N add · Del remove · Esc back · ^S save"
	case "list":
		return "Enter edit · ^N add · Del remove · Alt+↑/↓ move · Esc back · ^S save"
	case "fields":
		return "Enter edit · Del reset · Esc back · ^S save"
	case "languages":
		return "Enter fields · ^N add · Del remove · Esc back · ^S save"
	case "entries":
		return "Enter fields · ^N add · Del remove · Esc back · ^S save"
	case "dual":
		return "Tab side · Enter move/add · Del remove · Alt+↑/↓ move · Esc back · ^S save"
	}
	return "Esc back"
}

// promptValue opens a one-line prompt tagged for an edit or an add. The prompt
// is pushed from a message: pushing an overlay inside an overlay's Update would
// race the overlay stack's own update of the top slot.
func (p *settingsStructurePage) promptValue(path []string, label, input, kind string) tea.Cmd {
	return structurePrompt(p, prompt.LineOptions{
		Label:     label,
		Input:     input,
		SelectAll: true,
		Tag:       structurePromptTag{page: p, action: "edit", path: slices.Clone(path), kind: kind},
	})
}

// structurePrompt returns a command that opens one prompt over the page.
func structurePrompt(p *settingsStructurePage, options prompt.LineOptions) tea.Cmd {
	overlay := prompt.NewLine(p.parent.m.styles, options)
	return func() tea.Msg { return structureOverlayMsg{page: p, overlay: overlay} }
}

// structureOverlayMsg asks the root to push an overlay of a structure page after
// the stack has finished routing the key that asked for it.
type structureOverlayMsg struct {
	page    *settingsStructurePage
	overlay Overlay
}

// promptList opens the item prompt of a list row.
func (p *settingsStructurePage) promptList(path []string, item string) tea.Cmd {
	return structurePrompt(p, prompt.LineOptions{
		Label:     "Item: ",
		Input:     item,
		SelectAll: true,
		Tag:       structurePromptTag{page: p, action: "edit-item", path: []string{item}, kind: "string"},
	})
}

// editField opens the scalar editor of a language field row.
func (p *settingsStructurePage) editField(row SettingsStructureRow) tea.Cmd {
	switch row.Kind {
	case "bool":
		next := "on"
		if p.values[pathID(row.Path)] == "on" {
			next = "off"
		}
		p.setField(row.Path, next, row.Kind)
		return nil
	case "enum":
		rows := make([]list.Row, len(row.Choices))
		for i, choice := range row.Choices {
			rows[i] = list.Row{Left: choice, Value: choice}
		}
		path, kind := slices.Clone(row.Path), row.Kind
		v := list.New("Set "+row.Name, rows, p.parent.m.listChrome(), func(v *list.View, chosen list.Row) tea.Cmd {
			v.Close()
			p.setField(path, chosen.Value, kind)
			return nil
		})
		v.Fit = true
		return func() tea.Msg { return structureOverlayMsg{page: p, overlay: v} }
	default:
		return structurePrompt(p, prompt.LineOptions{
			Label:     row.Name + ": ",
			Input:     p.values[pathID(row.Path)],
			SelectAll: true,
			Tag:       structurePromptTag{page: p, action: "field", path: slices.Clone(row.Path), kind: row.Kind},
		})
	}
}

// addEntry starts the add flow of the structure.
func (p *settingsStructurePage) addEntry() tea.Cmd {
	if p.st.Key == "lsp" {
		return structurePrompt(p, prompt.LineOptions{
			Label: "Language: ",
			Tag:   structurePromptTag{page: p, action: "add-lsp-language"},
		})
	}
	switch p.st.Kind {
	case "map":
		return structurePrompt(p, prompt.LineOptions{
			Label: "Key: ",
			Tag:   structurePromptTag{page: p, action: "add-name"},
		})
	case "list":
		return structurePrompt(p, prompt.LineOptions{
			Label: "Item: ",
			Tag:   structurePromptTag{page: p, action: "add-list", kind: "string"},
		})
	case "entries":
		return structurePrompt(p, prompt.LineOptions{
			Label: p.st.AddLabel + " ",
			Tag:   structurePromptTag{page: p, action: "add-entry"},
		})
	case "languages":
		options := p.st.Available
		if len(options) == 0 {
			p.parent.m.notice = "Settings: every known language is already configured"
			return nil
		}
		rows := make([]list.Row, len(options))
		for i, name := range options {
			rows[i] = list.Row{Left: name, Value: name}
		}
		v := list.New("Add language", rows, p.parent.m.listChrome(), func(v *list.View, chosen list.Row) tea.Cmd {
			v.Close()
			return p.requestChild("languages." + chosen.Value)
		})
		v.Fit = true
		return func() tea.Msg { return structureOverlayMsg{page: p, overlay: v} }
	}
	return nil
}

// requestChild asks the port for a child structure, such as one language's
// fields.
func (p *settingsStructurePage) requestChild(key string) tea.Cmd {
	port, layer := p.parent.port, p.parent.layer
	p.seq++
	seq := p.seq
	return func() tea.Msg {
		structure, err := port.Structure(key, layer)
		return settingsStructureMsg{sub: p, seq: seq, structure: structure, err: err}
	}
}

// runAction runs a row's action. A "ui:" action is handed to the root after the
// Settings overlays close (a page cannot push an overlay from its own Update); an
// "act:" action runs on the port's live parts from a command and the page then
// refreshes itself with the result.
func (p *settingsStructurePage) runAction(row SettingsStructureRow) tea.Cmd {
	switch {
	case strings.HasPrefix(row.Action, "ui:"):
		action := strings.TrimPrefix(row.Action, "ui:")
		return func() tea.Msg { return settingsUIActionMsg{action: action} }
	case strings.HasPrefix(row.Action, "act:"):
		op := strings.TrimPrefix(row.Action, "act:")
		value := row.Value
		if value == "" {
			value = row.Name
		}
		port := p.parent.port
		return func() tea.Msg {
			message, err := port.Act(op, value)
			return settingsActDoneMsg{page: p, message: message, err: err}
		}
	}
	return nil
}

// requestSelf asks the port for this structure again, after an action changed the
// live state (an LSP server started or stopped).
func (p *settingsStructurePage) requestSelf() tea.Cmd {
	key, layer := p.st.Key, p.parent.layer
	p.seq++
	seq := p.seq
	port := p.parent.port
	return func() tea.Msg {
		st, err := port.Structure(key, layer)
		return settingsStructureMsg{sub: p, seq: seq, structure: st, err: err}
	}
}

// removeEntry takes one entry out of an "entries" structure and writes the whole
// array back: the entries are objects, which a single config path cannot address.
func (p *settingsStructurePage) removeEntry(item SettingsStructureRow) {
	kept := p.st.Rows[:0]
	for _, row := range p.st.Rows {
		if pathID(row.Path) != pathID(item.Path) {
			kept = append(kept, row)
		}
	}
	p.st.Rows = kept
	p.writeEntries()
}

// writeEntries records the whole entries array as one pending edit.
func (p *settingsStructurePage) writeEntries() {
	raws := make([]json.RawMessage, 0, len(p.st.Rows))
	for _, row := range p.st.Rows {
		if len(row.Raw) == 0 {
			return
		}
		raws = append(raws, row.Raw)
	}
	raw, err := json.Marshal(raws)
	if err != nil {
		return
	}
	p.parent.setPendingRaw(p.listPath(), raw)
}

// structKey returns the config path the structure's rows share, for writing the
// whole list: terminal.shell.args or editor.status_bar.left/right.
func (p *settingsStructurePage) listPath() []string {
	if len(p.st.Path) > 0 {
		return slices.Clone(p.st.Path)
	}
	if len(p.st.Rows) > 0 {
		return slices.Clone(p.st.Rows[0].Path)
	}
	return nil
}

// setField records a language field edit.
func (p *settingsStructurePage) setField(path []string, value, kind string) {
	p.values[pathID(path)] = value
	if raw, err := encodeStructureValue(kind, value); err == nil {
		p.parent.setPendingRaw(path, raw)
	} else {
		p.parent.m.notice = "Settings: " + err.Error()
	}
	p.layout()
}

// writeDual records both status-bar lists as whole-array edits.
func (p *settingsStructurePage) writeDual() {
	left, _ := json.Marshal(p.left)
	right, _ := json.Marshal(p.right)
	p.parent.setPendingRaw([]string{"editor", "status_bar", "left"}, left)
	p.parent.setPendingRaw([]string{"editor", "status_bar", "right"}, right)
}

// writeList records a whole-list edit.
func (p *settingsStructurePage) writeList() {
	body, _ := json.Marshal(p.items)
	p.parent.setPendingRaw(p.listPath(), body)
}

// remove deletes the highlighted entry (or moves a dual entry back to
// Available).
func (p *settingsStructurePage) remove() {
	row, ok := p.itemAt(p.sel)
	if !ok || row.header || row.add {
		return
	}
	if row.item.Locked {
		p.parent.m.notice = "Settings: " + row.item.Name + " is read-only in this layer"
		return
	}
	if row.item.Inherited {
		p.parent.m.notice = "Settings: " + row.item.Name + " already inherits its value"
		return
	}
	switch p.st.Kind {
	case "entries":
		p.removeEntry(row.item)
		p.layout()
		return
	case "map":
		p.parent.setPendingRemove(row.item.Path)
		delete(p.values, pathID(row.item.Path))
	case "fields":
		p.parent.setPendingRemove(row.item.Path)
		p.values[pathID(row.item.Path)] = ""
	case "languages":
		p.parent.setPendingRemove(row.item.Path)
		p.entries = removeString(p.entries, row.item.Name)
	case "list":
		p.items = removeString(p.items, row.item.Value)
		p.writeList()
	case "dual":
		if row.group == "left" || row.group == "right" {
			if row.group == "left" {
				p.left = removeString(p.left, row.item.Name)
			} else {
				p.right = removeString(p.right, row.item.Name)
			}
			p.writeDual()
		}
	}
	p.layout()
}

// reorder moves the highlighted list or dual entry within its group.
func (p *settingsStructurePage) reorder(d int) {
	row, ok := p.itemAt(p.sel)
	if !ok || row.header || row.add {
		return
	}
	switch p.st.Kind {
	case "list":
		i := slices.Index(p.items, row.item.Value)
		if i < 0 || i+d < 0 || i+d >= len(p.items) {
			return
		}
		p.items[i], p.items[i+d] = p.items[i+d], p.items[i]
		p.writeList()
	case "dual":
		side := p.left
		if row.group == "right" {
			side = p.right
		}
		i := slices.Index(side, row.item.Name)
		if i < 0 || i+d < 0 || i+d >= len(side) {
			return
		}
		side[i], side[i+d] = side[i+d], side[i]
		if row.group == "right" {
			p.right = side
		} else {
			p.left = side
		}
		p.writeDual()
	}
	p.layout()
	p.sel = max(0, min(p.sel+d, len(p.rows)-1))
	p.keepVisible()
}

// removeString returns s without the first item equal to value.
func removeString(s []string, value string) []string {
	out := make([]string, 0, len(s))
	for _, item := range s {
		if item == value {
			continue
		}
		out = append(out, item)
	}
	return out
}

// encodeStructureValue turns a display value into the JSON a config path holds.
func encodeStructureValue(kind, value string) (json.RawMessage, error) {
	switch kind {
	case "json":
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || !json.Valid([]byte(trimmed)) {
			return nil, errStructureValue("valid JSON", value)
		}
		return json.RawMessage(trimmed), nil
	case "bool":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "on", "true":
			return json.RawMessage("true"), nil
		case "off", "false":
			return json.RawMessage("false"), nil
		}
		return nil, errStructureValue("on", value)
	case "int":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return nil, errStructureValue("an integer", value)
		}
		return json.RawMessage(strconv.Itoa(n)), nil
	default:
		return json.Marshal(value)
	}
}

// errStructureValue is a malformed display value.
func errStructureValue(want, got string) error {
	return &structureValueError{want: want, got: got}
}

type structureValueError struct{ want, got string }

func (e *structureValueError) Error() string {
	return "must be " + e.want + ", got " + strconv.Quote(e.got)
}

// structurePromptTag is the tag of a structure page's prompt: what to do with
// the answer, and the config path it belongs to.
type structurePromptTag struct {
	page   *settingsStructurePage
	action string
	path   []string
	kind   string
}

// Update handles one message through the overlay contract.
func (p *settingsStructurePage) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = p.key(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			mo := tea.Mouse(msg)
			if i, ok := p.rowAt(mo.X, mo.Y); ok && !p.rows[i].header {
				p.sel = i
			}
		}
	case tea.MouseWheelMsg:
		switch tea.Mouse(msg).Button {
		case tea.MouseWheelUp:
			p.move(-list.WheelRows)
		case tea.MouseWheelDown:
			p.move(list.WheelRows)
		}
	case prompt.Done:
		cmd = p.promptDone(msg)
	}
	if !p.active {
		return nil, cmd
	}
	return p, cmd
}

// promptDone takes a prompt answer of the page.
func (p *settingsStructurePage) promptDone(done prompt.Done) tea.Cmd {
	tag, ok := done.Tag.(structurePromptTag)
	if !ok || tag.page != p || done.Outcome != prompt.Submitted {
		return nil
	}
	switch tag.action {
	case "edit":
		p.setField(tag.path, done.Text, tag.kind)
	case "edit-item":
		old := tag.path[0]
		for i, item := range p.items {
			if item == old {
				p.items[i] = done.Text
				break
			}
		}
		p.writeList()
		p.layout()
	case "field":
		p.setField(tag.path, done.Text, tag.kind)
	case "add-name":
		name := strings.TrimSpace(done.Text)
		if name == "" {
			p.parent.m.notice = "Settings: the key must not be empty"
			return nil
		}
		return structurePrompt(p, prompt.LineOptions{
			Label: "Value for " + name + ": ",
			Tag:   structurePromptTag{page: p, action: "add-value", path: []string{name}},
		})
	case "add-value":
		name := tag.path[0]
		path := []string{"formatters", name}
		raw, err := json.Marshal(done.Text)
		if err != nil {
			return nil
		}
		p.parent.setPendingRaw(path, raw)
		p.values[pathID(path)] = done.Text
		p.st.Rows = append(p.st.Rows, SettingsStructureRow{Path: path, Name: name, Value: done.Text})
		p.layout()
	case "add-list":
		p.items = append(p.items, done.Text)
		p.writeList()
		p.layout()
	case "add-lsp-language":
		language := strings.TrimSpace(done.Text)
		if language == "" {
			p.parent.m.notice = "Settings: the language must not be empty"
			return nil
		}
		return p.requestChild("lsp." + language)
	case "add-entry":
		command := strings.TrimSpace(done.Text)
		if command == "" {
			p.parent.m.notice = "Settings: the command must not be empty"
			return nil
		}
		entry, err := json.Marshal(map[string]any{"command": command, "enabled": true})
		if err != nil {
			return nil
		}
		index := len(p.st.Rows)
		path := append(slices.Clone(p.st.Path), strconv.Itoa(index))
		p.st.Rows = append(p.st.Rows, SettingsStructureRow{
			Path: path, Name: command, Value: "on", Raw: entry,
			Child: p.st.Key + "." + strconv.Itoa(index),
		})
		p.writeEntries()
		p.layout()
	}
	return nil
}

// rowAt returns the row at (x, y) of the last draw.
func (p *settingsStructurePage) rowAt(x, y int) (int, bool) {
	b := p.box
	if !b.Contains(x, y) || y < b.Y+2 || y >= b.Y+b.H-1 {
		return 0, false
	}
	i := p.top + (y - b.Y - 2)
	if i < 0 || i >= len(p.rows) {
		return 0, false
	}
	return i, true
}

// Draw paints the sub-page.
func (p *settingsStructurePage) Draw(g *grid.Grid) {
	n := min(len(p.rows), list.MaxHeight-3)
	box := list.Centred(g.Width(), g.Height(), list.MaxWidth, n+3)
	p.box = box
	if box.W == 0 || box.H < 3 {
		return
	}
	p.page = box.H - 3
	p.keepVisible()
	ov := p.parent.m.styles.Overlay
	title := "Settings · " + p.st.Title + " — " + settingsLayerTitle(p.parent.layer)
	if p.parent.dirty() {
		title += " *"
	}
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, title)
	end := box.X + box.W - 2
	// A small header line: the dual list's focused side, the page note, and the
	// typed filter.
	startRow := 2
	head := func(text string) {
		g.Fill(box.X+1, box.X+box.W-1, box.Y+startRow-1, ov.Box)
		g.Put(box.X+2, box.Y+startRow-1, end, text, ov.Muted)
		startRow++
	}
	if p.st.Kind == "dual" {
		head("Side: " + p.side)
	}
	if p.st.Note != "" {
		head(p.st.Note)
	}
	if p.st.Filter && len(p.filter) > 0 {
		head("Filter: " + string(p.filter))
	}
	p.page = box.H - startRow
	p.keepVisible()
	for k := 0; k < p.page && p.top+k < len(p.rows); k++ {
		row := p.rows[p.top+k]
		y := box.Y + startRow + k
		st, rightSt := ov.Box, ov.Muted
		if p.top+k == p.sel {
			st = ov.Selected
			rightSt = st
			g.Fill(box.X+1, box.X+box.W-1, y, st)
		}
		if row.header {
			st.Bold = true
			g.Put(box.X+2, y, end, row.title, st)
			continue
		}
		if row.add {
			g.Put(box.X+2, y, end, row.item.Name, ov.Muted)
			continue
		}
		name := row.item.Name
		if pending, ok := p.pendingFor(row.item.Path); ok && pending.remove {
			name = "* " + name
		}
		if row.item.Locked {
			name += " (locked)"
		}
		value := row.item.Value
		if p.st.Kind == "fields" || p.st.Kind == "map" {
			value = p.values[pathID(row.item.Path)]
			if value == "" && p.st.Kind == "fields" {
				value = "(default)"
			}
		}
		if row.item.Kind == "json" && !row.item.Inherited {
			value = "JSON (" + strconv.Itoa(len(value)) + " bytes) · Enter to edit"
		}
		if row.item.Inherited {
			if value == "" {
				value = "(inherit)"
			} else {
				value = "(inherit: " + value + ")"
			}
		}
		if row.item.Note != "" {
			value = strings.TrimSpace(value + " · " + row.item.Note)
		}
		valueX := end - ansi.StringWidth(value)
		g.Put(max(valueX, box.X+2), y, end, value, rightSt)
		room := max(valueX-box.X-4, 1)
		g.Put(box.X+2, y, box.X+2+room, name, st)
	}
	ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W, p.footer())
}
