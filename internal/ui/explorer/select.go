package explorer

import (
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/fuzzy"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// transient is the explorer state beyond the cursor: the rows picked with Space
// or a range extend, the anchor a range extend started from, the rows on a
// pending cut, and the type-to-search text. The panel holds it by pointer, so
// the copies of the panel that the root keeps see one state.
type transient struct {
	picked map[string]bool // paths picked with Space or by a range extend
	anchor string          // path a range extend started from; empty when none
	cut    map[string]bool // paths on a pending cut, drawn faint
	query  string          // the type-to-search text
}

// state returns the panel's transient state. A panel that was not made by New
// has none, so it gets an empty one that nothing keeps.
func (p Panel) state() *transient {
	if p.st == nil {
		return &transient{}
	}
	return p.st
}

// ClearTransient is the Esc step of the explorer. Each call clears one thing, in
// the order Fresh uses: the pending cut, then the picked rows, then the search
// text. It returns true when it cleared something. When it returns false the
// root moves focus to the editor. The root also drops its own cut clipboard once
// CutPending reports false.
func (p Panel) ClearTransient() bool {
	s := p.state()
	switch {
	case len(s.cut) > 0:
		s.cut = nil
	case len(s.picked) > 0:
		s.picked, s.anchor = nil, ""
	case s.query != "":
		s.query = ""
	default:
		return false
	}
	return true
}

// CutPending reports whether a cut is waiting for a paste. A copy ends it, and so
// does ClearTransient. A paste leaves it pending: the root drops each path from it as
// the paste moves that entry.
func (p Panel) CutPending() bool { return len(p.state().cut) > 0 }

// CarryCut returns p with the pending cut of prev. The root builds a new panel when
// it hides the explorer, and the cut dimming has to outlive that, as the root's
// clipboard does.
func (p Panel) CarryCut(prev Panel) Panel {
	p.state().cut = maps.Clone(prev.state().cut)
	return p
}

// DropCut takes path, and every path under it, out of the pending cut, so no row
// stays dimmed for an entry that the clipboard has let go.
func (p Panel) DropCut(path string) {
	s := p.state()
	for q := range s.cut {
		if Within(q, path) {
			delete(s.cut, q)
		}
	}
}

// DropPicked takes path, and every path under it, out of the picked rows, and clears
// the range anchor when it is one of them. It runs when the entry is trashed, renamed
// or moved away, so no row stays picked for an entry that has gone from its path.
func (p Panel) DropPicked(path string) {
	s := p.state()
	for q := range s.picked {
		if Within(q, path) {
			delete(s.picked, q)
		}
	}
	if Within(s.anchor, path) {
		s.anchor = ""
	}
}

// TypeSearch appends r to the search text and selects the first visible row whose
// name matches the whole text. The match is the palette's fuzzy match: the letters
// must appear in order, not next to each other.
func (p Panel) TypeSearch(r rune, g Geometry) {
	if p.tree == nil {
		return
	}
	s := p.state()
	s.query += string(r)
	p.searchFirst(g)
}

// SearchBackspace removes the last character of the search text and selects the
// first match of what is left. With no search text it does nothing.
func (p Panel) SearchBackspace(g Geometry) {
	s := p.state()
	if p.tree == nil || s.query == "" {
		return
	}
	r := []rune(s.query)
	s.query = string(r[:len(r)-1])
	p.searchFirst(g)
}

// ClearSearch empties the search text. The selection stays where it is.
func (p Panel) ClearSearch() { p.state().query = "" }

// searchFirst selects the first visible row that matches the search text and
// keeps it in view. The selection stays put when the text is empty or nothing
// matches.
func (p Panel) searchFirst(g Geometry) {
	query := p.state().query
	if query == "" {
		return
	}
	pat := fuzzy.NewPattern(query)
	rows := p.tree.Rows()
	for i, r := range rows {
		if _, _, ok := pat.Match(r.Name); ok {
			p.selectIndex(rows, i)
			p.follow(g)
			return
		}
	}
}

// moveCursor moves the cursor by delta rows for Up or Down. It ends any range
// run, and returns the preview of the file it lands on.
func (p Panel) moveCursor(delta int) tea.Cmd {
	p.state().anchor = ""
	p.tree.Move(delta)
	return p.previewSelected()
}

// previewSelected returns the preview of the selected file, which is the command
// a single click sends. A directory has no preview.
func (p Panel) previewSelected() tea.Cmd {
	sel, ok := p.tree.Selected()
	if !ok || sel.IsDir {
		return nil
	}
	return previewCmd(sel.Path)
}

// previewCmd returns the command that previews the file at path in a preview tab.
// A single click and the Up and Down keys send it.
func previewCmd(path string) tea.Cmd {
	return emit(OpenFileMsg{Path: path, Preview: true})
}

// togglePick picks the cursor row, or drops it when it is picked. The row becomes
// the anchor of a later range extend.
func (p Panel) togglePick() {
	sel, ok := p.tree.Selected()
	if !ok {
		return
	}
	s := p.state()
	if s.picked[sel.Path] {
		delete(s.picked, sel.Path)
	} else {
		if s.picked == nil {
			s.picked = make(map[string]bool)
		}
		s.picked[sel.Path] = true
	}
	s.anchor = sel.Path
}

// extend moves the cursor by delta rows and picks every row from the anchor to
// the cursor, in place of the picks it had. The anchor is where the run of extends
// began: the cursor row, or the last row picked. A plain Up or Down ends the run.
func (p Panel) extend(delta int) {
	sel, ok := p.tree.Selected()
	if !ok {
		return
	}
	s := p.state()
	if s.anchor == "" {
		s.anchor = sel.Path
	}
	p.tree.Move(delta)
	rows := p.tree.Rows()
	cur, _ := p.tree.Selected()
	ci := slices.IndexFunc(rows, func(r tree.Row) bool { return r.Path == cur.Path })
	ai := slices.IndexFunc(rows, func(r tree.Row) bool { return r.Path == s.anchor })
	if ai < 0 {
		// The anchor is no longer visible, as when its ancestor collapsed.
		s.anchor, ai = cur.Path, ci
	}
	lo, hi := min(ai, ci), max(ai, ci)
	s.picked = make(map[string]bool, hi-lo+1)
	for _, r := range rows[lo : hi+1] {
		s.picked[r.Path] = true
	}
}

// selectAll picks every visible row and ends any range run.
func (p Panel) selectAll() {
	rows := p.tree.Rows()
	s := p.state()
	s.picked = make(map[string]bool, len(rows))
	for _, r := range rows {
		s.picked[r.Path] = true
	}
	s.anchor = ""
}

// targets returns the paths a copy or a cut acts on, in display order: the
// visible picked rows, or the cursor row when no visible row is picked.
func (p Panel) targets() []string {
	var out []string
	picked := p.state().picked
	for _, r := range p.tree.Rows() {
		if picked[r.Path] {
			out = append(out, r.Path)
		}
	}
	if len(out) == 0 {
		if sel, ok := p.tree.Selected(); ok {
			out = append(out, sel.Path)
		}
	}
	return out
}

// cutCmd cuts the targets: it dims them and sends one CutMsg that carries all of
// them. A cut that holds the tree root is refused and dims nothing. With no target
// it does nothing.
func (p Panel) cutCmd() tea.Cmd {
	paths := p.targets()
	switch {
	case len(paths) == 0:
		return nil
	case slices.Contains(paths, p.tree.Root()):
		return rootRefused("cut")
	}
	p.state().cut = pathSet(paths)
	return emit(clipboardMsg(paths, true))
}

// pathSet returns the paths in paths as a set.
func pathSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = true
	}
	return out
}

// rowStyle is the style of one tree row: the body style, the selection background
// on a picked row, and faint text on a row under a pending cut.
func (p Panel) rowStyle(r tree.Row, body grid.Style) grid.Style {
	s := p.state()
	st := body
	if s.picked[r.Path] {
		st = style.Cell(p.styles.Selection)
	}
	if s.cut[r.Path] {
		st.Faint = true
	}
	return st
}
