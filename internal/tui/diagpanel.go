package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// diagState is the native Diagnostics sidebar section: whether it is open,
// collapsed, the body rows it asks the column for, the selected row and its
// scroll. The diagnostics themselves live in the root's workspace store
// (lspState.wsDiags); this is only where the section stands. See diagpanel.go.
type diagState struct {
	open      bool
	collapsed bool
	rows      int
	sel       int
	scroll    int
	focus     bool
}

// diagDefaultRows is the body height of the Diagnostics section when it first
// opens.
const diagDefaultRows = 8

// Open reports whether the section is in the column.
func (d *diagState) Open() bool { return d.open }

// Collapsed reports whether the section header is folded.
func (d *diagState) Collapsed() bool { return d.collapsed }

// Rows is the body rows the section asks the column for.
func (d *diagState) Rows() int {
	if d.rows <= 0 {
		return diagDefaultRows
	}
	return d.rows
}

// ToggleCollapsed folds or unfolds the section.
func (d *diagState) ToggleCollapsed() { d.collapsed = !d.collapsed }

// diagCounts is the number of diagnostics by severity, split as the status row's
// diagnostics element does: errors, warnings, and info and hints together.
type diagCounts struct {
	errors, warnings, info int
}

// Total is the number of diagnostics.
func (c diagCounts) Total() int { return c.errors + c.warnings + c.info }

// Label renders the counts as "E:1 W:2 I:3", each part only when it is not zero.
// No counts at all is the empty string.
func (c diagCounts) Label() string {
	var parts []string
	if c.errors > 0 {
		parts = append(parts, "E:"+strconv.Itoa(c.errors))
	}
	if c.warnings > 0 {
		parts = append(parts, "W:"+strconv.Itoa(c.warnings))
	}
	if c.info > 0 {
		parts = append(parts, "I:"+strconv.Itoa(c.info))
	}
	return strings.Join(parts, " ")
}

// severity counts one diagnostic.
func (c *diagCounts) add(severity int) {
	switch severity {
	case 1:
		c.errors++
	case 2:
		c.warnings++
	default:
		c.info++
	}
}

// countDiagnostics counts a set of diagnostics.
func countDiagnostics(items []Diagnostic) (c diagCounts) {
	for _, d := range items {
		c.add(d.Severity)
	}
	return c
}

// workspaceCounts counts every diagnostic of the workspace store, for the panel
// header and its empty state.
func (m *Model) workspaceCounts() (c diagCounts) {
	for _, file := range m.lsp.wsDiags {
		for _, d := range file.Items {
			c.add(d.Severity)
		}
	}
	return c
}

// diagRow is one row of the Diagnostics panel: a file header or one diagnostic of
// a file. The panel lists a header with the file's counts, then the file's
// diagnostics most severe first.
type diagRow struct {
	header bool
	path   string
	counts diagCounts
	// source is the diagnostic set's source: "publish" for a published set and
	// "workspace" for a workspace/diagnostic pull (usually a file that is not open).
	source string
	item   Diagnostic
}

// diagRows builds the panel's flat rows from the workspace store, which the port
// keeps sorted by path.
func (m *Model) diagRows() []diagRow {
	var rows []diagRow
	for _, file := range m.lsp.wsDiags {
		if len(file.Items) == 0 {
			continue
		}
		items := slices.Clone(file.Items)
		slices.SortStableFunc(items, func(a, b Diagnostic) int {
			if a.Severity != b.Severity {
				return a.Severity - b.Severity
			}
			if a.Start.Line != b.Start.Line {
				return a.Start.Line - b.Start.Line
			}
			return a.Start.Col - b.Start.Col
		})
		rows = append(rows, diagRow{header: true, path: file.Path, counts: countDiagnostics(items), source: file.Source})
		for _, d := range items {
			rows = append(rows, diagRow{path: file.Path, item: d, source: file.Source})
		}
	}
	return rows
}

// selectableDiagRows counts the rows a selection can land on: every row but the
// file headers.
func selectableDiagRows(rows []diagRow) int {
	n := 0
	for _, r := range rows {
		if !r.header {
			n++
		}
	}
	return n
}

// diagFlatIndex returns the index in rows of the sel'th selectable row, or -1.
func diagFlatIndex(rows []diagRow, sel int) int {
	if sel < 0 {
		return -1
	}
	n := 0
	for i, r := range rows {
		if r.header {
			continue
		}
		if n == sel {
			return i
		}
		n++
	}
	return -1
}

