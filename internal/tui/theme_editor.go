package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// themeEditorPage is the native theme editor: a list of the resolved theme's
// sections and color keys with swatches, an in-memory working theme that the
// frame previews live, and Save/Save As/Delete through the ThemeCatalog. It is
// prompt/list driven; it has no widget runtime.
type themeEditorPage struct {
	model   *Model
	base    theme.Theme            // the theme the editor opened on
	values  map[string]theme.Color // the changes made in the editor
	working theme.Theme
	name    string // display name of the theme being edited
	ref     string // its portable ref
	// original is the theme the frame showed when the editor opened; Esc puts it
	// back when the working theme was only previewed.
	original theme.Theme
	origRef  string
	readOnly bool

	rows []themeEditorRow
	sel  int
	top  int
	page int

	dirty  bool
	active bool
	box    space.Rect
}

// themeEditorRow is a section header or one color key.
type themeEditorRow struct {
	header bool
	title  string
	key    theme.Key
}

var _ Overlay = (*themeEditorPage)(nil)

// openThemeEditor opens the editor over the active theme.
func (m *Model) openThemeEditor() {
	ref := m.currentThemeRef()
	page := &themeEditorPage{
		model:    m,
		base:     m.theme,
		original: m.theme,
		origRef:  ref,
		values:   make(map[string]theme.Color),
		working:  m.theme,
		name:     m.theme.Name,
		ref:      ref,
		active:   true,
	}
	page.readOnly = page.isBuiltin()
	if page.name == "" {
		page.name = ref
	}
	page.layout()
	m.pushOverlay(page)
}

// isBuiltin reports whether the edited theme is a bundled (read-only) theme.
func (p *themeEditorPage) isBuiltin() bool {
	if p.model.util.themeCatalog == nil {
		return true
	}
	for _, info := range p.model.themeInfos() {
		if info.Ref == p.ref || info.Name == p.name {
			return info.Builtin
		}
	}
	return true
}

// layout rebuilds the rows from the working theme, grouped by section.
func (p *themeEditorPage) layout() {
	selected := p.selectedKey()
	p.rows = p.rows[:0]
	lastSection := ""
	for _, key := range p.working.Keys() {
		if key.Section != lastSection {
			lastSection = key.Section
			p.rows = append(p.rows, themeEditorRow{header: true, title: key.Section})
		}
		p.rows = append(p.rows, themeEditorRow{key: key})
	}
	p.sel = p.firstRow()
	if selected != "" {
		for i, row := range p.rows {
			if !row.header && row.key.Name() == selected {
				p.sel = i
			}
		}
	}
	p.keepVisible()
}

// selectedKey is the theme key of the highlighted row.
func (p *themeEditorPage) selectedKey() string {
	if p.sel < 0 || p.sel >= len(p.rows) || p.rows[p.sel].header {
		return ""
	}
	return p.rows[p.sel].key.Name()
}

func (p *themeEditorPage) firstRow() int {
	for i, row := range p.rows {
		if !row.header {
			return i
		}
	}
	return 0
}

// Active reports whether the editor is open.
func (p *themeEditorPage) Active() bool { return p.active }

// Close closes the editor.
func (p *themeEditorPage) Close() { p.active = false }

// TakesEscape reports that Esc restores the theme (and asks when dirty).
func (p *themeEditorPage) TakesEscape() bool { return true }

// themeEditorOverlayMsg asks the root to push an overlay of the editor after
// the overlay stack has routed the key that asked for it.
type themeEditorOverlayMsg struct {
	page    *themeEditorPage
	overlay Overlay
}

// pushOverlay opens one overlay over the editor through a message.
func (p *themeEditorPage) pushOverlay(overlay Overlay) tea.Cmd {
	return func() tea.Msg { return themeEditorOverlayMsg{page: p, overlay: overlay} }
}

// themeEditorColorTag is the tag of the color prompt.
type themeEditorColorTag struct {
	page *themeEditorPage
	key  string
}

