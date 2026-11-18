package tui

import (
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/space"
)

// mouseHoverState tracks the debounced LSP hover timer when ViewOptions.MouseHover is enabled.
type mouseHoverState struct {
	seq         uint64
	pane        int
	pos         buffer.Pos
	x, y        int
	armed       bool
	active      bool
	pendingTick tea.Cmd
}

// mouseHoverTickMsg fires when the pointer rests on an editor cell for MouseHoverDelayMS.
type mouseHoverTickMsg struct {
	seq  uint64
	pane int
	pos  buffer.Pos
}

// hoverDelay returns the configured mouse hover delay (default 500ms).
func (m *Model) hoverDelay() time.Duration {
	if m.view.opts.MouseHoverDelayMS > 0 {
		return time.Duration(m.view.opts.MouseHoverDelayMS) * time.Millisecond
	}
	return doubleClickWindow
}

// cancelMouseHover disarms any pending hover tick so stale motions cannot pop a tip.
func (m *Model) cancelMouseHover() {
	m.hover.seq++
	m.hover.armed = false
	m.hover.active = false
}

// armMouseHover starts or restarts the debounced LSP hover timer when the pointer
// moves over editor text while mouse hover is enabled.
func (m *Model) armMouseHover(mo tea.Mouse) tea.Cmd {
	if !m.view.mouseHover || !m.view.mouseCapture || mo.Button != tea.MouseNone ||
		m.drag.active || m.divider.active || len(m.overlays) > 0 || m.menu.IsOpen() || m.welcomeVisible() || m.lsp.port == nil {
		m.cancelMouseHover()
		return nil
	}
	l := m.layout()
	t := l.Hit(mo.X, mo.Y)
	if t.Kind != frame.HitEditorText {
		m.cancelMouseHover()
		return nil
	}
	if t.Pane < 0 || t.Pane >= len(l.Panes) {
		m.cancelMouseHover()
		return nil
	}
	sp := l.Panes[t.Pane]
	if doc := m.paneDoc(sp); doc != nil && mo.Y == sp.Y {
		if _, hasSticky := m.stickyScrollLine(sp, doc); hasSticky {
			m.cancelMouseHover()
			return nil
		}
	}
	p, hit, _, ok := m.paneHitIn(t.Pane, mo.X, mo.Y)
	if !ok || hit.Kind != editorpane.HitText || hit.Below > 0 || hit.Virtual > 0 || !p.HasTextAt(sp.W, sp.H, mo.X-sp.X, mo.Y-sp.Y) {
		m.cancelMouseHover()
		return nil
	}
	if m.hover.armed && m.hover.pane == t.Pane && m.hover.pos == hit.Pos {
		m.hover.x, m.hover.y = mo.X, mo.Y
		return nil
	}
	m.hover.seq++
	seq := m.hover.seq
	m.hover.pane = t.Pane
	m.hover.pos = hit.Pos
	m.hover.x, m.hover.y = mo.X, mo.Y
	m.hover.armed = true
	m.hover.active = false
	return tea.Tick(m.hoverDelay(), func(time.Time) tea.Msg {
		return mouseHoverTickMsg{seq: seq, pane: t.Pane, pos: hit.Pos}
	})
}

// triggerMouseHover requests LSP hover info if the armed timer is still current.
func (m *Model) triggerMouseHover(msg mouseHoverTickMsg) tea.Cmd {
	if !m.hover.armed || msg.seq != m.hover.seq || !m.view.mouseHover ||
		len(m.overlays) > 0 || m.menu.IsOpen() || m.lsp.port == nil {
		return nil
	}
	m.hover.armed = false
	l := m.layout()
	if msg.pane < 0 || msg.pane >= len(l.Panes) {
		return nil
	}
	doc := m.paneDoc(l.Panes[msg.pane])
	if doc == nil {
		return nil
	}
	path := docPath(doc)
	if path == "" || isLargeDoc(doc) {
		return nil
	}
	lang := m.lspLanguage(doc)
	if !m.lsp.enabled(lang) {
		return nil
	}
	tr := m.lsp.tracks[path]
	if tr == nil || tr.state != lspOpen {
		return nil
	}
	m.hover.active = true
	return m.lspRequest(lspAskMouseHover, path, msg.pos, "")
}

// lspHoverSilent shows a hover popup only when there is diagnostic or symbol hover content,
// without displaying a "No hover information" notice when moving over plain whitespace.
func (m *Model) lspHoverSilent(path string, at buffer.Pos, text string) {
	if !m.view.mouseHover || !m.view.mouseCapture || len(m.overlays) > 0 || m.menu.IsOpen() || (m.hover.seq > 0 && !m.hover.active) {
		return
	}
	diags := m.diagsAt(path, at)
	if strings.TrimSpace(text) == "" && len(diags) == 0 {
		return
	}
	var items []popup.Item
	width := popup.TextWidth(m.width)
	add := func(line string) {
		for _, row := range space.Wrap(line, width, len(line)+1) {
			items = append(items, popup.Item{Label: row})
		}
	}
	for _, d := range diags {
		for i, line := range strings.Split(strings.TrimSpace(d.Message), "\n") {
			if i == 0 {
				line = diagText(Diagnostic{Severity: d.Severity, Source: d.Source, Message: line})
			}
			add(line)
		}
	}
	if strings.TrimSpace(text) != "" {
		if len(diags) > 0 {
			items = append(items, popup.Item{Label: ""})
		}
		for line := range strings.SplitSeq(text, "\n") {
			add(line)
		}
	}
	if len(items) == 0 {
		return
	}
	var anchor popup.Point
	if m.hover.x > 0 || m.hover.y > 0 {
		anchor = popup.Point{X: m.hover.x, Y: m.hover.y}
	} else if x, y, ok := m.caret(); ok {
		anchor = popup.Point{X: x, Y: y}
	}
	m.hover.active = false
	m.lspPopupAt(anchor, uiText("gvim.lsp.popup_hover", "Hover"), items, 0)
}