// clampDiagSelection keeps the selection on a diagnostic and the scroll such that
// the selected row is inside the body.
func (m *Model) clampDiagSelection() {
	rows := m.diagRows()
	n := selectableDiagRows(rows)
	switch {
	case n == 0:
		m.diag.sel, m.diag.scroll = 0, 0
		return
	case m.diag.sel >= n:
		m.diag.sel = n - 1
	case m.diag.sel < 0:
		m.diag.sel = 0
	}
	flat := diagFlatIndex(rows, m.diag.sel)
	height := m.diag.Rows()
	if flat < m.diag.scroll {
		m.diag.scroll = flat
	}
	if flat >= m.diag.scroll+height {
		m.diag.scroll = flat - height + 1
	}
	m.diag.scroll = min(max(m.diag.scroll, 0), max(len(rows)-height, 0))
}

// diagHeaderTitle is the section's header: "Diagnostics" and the workspace counts,
// else the first $/progress line (with " +N more"), or an ellipsis while the
// servers are still starting or nothing has arrived.
func (m *Model) diagHeaderTitle() string {
	if c := m.workspaceCounts(); c.Total() > 0 {
		return "Diagnostics " + c.Label()
	}
	if progress := m.progressSummary(); progress != "" {
		return "Diagnostics · " + progress
	}
	if m.diagWaiting() != "" {
		return "Diagnostics …"
	}
	return "Diagnostics"
}

// progressLine renders one $/progress entry as one line: the server, its title,
// the latest message and the percentage when the server gave one.
func progressLine(p LSPProgress) string {
	var b strings.Builder
	if p.Server != "" {
		b.WriteString(p.Server + ": ")
	}
	switch {
	case p.Title != "" && p.Message != "":
		b.WriteString(p.Title + " — " + p.Message)
	case p.Title != "":
		b.WriteString(p.Title)
	default:
		b.WriteString(p.Message)
	}
	if p.Percentage >= 0 {
		fmt.Fprintf(&b, " %d%%", p.Percentage)
	}
	return strings.TrimSpace(b.String())
}

// progressSummary is the one-line summary the header shows: the first unfinished
// progress entry and how many more there are. Empty when nothing is in progress.
func (m *Model) progressSummary() string {
	if len(m.lsp.progress) == 0 {
		return ""
	}
	summary := progressLine(m.lsp.progress[0])
	if extra := len(m.lsp.progress) - 1; extra > 0 {
		summary += fmt.Sprintf(" +%d more", extra)
	}
	return summary
}

// diagWaiting is what the panel says while it has no rows but diagnostics may
// still arrive: a server is starting, or the first publish/pull has not come back.
// "" means nothing is pending. It is the panel's progress indicator; the status
// row's LSP pill keeps its own starting state.
func (m *Model) diagWaiting() string {
	if m.lsp.port == nil {
		return ""
	}
	starting, open := false, false
	for _, tr := range m.lsp.tracks {
		switch tr.state {
		case lspOpening:
			starting = true
		case lspOpen:
			open = true
		}
	}
	switch {
	case starting:
		return "Starting language servers…"
	case open || m.lsp.polling:
		return "Waiting for diagnostics…"
	}
	return ""
}

// diagEmptyText is the panel's message when it has no rows. While a server reports
// progress, the first entry is the message: that is the panel's text progress
// indicator, and it clears when the server ends it.
func (m *Model) diagEmptyText() string {
	switch {
	case m.lsp.port == nil:
		return "No language server is configured"
	case m.lsp.stopped:
		return "Language servers are stopped"
	case len(m.lsp.progress) > 0:
		return "In progress: " + m.progressSummary()
	case m.diagWaiting() != "":
		return m.diagWaiting()
	}
	return "No diagnostics"
}