// themeEditorAttrsTag is the tag of the attributes prompt.
type themeEditorAttrsTag struct {
	page *themeEditorPage
	key  string
}

// themeEditorSaveAsTag is the tag of the Save As name prompt.
type themeEditorSaveAsTag struct {
	page *themeEditorPage
}

// themeEditorDiscardTag is the tag of the discard confirmation.
type themeEditorDiscardTag struct {
	page *themeEditorPage
}

// themeEditorDeleteTag is the tag of the delete confirmation.
type themeEditorDeleteTag struct {
	page *themeEditorPage
}

// key handles one key press.
func (p *themeEditorPage) key(msg tea.KeyPressMsg) tea.Cmd {
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
		return p.editColor()
	case "p":
		return p.openColorPicker()
	case "ctrl+a":
		return p.editAttrs()
	case "ctrl+s":
		return p.save()
	case "ctrl+shift+s":
		return p.saveAs()
	case "ctrl+d":
		return p.delete()
	case "ctrl+o":
		return p.openPicker()
	case "esc":
		return p.cancel()
	}
	return nil
}

// move shifts the highlight by d, skipping headers.
func (p *themeEditorPage) move(d int) {
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

// lastRow is the index of the last color key.
func (p *themeEditorPage) lastRow() int {
	for i := len(p.rows) - 1; i >= 0; i-- {
		if !p.rows[i].header {
			return i
		}
	}
	return 0
}

// pageCount is the visible row count of the last draw.
func (p *themeEditorPage) pageCount() int {
	if p.page > 0 {
		return p.page
	}
	return list.DefaultPage
}

// keepVisible scrolls the highlight into the page.
func (p *themeEditorPage) keepVisible() {
	page := p.pageCount()
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+page {
		p.top = p.sel - page + 1
	}
	p.top = max(0, min(p.top, len(p.rows)-page))
}

// editColor opens the color prompt of the highlighted key.
func (p *themeEditorPage) editColor() tea.Cmd {
	row, ok := p.rowAtSel()
	if !ok {
		return nil
	}
	value, _ := p.working.Value(row.key.Section, row.key.Key)
	return p.pushOverlay(prompt.NewLine(p.model.styles, prompt.LineOptions{
		Label:     row.key.Name() + ": ",
		Input:     value.Hex(),
		SelectAll: true,
		Tag:       themeEditorColorTag{page: p, key: row.key.Name()},
		Validate:  func(text string) error { _, err := theme.ParseColorText(text); return err },
	}))
}

// editAttrs opens the attributes prompt of the highlighted key.
func (p *themeEditorPage) editAttrs() tea.Cmd {
	row, ok := p.rowAtSel()
	if !ok {
		return nil
	}
	value, _ := p.working.Value(row.key.Section, row.key.Key)
	return p.pushOverlay(prompt.NewLine(p.model.styles, prompt.LineOptions{
		Label:     row.key.Name() + " attributes: ",
		Input:     theme.FormatModifiers(value.Modifiers),
		SelectAll: true,
		Tag:       themeEditorAttrsTag{page: p, key: row.key.Name()},
		Validate:  func(text string) error { _, err := theme.ParseModifiers(text); return err },
	}))
}

// rowAtSel returns the highlighted key row.
func (p *themeEditorPage) rowAtSel() (themeEditorRow, bool) {
	if p.sel < 0 || p.sel >= len(p.rows) || p.rows[p.sel].header {
		return themeEditorRow{}, false
	}
	return p.rows[p.sel], true
}

// setColor records a color edit and previews it.
func (p *themeEditorPage) setColor(key string, color theme.Color) {
	p.values[key] = color
	p.preview()
}

// preview recomputes the working theme from the base and the edits and rebuilds
// the frame's styles, so an edit is visible immediately.
func (p *themeEditorPage) preview() {
	working, err := theme.WithValues(p.base, p.values)
	if err != nil {
		p.model.notice = "Theme editor: " + err.Error()
		return
	}
	p.working = working
	p.model.theme = working
	p.model.rebuildTheme()
	p.dirty = true
	p.layout()
}

// previewValue previews a candidate colour on one key without recording it as an
// edit: the colour picker drives this while its selection moves.
func (p *themeEditorPage) previewValue(key theme.Key, value theme.Color) {
	values := make(map[string]theme.Color, len(p.values)+1)
	for name, c := range p.values {
		values[name] = c
	}
	values[key.Name()] = value
	working, err := theme.WithValues(p.base, values)
	if err != nil {
		p.model.notice = "Theme editor: " + err.Error()
		return
	}
	p.working = working
	p.model.theme = working
	p.model.rebuildTheme()
	p.layout()
}

// reapplyValues puts the frame and the editor rows back on the recorded edits,
// dropping a picker preview that was not applied.
func (p *themeEditorPage) reapplyValues() {
	working, err := theme.WithValues(p.base, p.values)
	if err != nil {
		return
	}
	p.working = working
	p.model.theme = working
	p.model.rebuildTheme()
	p.layout()
}

// save writes the working theme: a user theme replaces its file, a bundled theme
// routes to Save As.
func (p *themeEditorPage) save() tea.Cmd {
	if p.readOnly {
		p.model.notice = "Theme " + p.name + " is bundled: use Save As to keep a copy"
		return p.saveAs()
	}
	if p.model.util.themeCatalog == nil {
		p.model.notice = "Theme editor: there is no theme catalog to save to"
		return nil
	}
	catalog := p.model.util.themeCatalog
	name, working := p.name, p.working
	return func() tea.Msg {
		ref, err := catalog.SaveTheme(name, working)
		return themeEditorSaveDoneMsg{page: p, ref: ref, err: err}
	}
}

// saveAs asks for a name and writes the working theme under it.
func (p *themeEditorPage) saveAs() tea.Cmd {
	if p.model.util.themeCatalog == nil {
		p.model.notice = "Theme editor: there is no theme catalog to save to"
		return nil
	}
	return p.pushOverlay(prompt.NewLine(p.model.styles, prompt.LineOptions{
		Label:     "Save theme as: ",
		Input:     p.name + "-copy",
		SelectAll: true,
		Tag:       themeEditorSaveAsTag{page: p},
	}))
}

// delete removes a user theme, after a confirmation.
func (p *themeEditorPage) delete() tea.Cmd {
	if p.readOnly {
		p.model.notice = "Theme " + p.name + " is bundled and cannot be deleted"
		return nil
	}
	return p.pushOverlay(prompt.NewConfirm(p.model.styles, "Delete theme "+p.name+"?", themeEditorDeleteTag{page: p}))
}

// openPicker lists the catalog's themes to open.
func (p *themeEditorPage) openPicker() tea.Cmd {
	infos := p.model.themeInfos()
	rows := make([]list.Row, len(infos))
	for i, info := range infos {
		right := "user"
		if info.Builtin {
			right = "builtin"
		}
		rows[i] = list.Row{Left: info.Name, Right: right, Value: info.Ref}
	}
	picker := list.New("Open theme", rows, p.model.listChrome(), func(v *list.View, row list.Row) tea.Cmd {
		v.Close()
		return p.openRef(row.Value)
	})
	picker.Fit = true
	return p.pushOverlay(picker)
}

// openRef switches the editor to another theme.
func (p *themeEditorPage) openRef(ref string) tea.Cmd {
	if p.model.util.themeCatalog == nil {
		return nil
	}
	catalog := p.model.util.themeCatalog
	return func() tea.Msg {
		loaded, err := catalog.Load(ref)
		return themeEditorOpenDoneMsg{page: p, theme: loaded, err: err}
	}
}

// cancel restores the original theme and closes, asking first when dirty.
func (p *themeEditorPage) cancel() tea.Cmd {
	if !p.dirty {
		p.closeAndRestore()
		return nil
	}
	return p.pushOverlay(prompt.NewConfirm(p.model.styles, "Discard theme changes?", themeEditorDiscardTag{page: p}))
}

// closeAndRestore puts the theme that was applied before the editor opened back.
func (p *themeEditorPage) closeAndRestore() {
	p.model.theme = p.original
	p.model.themeRef = p.origRef
	p.model.rebuildTheme()
	p.Close()
}

// themeEditorSaveDoneMsg is the result of a Save/Save As command.
type themeEditorSaveDoneMsg struct {
	page *themeEditorPage
	ref  string
	err  error
}

// themeEditorOpenDoneMsg is the result of opening another theme.
type themeEditorOpenDoneMsg struct {
	page  *themeEditorPage
	theme theme.Theme
	err   error
}

// Update handles the editor's keys, its prompts' answers and its commands.
func (p *themeEditorPage) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = p.key(msg)
	case tea.MouseClickMsg:
		mo := tea.Mouse(msg)
		if mo.Button == tea.MouseLeft {
			if i, ok := p.rowAt(mo.X, mo.Y); ok && !p.rows[i].header {
				p.sel = i
				// A click on the row's own swatch opens the colour picker for it,
				// as Fresh's editor does; clicking elsewhere only selects the row.
				if mo.X <= p.box.X+3 {
					cmd = p.openColorPicker()
				}
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
	case themeEditorSaveDoneMsg:
		cmd = p.saveDone(msg)
	case themeEditorOpenDoneMsg:
		cmd = p.openDone(msg)
	case themeEditorDeleteDoneMsg:
		cmd = p.deleteDone(msg)
	}
	if !p.active {
		return nil, cmd
	}
	return p, cmd
}

// promptDone takes a prompt answer.
func (p *themeEditorPage) promptDone(done prompt.Done) tea.Cmd {
	switch tag := done.Tag.(type) {
	case themeEditorColorTag:
		if tag.page != p || done.Outcome != prompt.Submitted {
			return nil
		}
		color, err := theme.ParseColorText(done.Text)
		if err != nil {
			p.model.notice = "Theme editor: " + err.Error()
			return nil
		}
		if parts := strings.SplitN(tag.key, ".", 2); len(parts) == 2 {
			if old, ok := p.working.Value(parts[0], parts[1]); ok {
				color.Modifiers = old.Modifiers
			}
		}
		p.setColor(tag.key, color)
	case themeEditorAttrsTag:
		if tag.page != p || done.Outcome != prompt.Submitted {
			return nil
		}
		mods, err := theme.ParseModifiers(done.Text)
		if err != nil {
			p.model.notice = "Theme editor: " + err.Error()
			return nil
		}
		parts := strings.SplitN(tag.key, ".", 2)
		if len(parts) != 2 {
			return nil
		}
		color, ok := p.working.Value(parts[0], parts[1])
		if !ok {
			return nil
		}
		color.Modifiers = mods
		p.setColor(tag.key, color)
	case themeEditorSaveAsTag:
		if tag.page != p || done.Outcome != prompt.Submitted {
			return nil
		}
		name := strings.TrimSpace(done.Text)
		if name == "" {
			p.model.notice = "Theme editor: the name must not be empty"
			return nil
		}
		catalog := p.model.util.themeCatalog
		working := p.working
		return func() tea.Msg {
			ref, err := catalog.SaveTheme(name, working)
			return themeEditorSaveDoneMsg{page: p, ref: ref, err: err}
		}
	case themeEditorDiscardTag:
		if tag.page == p && done.Outcome == prompt.Yes {
			p.closeAndRestore()
		}
	case themeEditorDeleteTag:
		if tag.page != p || done.Outcome != prompt.Yes || p.model.util.themeCatalog == nil {
			return nil
		}
		catalog := p.model.util.themeCatalog
		ref := p.ref
		return func() tea.Msg {
			return themeEditorDeleteDoneMsg{page: p, ref: ref, err: catalog.DeleteTheme(ref)}
		}
	}
	return nil
}

// themeEditorDeleteDoneMsg is the result of a delete.
type themeEditorDeleteDoneMsg struct {
	page *themeEditorPage
	ref  string
	err  error
}

// saveDone applies a saved theme and closes the editor.
func (p *themeEditorPage) saveDone(msg themeEditorSaveDoneMsg) tea.Cmd {
	if msg.page != p {
		return nil
	}
	if msg.err != nil {
		p.model.notice = "Theme editor: " + msg.err.Error()
		return nil
	}
	p.dirty = false
	p.Close()
	// The saved theme is now a file: apply it through the catalog so the ref and
	// the registry are consistent.
	p.model.applyTheme(msg.ref)
	p.model.notice = "Theme saved to " + msg.ref
	return nil
}

// openDone switches the editor to a loaded theme.
func (p *themeEditorPage) openDone(msg themeEditorOpenDoneMsg) tea.Cmd {
	if msg.page != p {
		return nil
	}
	if msg.err != nil {
		p.model.notice = "Theme editor: " + msg.err.Error()
		return nil
	}
	p.base = msg.theme
	p.working = msg.theme
	p.values = make(map[string]theme.Color)
	p.name = msg.theme.Name
	p.ref = p.model.currentThemeRef()
	p.readOnly = p.isBuiltin()
	p.dirty = false
	p.layout()
	p.model.theme = msg.theme
	p.model.rebuildTheme()
	return nil
}

// rowAt maps a frame cell to a row.
func (p *themeEditorPage) rowAt(x, y int) (int, bool) {
	b := p.box
	if !b.Contains(x, y) || y < b.Y+1 || y >= b.Y+b.H-1 {
		return 0, false
	}
	i := p.top + (y - b.Y - 1)
	if i < 0 || i >= len(p.rows) {
		return 0, false
	}
	return i, true
}

// Draw paints the editor.
func (p *themeEditorPage) Draw(g *grid.Grid) {
	n := min(len(p.rows), list.MaxHeight-3)
	box := list.Centred(g.Width(), g.Height(), list.MaxWidth, n+3)
	p.box = box
	if box.W == 0 || box.H < 3 {
		return
	}
	p.page = box.H - 3
	p.keepVisible()
	ov := p.model.styles.Overlay
	title := "Theme editor — " + p.name
	if p.readOnly {
		title += " (builtin)"
	}
	if p.dirty {
		title += " *"
	}
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, title)
	end := box.X + box.W - 2
	for k := 0; k < p.page && p.top+k < len(p.rows); k++ {
		row := p.rows[p.top+k]
		y := box.Y + 1 + k
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
		value, ok := p.working.Value(row.key.Section, row.key.Key)
		if !ok {
			continue
		}
		sw := st
		sw.FG = style.RenderedColor(value)
		g.Put(box.X+2, y, box.X+3, "██", sw)
		text := value.Hex()
		if attrs := theme.FormatModifiers(value.Modifiers); attrs != "" {
			text += " " + attrs
		}
		textX := end - ansi.StringWidth(text)
		g.Put(max(textX, box.X+4), y, end, text, rightSt)
		g.Put(box.X+4, y, max(textX-1, box.X+5), row.key.Key, st)
	}
	ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W,
		"Enter color · p picker · ^A attrs · ^S save · ^⇧S save as · ^D delete · ^O open · Esc")
}

// deleteDone applies a delete: open the fallback theme and close.
func (p *themeEditorPage) deleteDone(msg themeEditorDeleteDoneMsg) tea.Cmd {
	if msg.page != p {
		return nil
	}
	if msg.err != nil {
		p.model.notice = "Theme editor: " + msg.err.Error()
		return nil
	}
	p.dirty = false
	p.Close()
	p.model.applyTheme("dark")
	p.model.notice = "Theme deleted"
	return nil
}

// themeEditorMessage handles the editor's overlay messages.
func (m *Model) themeEditorMessage(msg tea.Msg) (tea.Cmd, bool) {
	d, ok := msg.(themeEditorOverlayMsg)
	if !ok {
		return nil, false
	}
	if d.page.Active() {
		m.pushOverlay(d.overlay)
	}
	return nil, true
}