// urlAtRuneCol returns a valid http(s) URL spanning rune offset col on line.
func urlAtRuneCol(line string, col int) (string, bool) {
	if line == "" || col < 0 {
		return "", false
	}
	runes := []rune(line)
	if col >= len(runes) {
		return "", false
	}
	for i := 0; i < len(runes); {
		if !hasURLSchemeAt(runes, i) {
			i++
			continue
		}
		end := scanURLSpanEnd(runes, i)
		if col >= i && col < end {
			cand := string(runes[i:end])
			if validateOpenURL(cand) == nil {
				return cand, true
			}
		}
		i = max(end, i+1)
	}
	return "", false
}

func hasURLSchemeAt(runes []rune, i int) bool {
	return matchRunesFold(runes[i:], "https://") || matchRunesFold(runes[i:], "http://")
}

func matchRunesFold(runes []rune, prefix string) bool {
	if len(runes) < len(prefix) {
		return false
	}
	for i := range len(prefix) {
		if unicode.ToLower(runes[i]) != rune(prefix[i]) {
			return false
		}
	}
	return true
}

func scanURLSpanEnd(runes []rune, start int) int {
	parens := 0
	end := start
	for end < len(runes) {
		r := runes[end]
		if unicode.IsSpace(r) || strings.ContainsRune(`<>"'[]{}`, r) {
			break
		}
		if r == '(' {
			parens++
		} else if r == ')' {
			if parens == 0 {
				break
			}
			parens--
		}
		end++
	}
	for end > start {
		last := runes[end-1]
		if strings.ContainsRune(".,;:!?", last) {
			end--
			continue
		}
		break
	}
	return end
}

// isMarkdownDocument reports whether doc holds Markdown text (by language ID or file extension).
func (m *Model) isMarkdownDocument(doc Document) bool {
	if doc == nil {
		return false
	}
	if m.lspLanguage(doc) == "markdown" {
		return true
	}
	switch strings.ToLower(filepath.Ext(doc.Name())) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// toggleMarkdownCheckboxAt toggles a task list marker "[ ]" <-> "[x]" when pos clicks on
// the checkbox in a writable Markdown document.
func (m *Model) toggleMarkdownCheckboxAt(pos buffer.Pos) bool {
	doc := m.ed.Active()
	if doc == nil || m.Holds("read_only") || !m.isMarkdownDocument(doc) {
		return false
	}
	rr, ok := m.ed.(rangeReplacer)
	if !ok {
		return false
	}
	if pos.Line < 0 || pos.Line >= doc.LineCount() {
		return false
	}
	runes := []rune(doc.Line(pos.Line))
	boxCol, state, ok := markdownTaskBoxCol(runes)
	if !ok || pos.Col < boxCol || pos.Col >= boxCol+3 {
		return false
	}
	next := "x"
	if state == 'x' || state == 'X' {
		next = " "
	}
	return rr.ReplaceActiveRange(
		buffer.Pos{Line: pos.Line, Col: boxCol + 1},
		buffer.Pos{Line: pos.Line, Col: boxCol + 2},
		next,
	)
}

// markdownTaskBoxCol locates a leading Markdown task checkbox (`- [ ]`, `* [x]`, `1. [ ]`)
// and returns the rune index of '[', the inner check rune, and true.
func markdownTaskBoxCol(runes []rune) (boxCol int, state rune, ok bool) {
	i := 0
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	if i >= len(runes) {
		return 0, 0, false
	}
	switch {
	case runes[i] == '-' || runes[i] == '*' || runes[i] == '+':
		i++
	case runes[i] >= '0' && runes[i] <= '9':
		for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
		if i >= len(runes) || (runes[i] != '.' && runes[i] != ')') {
			return 0, 0, false
		}
		i++
	default:
		return 0, 0, false
	}
	if i >= len(runes) || (runes[i] != ' ' && runes[i] != '\t') {
		return 0, 0, false
	}
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	if i+2 >= len(runes) || runes[i] != '[' || runes[i+2] != ']' {
		return 0, 0, false
	}
	ch := runes[i+1]
	if ch != ' ' && ch != 'x' && ch != 'X' {
		return 0, 0, false
	}
	if i+3 < len(runes) && runes[i+3] != ' ' && runes[i+3] != '\t' {
		return 0, 0, false
	}
	return i, ch, true
}