// paintDiagPanel paints the Diagnostics section's body: the file/diagnostic rows
// inside the column's walls, scrolling so the selection is visible, or the
// section's empty state.
func (m *Model) paintDiagPanel(dst *grid.Grid, l layout, sec sidebar.Section) {
	rows, right, x := sec.Rows(), l.ExX+l.Ex, l.ExX
	if l.Ex < 2 || rows <= 0 {
		return
	}
	body := style.TaggedCell(m.styles.Explorer, "editor", "fg", "editor", "bg")
	sel := style.TaggedCell(m.styles.Selection, "editor", "fg", "editor", "selection_bg")
	accent := style.Cell(m.styles.Accent).BG
	for y := sec.BodyTop; y <= sec.BodyBottom; y++ {
		dst.Fill(x, right, y, body)
		dst.Set(x, y, "│", body)
		dst.Set(right-1, y, "│", body)
	}
	m.clampDiagSelection()
	flat := m.diagRows()
	end := right - 1 - space.PadX
	if len(flat) == 0 {
		st := body
		st.Faint = true
		dst.Put(x+1+space.PadX, sec.BodyTop, end, m.diagEmptyText(), st)
		return
	}
	selected := diagFlatIndex(flat, m.diag.sel)
	for off := 0; off < rows; off++ {
		pos := m.diag.scroll + off
		if pos >= len(flat) {
			break
		}
		r := flat[pos]
		y := sec.BodyTop + off
		st := body
		if !r.header && pos == selected {
			st = sel
			dst.Fill(x+1, right-1, y, st)
		}
		if !r.header && pos == selected && m.diag.focus {
			bar := st
			bar.FG = accent
			dst.Set(x+1, y, "▌", bar)
		}
		nameX := x + 1 + space.PadX
		if r.header {
			name := m.diagRel(r.path) + diagOpenMarker(r.source)
			dst.Put(nameX, y, end, ansi.Truncate(name, max(end-nameX, 0), "…"), headerStyle(body))
			label := r.counts.Label()
			if label != "" {
				dst.Put(max(end-ansi.StringWidth(label), nameX), y, end, label, headerStyle(body))
			}
			continue
		}
		dst.Set(nameX, y, diagSeverityGlyph(r.item.Severity), diagSeverityStyle(body, r.item.Severity, m.styles))
		text := diagPanelText(r.item)
		textX, room := nameX+2, end
		if label := fmt.Sprintf("%d:%d", r.item.Start.Line+1, r.item.Start.Col+1); room-textX > len(label)+2 {
			dst.Put(max(end-ansi.StringWidth(label), textX), y, end, label, body)
			room -= len(label) + 1
		}
		dst.Put(textX, y, room, ansi.Truncate(text, max(room-textX, 0), "…"), st)
	}
}

// diagOpenMarker marks a file whose diagnostics came from a workspace pull rather
// than from the server publishing for an open document: it is usually a file the
// user has not opened, and Enter opens it.
func diagOpenMarker(source string) string {
	if source == "workspace" {
		return " (not open)"
	}
	return ""
}

// headerStyle is the style of a file header: the body style, bold.
func headerStyle(body grid.Style) grid.Style {
	body.Bold = true
	return body
}

// diagSeverityGlyph is the one-cell mark of a diagnostic's severity.
func diagSeverityGlyph(severity int) string {
	switch severity {
	case 1:
		return "✗"
	case 2:
		return "▲"
	case 3:
		return "●"
	case 4:
		return "○"
	}
	return "·"
}

// diagSeverityStyle colors a severity mark with the theme's diagnostic color.
func diagSeverityStyle(body grid.Style, severity int, styles style.Styles) grid.Style {
	var mark grid.Style
	switch severity {
	case 1:
		mark = style.TaggedCell(styles.DiagError, "diagnostic", "error_fg", "", "")
	case 2:
		mark = style.TaggedCell(styles.DiagWarning, "diagnostic", "warning_fg", "", "")
	case 3:
		mark = style.TaggedCell(styles.DiagInfo, "diagnostic", "info_fg", "", "")
	default:
		mark = style.TaggedCell(styles.DiagHint, "diagnostic", "hint_fg", "", "")
	}
	if mark.FG != nil {
		body.FG = mark.FG
	}
	return body
}

// diagPanelText is a diagnostic's message, with its source and code appended when
// the server gave them.
func diagPanelText(d Diagnostic) string {
	text := strings.TrimSpace(d.Message)
	if d.Code != "" {
		text += " [" + d.Code + "]"
	}
	if d.Source != "" {
		text += " (" + d.Source + ")"
	}
	return text
}

// diagRel returns path relative to the workspace root when it is inside it.
func (m *Model) diagRel(path string) string {
	return m.lspRel(path)
}

// takeDiagFocus shows the column and gives the Diagnostics section the keyboard.
func (m *Model) takeDiagFocus() {
	if !m.explorerOpen {
		m.explorerOpen = true
		m.syncViewport()
	}
	m.blurPluginPanels()
	m.tabFocus = false
	m.dock.SetFocus(false)
	m.explorerFocus = false
	m.contentsFocus = false
	m.contents.SetFocus(false)
	m.diag.focus = true
}

// focusDiagnostics opens the panel and gives it the keyboard, unfolding it first:
// the keyboard does not go to a section with no rows on screen.
func (m *Model) focusDiagnostics() {
	m.diag.open = true
	m.diag.collapsed = false
	m.takeDiagFocus()
	m.clampDiagSelection()
}

// diagKey handles a key while the Diagnostics section has the keyboard. Up and Down
// move the selection, PageUp and PageDown a page, Home and End its ends, Enter
// opens the selected diagnostic, Escape gives the keyboard back to the editor.
// The other application-wide actions keep their Normal bindings; typed text never
// reaches the buffer.
func (m *Model) diagKey(kp KeyPress, _ string) tea.Cmd {
	d := &m.diag
	if d.collapsed {
		switch kp.Name {
		case "Enter", " ":
			d.collapsed = false
			return nil
		case "Escape":
			m.focusEditor()
			return nil
		}
		return m.sidebarFallthroughKey(kp)
	}
	if len(kp.Mods) == 0 {
		switch kp.Name {
		case "Up":
			d.sel--
			m.clampDiagSelection()
			return nil
		case "Down":
			d.sel++
			m.clampDiagSelection()
			return nil
		case "PageUp":
			d.sel -= d.Rows()
			m.clampDiagSelection()
			return nil
		case "PageDown":
			d.sel += d.Rows()
			m.clampDiagSelection()
			return nil
		case "Home":
			d.sel = 0
			m.clampDiagSelection()
			return nil
		case "End":
			d.sel = selectableDiagRows(m.diagRows()) - 1
			m.clampDiagSelection()
			return nil
		case "Enter":
			cmd := m.diagJump()
			if cmd != nil {
				m.focusEditor()
			}
			return cmd
		case "Escape":
			m.focusEditor()
			return nil
		}
	}
	return m.sidebarFallthroughKey(kp)
}

// diagJump opens the selected diagnostic's file at its position, through the same
// jump the other LSP go-tos use.
func (m *Model) diagJump() tea.Cmd {
	rows := m.diagRows()
	flat := diagFlatIndex(rows, m.diag.sel)
	if flat < 0 {
		return nil
	}
	r := rows[flat]
	return m.lspGoto(LSPLocation{Path: r.path, Start: r.item.Start, End: r.item.End})
}

// pressDiagHeader is a left press on the Diagnostics header: it takes the keyboard
// and folds or unfolds the section.
// pressDiagRow is a left press on a row of the Diagnostics body: it selects the row
// and, for a diagnostic, jumps to it. A header row only selects nothing.
func (m *Model) pressDiagRow(row int) tea.Cmd {
	m.takeDiagFocus()
	rows := m.diagRows()
	pos := m.diag.scroll + row
	if pos < 0 || pos >= len(rows) {
		return nil
	}
	if rows[pos].header {
		return nil
	}
	// Translate the flat row into the selectable index.
	sel := 0
	for i := range pos {
		if !rows[i].header {
			sel++
		}
	}
	m.diag.sel = sel
	m.clampDiagSelection()
	return m.diagJump()
}

// toggleDiagnosticsPanel is the palette action: it shows the Diagnostics section
// with the keyboard, or closes it and gives the keyboard back to the editor.
func (m *Model) toggleDiagnosticsPanel() tea.Cmd {
	if m.diag.open {
		m.diag.open = false
		m.focusEditor()
		return nil
	}
	m.focusDiagnostics()
	return m.lspWorkspacePoll()
}

// openDiagnosticsList is the quickfix list: the workspace's diagnostics in the
// shared list overlay, grouped by file, filterable, Enter jumps and keeps the list
// open as a fix list does.
func (m *Model) openDiagnosticsList() tea.Cmd {
	rows := m.diagRows()
	if len(rows) == 0 {
		m.notice = m.diagEmptyText()
		return m.lspWorkspacePoll()
	}
	items := make([]list.Row, 0, len(rows))
	for _, r := range rows {
		if r.header {
			items = append(items, list.Row{Header: true, Left: m.diagRel(r.path) + diagOpenMarker(r.source), Right: r.counts.Label()})
			continue
		}
		items = append(items, list.Row{
			Left:  fmt.Sprintf("%d:%d  %s", r.item.Start.Line+1, r.item.Start.Col+1, diagPanelText(r.item)),
			Value: diagListValue(r.path, r.item.Start),
		})
	}
	v := list.New("Diagnostics", items, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		if r.Value == "" {
			return nil
		}
		path, at, ok := parseDiagListValue(r.Value)
		if !ok {
			return nil
		}
		// No Close: a fix list stays open after a jump.
		return m.lspGoto(LSPLocation{Path: path, Start: at, End: at})
	})
	m.pushOverlay(v)
	return nil
}

// diagListValue encodes a diagnostic as the quickfix list row's value: the path,
// the zero-based line and the rune column, tab separated (a path cannot hold a tab
// after the root normalises it).
func diagListValue(path string, at buffer.Pos) string {
	return path + "\t" + strconv.Itoa(at.Line) + "\t" + strconv.Itoa(at.Col)
}

// parseDiagListValue decodes diagListValue.
func parseDiagListValue(value string) (string, buffer.Pos, bool) {
	parts := strings.Split(value, "\t")
	if len(parts) != 3 {
		return "", buffer.Pos{}, false
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", buffer.Pos{}, false
	}
	col, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", buffer.Pos{}, false
	}
	return parts[0], buffer.Pos{Line: line, Col: col}, true
}
