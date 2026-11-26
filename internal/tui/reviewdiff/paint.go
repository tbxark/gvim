package reviewdiff

import (
	"fmt"
	"image/color"
	"path"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/textsafe"
	"github.com/tbxark/gvim/syntax"
)

// SidebarRect describes the Changes sidebar geometry in frame coordinates.
type SidebarRect struct {
	X, W   int
	Top    int // header row (┌ Changes ... ┐)
	Bottom int // bottom border row (└─────────┘)
	Right  bool
}

// FilterY is the screen row of the filter bar inside the sidebar.
func (r SidebarRect) FilterY() int { return r.Top + 1 }

// ListTop is the first file row inside the sidebar.
func (r SidebarRect) ListTop() int { return r.Top + 2 }

// ListRows returns the number of visible file rows inside the sidebar.
func (r SidebarRect) ListRows() int {
	return max(r.Bottom-r.ListTop(), 0)
}

// DiffRect describes the main diff viewport geometry in frame coordinates.
type DiffRect struct {
	X, Y, W, H int
}

// HeaderAction identifies a clickable control in the 1-row Diff header bar.
type HeaderAction uint8

const (
	HeaderNone HeaderAction = iota
	HeaderBaseRef
	HeaderViewMode
	HeaderFullFile
	HeaderWrap
	HeaderEdit
)

// HeaderButton is a placed clickable control on the Diff header bar.
type HeaderButton struct {
	X, W   int
	Action HeaderAction
	Label  string
}

// Covers reports whether column x falls inside the button.
func (b HeaderButton) Covers(x int) bool { return x >= b.X && x < b.X+b.W }

// HeaderLayout records the placed controls of the 1-row Diff header bar so
// painting and mouse hit-testing always agree.
type HeaderLayout struct {
	Y       int
	Buttons []HeaderButton
}

// Hit returns the HeaderAction at (x, y), or HeaderNone.
func (h HeaderLayout) Hit(x, y int) HeaderAction {
	if y != h.Y {
		return HeaderNone
	}
	for _, b := range h.Buttons {
		if b.Covers(x) {
			return b.Action
		}
	}
	return HeaderNone
}

// SidebarHitKind classifies a pointer target inside the Changes sidebar.
type SidebarHitKind uint8

const (
	SidebarHitNone SidebarHitKind = iota
	SidebarHitReload
	SidebarHitClose
	SidebarHitGrip
	SidebarHitFilter
	SidebarHitFile
	SidebarHitBody
)

// SidebarHit is the hit-test result for the Changes sidebar.
type SidebarHit struct {
	Kind        SidebarHitKind
	FilteredIdx int // valid when Kind == SidebarHitFile
}

// DiffHitKind classifies a pointer target inside the Diff pane.
type DiffHitKind uint8

const (
	DiffHitNone DiffHitKind = iota
	DiffHitRow
	DiffHitExpand
	DiffHitScrollbar
	DiffHitSearchBar
)

// DiffHit is the hit-test result for the Diff pane.
type DiffHit struct {
	Kind      DiffHitKind
	RowIdx    int // logical row index for DiffHitRow / DiffHitExpand
	GapID     int // gap id for DiffHitExpand
	ScrollRow int // 0-based row in track for DiffHitScrollbar
}

// SidebarHeader returns the computed sidebar.Header for the Changes column.
func (m *Model) SidebarHeader(width int, iconMode icons.Mode) sidebar.Header {
	title := fmt.Sprintf("Changes (%d)", len(m.Files))
	if m.LoadingFiles && len(m.Files) == 0 {
		title = "Changes"
	}
	return sidebar.HeaderFor(width, iconMode, sidebar.HeaderSpec{
		Kind:  sidebar.KindExplorer,
		First: true,
		Title: title,
	})
}

// HitSidebar resolves a frame cell (x, y) inside the Changes sidebar.
func (m *Model) HitSidebar(r SidebarRect, iconMode icons.Mode, x, y int) SidebarHit {
	if r.W <= 0 || x < r.X || x >= r.X+r.W || y < r.Top || y > r.Bottom {
		return SidebarHit{}
	}
	if y == r.Top {
		h := m.SidebarHeader(r.W, iconMode)
		if h.Reload >= 0 && x == r.X+h.Reload {
			return SidebarHit{Kind: SidebarHitReload}
		}
		if h.Close >= 0 && x == r.X+h.Close {
			return SidebarHit{Kind: SidebarHitClose}
		}
		return SidebarHit{Kind: SidebarHitBody}
	}
	if y == r.Bottom {
		return SidebarHit{Kind: SidebarHitBody}
	}
	grip := r.X + r.W - 1
	if r.Right {
		grip = r.X
	}
	if x == grip {
		return SidebarHit{Kind: SidebarHitGrip}
	}
	if y == r.FilterY() {
		return SidebarHit{Kind: SidebarHitFilter}
	}
	if row := y - r.ListTop(); row >= 0 && row < r.ListRows() {
		idx := m.FileScroll + row
		if idx >= 0 && idx < len(m.Filtered) {
			return SidebarHit{Kind: SidebarHitFile, FilteredIdx: idx}
		}
	}
	return SidebarHit{Kind: SidebarHitBody}
}

// PaintSidebar draws the Changes sidebar (header, filter input, file list,
// scrollbar, and borders) into g.
func (m *Model) PaintSidebar(g *grid.Grid, r SidebarRect, st Styles, iconMode icons.Mode) {
	if r.W <= 0 || r.Bottom < r.Top {
		return
	}
	h := m.SidebarHeader(r.W, iconMode)
	sidebar.PaintHeader(g, r.X, r.W, r.Top, h, st.Chrome)

	// Fill body and side walls.
	wall := st.Text
	for y := r.Top + 1; y < r.Bottom; y++ {
		g.Fill(r.X, r.X+r.W, y, st.Text)
		g.Set(r.X, y, "│", wall)
		if r.W > 1 {
			g.Set(r.X+r.W-1, y, "│", wall)
		}
	}
	sidebar.PaintBottomBorder(g, r.X, r.W, r.Bottom, st.Chrome)

	innerX := r.X + 1
	innerW := r.W - 2
	if innerW <= 0 || r.Bottom <= r.Top+1 {
		return
	}

	// Row 1 inside box: filter bar.
	fy := r.FilterY()
	if fy < r.Bottom {
		m.paintFilterRow(g, innerX, fy, innerW, st)
	}

	listRows := r.ListRows()
	if listRows <= 0 {
		return
	}
	m.ensureFileVisible(listRows)

	// Empty / loading / error states.
	if len(m.Filtered) == 0 {
		msg := "No changes"
		msgStyle := st.Muted
		switch {
		case m.LoadingFiles:
			msg = "Loading changes…"
		case m.NotRepo:
			msg = "Not a git repository"
		case m.FilesErr != "":
			msg = m.FilesErr
			msgStyle = st.Chrome.Overlay.Error
			msgStyle.BG = st.Text.BG
		case m.FilterQuery != "" && len(m.Files) > 0:
			msg = "No matching files"
		}
		g.Put(innerX+1, r.ListTop(), innerX+innerW, space.End(msg, max(innerW-1, 0)), msgStyle)
		return
	}

	hasScroll := len(m.Filtered) > listRows && innerW > 4
	rowW := innerW
	if hasScroll {
		rowW = innerW - 1
	}

	for row := range listRows {
		idx := m.FileScroll + row
		if idx >= len(m.Filtered) {
			break
		}
		y := r.ListTop() + row
		f := m.Files[m.Filtered[idx]]
		selected := idx == m.Selected
		m.paintFileRow(g, innerX, y, rowW, f, selected, m.Focus == FocusFiles && !m.FilterActive, st)
	}

	if hasScroll {
		thumbTop, thumbLen := scrollbarThumb(len(m.Filtered), listRows, m.FileScroll)
		sx := innerX + innerW - 1
		for row := range listRows {
			y := r.ListTop() + row
			if row >= thumbTop && row < thumbTop+thumbLen {
				g.Set(sx, y, "┃", st.ScrollbarThumb)
			} else {
				g.Set(sx, y, "│", st.Scrollbar)
			}
		}
	}
}

func (m *Model) paintFilterRow(g *grid.Grid, x, y, w int, st Styles) {
	if m.FilterActive {
		g.Fill(x, x+w, y, st.InputField)
		prefix := " / "
		px := x + g.Put(x, y, x+w, prefix, st.InputField)
		avail := x + w - px
		if avail <= 0 {
			return
		}
		q := textsafe.Clean(m.FilterQuery)
		// Keep the tail of the query visible while typing.
		shown := q
		if ansi.StringWidth(shown) >= avail {
			shown = ansi.TruncateLeft(shown, ansi.StringWidth(shown)-avail+1, "")
		}
		cx := px + g.Put(px, y, x+w, shown, st.InputField)
		if cx < x+w {
			g.Set(cx, y, " ", st.InputCaret)
		}
		return
	}
	if m.FilterQuery != "" {
		row := st.SelectionBlur
		g.Fill(x, x+w, y, row)
		label := " / " + textsafe.Clean(m.FilterQuery)
		g.Put(x, y, x+w, space.End(label, w), row)
		return
	}
	g.Put(x, y, x+w, space.End(" / Filter files…", w), st.Muted)
}

func (m *Model) paintFileRow(g *grid.Grid, x, y, w int, f FileEntry, selected, focused bool, st Styles) {
	base := st.Text
	if selected {
		if focused {
			base = st.Selection
		} else {
			base = st.SelectionBlur
		}
		g.Fill(x, x+w, y, base)
	}

	curX := x
	if selected && focused {
		marker := base
		marker.Bold = true
		curX += g.Put(curX, y, x+w, "▌", marker)
	} else {
		curX += g.Put(curX, y, x+w, " ", base)
	}

	badgeStyle := base
	if !selected || !focused {
		badgeStyle.FG = st.StatusFG(f.Status)
	}
	badgeStyle.Bold = true
	curX += g.Put(curX, y, x+w, f.Status.Badge()+" ", badgeStyle)

	// Build compact right-aligned diffstat when space permits.
	var addStr, delStr string
	statW := 0
	if !f.Binary && !f.StatOnly && (f.Added > 0 || f.Deleted > 0) {
		if f.Added > 0 {
			addStr = "+" + strconv.Itoa(f.Added)
		}
		if f.Deleted > 0 {
			delStr = "-" + strconv.Itoa(f.Deleted)
		}
		statW = ansi.StringWidth(addStr) + ansi.StringWidth(delStr) + 1
		if addStr != "" && delStr != "" {
			statW++ // space between +N and -M
		}
	} else if f.Binary {
		addStr = "bin"
		statW = 4
	}

	avail := x + w - curX
	showStat := statW > 0 && avail-statW >= 10
	nameRoom := avail
	if showStat {
		nameRoom = avail - statW
	}
	if nameRoom > 0 {
		dispPath := textsafe.Clean(f.Path)
		baseName := path.Base(dispPath)
		dirName := path.Dir(dispPath)
		if dirName == "." || dirName == "/" {
			dirName = ""
		}
		if dirName == "" || ansi.StringWidth(dispPath) <= nameRoom {
			// Full path fits: draw dir muted, basename regular/bold.
			if dirName != "" {
				dirPrefix := dirName + "/"
				dirStyle := base
				if !selected || !focused {
					dirStyle.Faint = true
				}
				nx := curX + g.Put(curX, y, curX+nameRoom, dirPrefix, dirStyle)
				g.Put(nx, y, curX+nameRoom, baseName, base)
			} else {
				g.Put(curX, y, curX+nameRoom, dispPath, base)
			}
		} else if bw := ansi.StringWidth(baseName); bw <= nameRoom {
			// Prioritize keeping the full filename visible, truncating the directory.
			if rem := nameRoom - bw - 1; rem >= 3 {
				dirPart := space.End(dirName, rem) + "/"
				dirStyle := base
				if !selected || !focused {
					dirStyle.Faint = true
				}
				nx := curX + g.Put(curX, y, curX+nameRoom, dirPart, dirStyle)
				g.Put(nx, y, curX+nameRoom, baseName, base)
			} else {
				g.Put(curX, y, curX+nameRoom, baseName, base)
			}
		} else {
			g.Put(curX, y, curX+nameRoom, space.Middle(dispPath, nameRoom), base)
		}
	}

	if showStat {
		sx := x + w - statW + 1
		if f.Binary {
			bs := base
			bs.Faint = true
			g.Put(sx, y, x+w, addStr, bs)
			return
		}
		if addStr != "" {
			as := base
			if !selected || !focused {
				as.FG = st.AddFG
			}
			sx += g.Put(sx, y, x+w, addStr, as)
			if delStr != "" {
				sx += g.Put(sx, y, x+w, " ", base)
			}
		}
		if delStr != "" {
			ds := base
			if !selected || !focused {
				ds.FG = st.RemoveFG
			}
			g.Put(sx, y, x+w, delStr, ds)
		}
	}
}

// PaintHeaderBar draws the 1-row Diff header and action bar in the Tab Strip
// slot above the diff pane, and returns its exact button hit layout.
func (m *Model) PaintHeaderBar(g *grid.Grid, x, y, w int, st Styles, _ icons.Mode) HeaderLayout {
	hl := HeaderLayout{Y: y}
	if w <= 0 || y < 0 {
		return hl
	}
	g.Fill(x, x+w, y, st.HeaderBar)

	// Right-aligned clickable action pills, priority ordered from most important.
	baseLabel := " vs " + textsafe.Clean(m.BaseRef) + " ▾ "
	viewLabel := " " + m.EffectiveViewMode(w).Label() + " "
	ctxLabel := " Hunks "
	if m.FullFile {
		ctxLabel = " Full "
	}
	wrapLabel := " Wrap "
	if !m.Wrap {
		wrapLabel = " NoWrap "
	}
	editLabel := " Edit "

	type candidate struct {
		action HeaderAction
		label  string
	}
	candidates := []candidate{
		{HeaderEdit, editLabel},
		{HeaderViewMode, viewLabel},
		{HeaderBaseRef, baseLabel},
		{HeaderFullFile, ctxLabel},
	}
	if m.EffectiveViewMode(w) == ViewUnified {
		candidates = append(candidates, candidate{HeaderWrap, wrapLabel})
	}

	// Reserve at least 16 cells on the left for the filename when possible.
	minTitle := min(16, max(w/3, 4))
	rightEdge := x + w
	var placed []HeaderButton
	for _, c := range candidates {
		bw := ansi.StringWidth(c.label)
		if rightEdge-bw-1 < x+minTitle {
			continue
		}
		bx := rightEdge - bw
		placed = append(placed, HeaderButton{X: bx, W: bw, Action: c.action, Label: c.label})
		rightEdge = bx - 1
	}
	hl.Buttons = placed

	for _, b := range placed {
		bs := st.HeaderPill
		if m.HeaderHover == b.Action {
			bs = st.HeaderPillHot
			bs.Underline = true
		}
		g.Put(b.X, y, b.X+b.W, b.Label, bs)
	}

	// Left side: status badge, path (or rename), diffstat, unsaved indicator.
	leftEnd := rightEdge
	curX := x + 1
	if curX >= leftEnd {
		return hl
	}
	if f, ok := m.SelectedFile(); ok {
		badge := st.HeaderBar
		badge.FG = st.StatusFG(f.Status)
		badge.Bold = true
		curX += g.Put(curX, y, leftEnd, "["+f.Status.Badge()+"] ", badge)

		title := textsafe.Clean(f.Path)
		if f.Status == StatusRenamed && f.OldPath != "" {
			title = textsafe.Clean(f.OldPath) + " → " + title
		}
		titleStyle := st.HeaderBar
		titleStyle.Bold = true

		// Measure optional suffixes (+N -M and [unsaved]).
		var statAdd, statDel string
		statW := 0
		if !f.Binary && !f.StatOnly && (f.Added > 0 || f.Deleted > 0) {
			if f.Added > 0 {
				statAdd = "+" + strconv.Itoa(f.Added)
			}
			if f.Deleted > 0 {
				statDel = "-" + strconv.Itoa(f.Deleted)
			}
			statW = 2 + ansi.StringWidth(statAdd) + ansi.StringWidth(statDel)
			if statAdd != "" && statDel != "" {
				statW++
			}
		}
		unsavedText := ""
		unsavedW := 0
		if m.UnsavedInEditor {
			unsavedText = " [unsaved]"
			unsavedW = ansi.StringWidth(unsavedText)
		}

		avail := leftEnd - curX
		showUnsaved := unsavedW > 0 && avail-unsavedW >= 12
		if showUnsaved {
			avail -= unsavedW
		}
		showStat := statW > 0 && avail-statW >= 12
		if showStat {
			avail -= statW
		}
		if avail > 0 {
			curX += g.Put(curX, y, curX+avail, space.Middle(title, avail), titleStyle)
		}
		if showStat {
			curX += g.Put(curX, y, leftEnd, "  ", st.HeaderBar)
			if statAdd != "" {
				as := st.HeaderBar
				as.FG = st.AddFG
				curX += g.Put(curX, y, leftEnd, statAdd, as)
				if statDel != "" {
					curX += g.Put(curX, y, leftEnd, " ", st.HeaderBar)
				}
			}
			if statDel != "" {
				ds := st.HeaderBar
				ds.FG = st.RemoveFG
				curX += g.Put(curX, y, leftEnd, statDel, ds)
			}
		}
		if showUnsaved {
			g.Put(curX, y, leftEnd, unsavedText, st.HeaderWarning)
		}
	} else {
		label := "Review Diff"
		if m.Branch != "" {
			label += " · " + textsafe.Clean(m.Branch)
		}
		g.Put(curX, y, leftEnd, space.End(label, leftEnd-curX), st.HeaderBar)
	}
	return hl
}

// DiffViewportRows returns the number of diff content rows visible inside rect,
// reserving the bottom row when the inline diff search bar is shown.
func (m *Model) DiffViewportRows(r DiffRect) int {
	h := r.H
	if m.showSearchBar() && h > 1 {
		h--
	}
	return max(h, 0)
}

func (m *Model) showSearchBar() bool {
	return m.SearchActive || m.SearchQuery != ""
}

// HitDiff resolves a frame cell (x, y) inside the main diff pane.
func (m *Model) HitDiff(r DiffRect, x, y int) DiffHit {
	if r.W <= 0 || r.H <= 0 || x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
		return DiffHit{}
	}
	viewRows := m.DiffViewportRows(r)
	if m.showSearchBar() && y >= r.Y+viewRows {
		return DiffHit{Kind: DiffHitSearchBar}
	}
	rowOff := y - r.Y
	if rowOff < 0 || rowOff >= viewRows {
		return DiffHit{}
	}
	if x == r.X+r.W-1 && m.visualRowCount(r.W) > viewRows {
		return DiffHit{Kind: DiffHitScrollbar, ScrollRow: rowOff}
	}
	if m.EffectiveViewMode(r.W) == ViewSplit {
		idx := m.Scroll + rowOff
		if idx < 0 || idx >= len(m.split) {
			return DiffHit{}
		}
		sr := m.split[idx]
		if sr.Kind == RowHunkHeader && sr.GapID >= 0 {
			return DiffHit{Kind: DiffHitExpand, RowIdx: idx, GapID: sr.GapID}
		}
		return DiffHit{Kind: DiffHitRow, RowIdx: idx}
	}
	vis := m.ensureVisualLines(r.W)
	vi := m.Scroll + rowOff
	if vi < 0 || vi >= len(vis) {
		return DiffHit{}
	}
	rowIdx := vis[vi].rowIdx
	if rowIdx < 0 || rowIdx >= len(m.unified) {
		return DiffHit{}
	}
	ur := m.unified[rowIdx]
	if ur.Kind == RowHunkHeader && ur.GapID >= 0 {
		return DiffHit{Kind: DiffHitExpand, RowIdx: rowIdx, GapID: ur.GapID}
	}
	return DiffHit{Kind: DiffHitRow, RowIdx: rowIdx}
}

// PaintDiff draws the main diff pane (Unified or Split, plus optional inline
// search bar and hunk-marked scrollbar) into g.
func (m *Model) PaintDiff(g *grid.Grid, r DiffRect, st Styles) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	for y := r.Y; y < r.Y+r.H; y++ {
		g.Fill(r.X, r.X+r.W, y, st.Text)
	}

	viewRows := m.DiffViewportRows(r)
	if m.showSearchBar() && viewRows < r.H {
		m.paintSearchBar(g, r.X, r.Y+viewRows, r.W, st)
	}
	if viewRows <= 0 {
		return
	}

	// Empty / loading / error states when no diff rows are ready.
	if m.Diff == nil {
		msg := ""
		msgStyle := st.Muted
		switch {
		case m.LoadingFiles || m.LoadingDiff:
			msg = "Loading diff…"
		case m.NotRepo:
			msg = "This workspace is not a Git repository."
		case m.DiffErr != "":
			msg = m.DiffErr
			msgStyle = st.Chrome.Overlay.Error
			msgStyle.BG = st.Text.BG
		case len(m.Files) == 0:
			msg = "Working tree clean — no changes against " + m.BaseRef + "."
		case len(m.Filtered) == 0:
			msg = "No files match the current filter."
		default:
			msg = "Select a file on the left to inspect its diff."
		}
		cy := r.Y + min(2, viewRows-1)
		g.Put(r.X+2, cy, r.X+r.W-1, space.End(msg, max(r.W-3, 0)), msgStyle)
		return
	}

	m.ensureSearchMode(r.W)
	if m.EffectiveViewMode(r.W) == ViewSplit {
		m.paintSplit(g, r, viewRows, st)
		return
	}
	m.paintUnified(g, r, viewRows, st)
}

func (m *Model) paintSearchBar(g *grid.Grid, x, y, w int, st Styles) {
	bg := st.HeaderBar
	if m.SearchActive {
		bg = st.InputField
	}
	g.Fill(x, x+w, y, bg)
	curX := x + g.Put(x, y, x+w, " / ", bg)

	var status string
	if m.SearchQuery != "" {
		if len(m.SearchMatches) == 0 {
			status = " 0 matches  Esc:clear "
		} else {
			status = fmt.Sprintf(" %d/%d  n/N:jump  Esc:clear ", m.SearchIdx+1, len(m.SearchMatches))
		}
	} else {
		status = " Enter:search  Esc:cancel "
	}
	statusW := ansi.StringWidth(status)
	queryEnd := x + w - statusW
	if queryEnd < curX {
		queryEnd = x + w
		statusW = 0
	}
	q := textsafe.Clean(m.SearchQuery)
	if qEndW := queryEnd - curX; qEndW > 0 {
		shown := q
		if ansi.StringWidth(shown) >= qEndW {
			shown = ansi.TruncateLeft(shown, ansi.StringWidth(shown)-qEndW+1, "")
		}
		cx := curX + g.Put(curX, y, queryEnd, shown, bg)
		if m.SearchActive && cx < queryEnd {
			g.Set(cx, y, " ", st.InputCaret)
		}
	}
	if statusW > 0 {
		ms := bg
		ms.Faint = true
		g.Put(x+w-statusW, y, x+w, status, ms)
	}
}

func (m *Model) paintUnified(g *grid.Grid, r DiffRect, viewRows int, st Styles) {
	vis := m.ensureVisualLines(r.W)
	if len(vis) == 0 {
		return
	}
	maxScroll := max(len(vis)-viewRows, 0)
	if m.Scroll > maxScroll {
		m.Scroll = maxScroll
	}
	if m.Scroll < 0 {
		m.Scroll = 0
	}

	gutterW := m.unifiedGutterWidth()
	digits := (gutterW - 5) / 2
	contentW := max(r.W-1, 1) // reserve 1 col on right for scrollbar
	textX := r.X + gutterW
	textW := max(r.X+contentW-textX, 0)

	for row := range viewRows {
		vi := m.Scroll + row
		if vi >= len(vis) {
			break
		}
		y := r.Y + row
		vl := vis[vi]
		ur := m.unified[vl.rowIdx]
		isCur := vl.rowIdx == m.Cursor

		if ur.Kind == RowHunkHeader {
			hs := st.HunkHeader
			if isCur && m.Focus == FocusDiff {
				hs = st.HunkHeaderCur
			}
			g.Fill(r.X, r.X+contentW, y, hs)
			if isCur && m.Focus == FocusDiff {
				g.Set(r.X, y, "▌", hs)
			}
			g.Put(r.X+2, y, r.X+contentW, space.End(ur.Text, max(contentW-3, 0)), hs)
			continue
		}
		if ur.Kind == RowInfo {
			is := st.Muted
			if isCur && m.Focus == FocusDiff {
				is = st.SelectionBlur
			}
			g.Fill(r.X, r.X+contentW, y, is)
			g.Put(r.X+2, y, r.X+contentW, space.End(ur.Text, max(contentW-3, 0)), is)
			continue
		}

		rowBG := diffLineBG(ur.Kind, isCur && m.Focus == FocusDiff, st)
		hlBG := diffHighlightBG(ur.Kind, st)

		// Gutter cell.
		gStyle := st.Gutter
		if rowBG != nil {
			gStyle.BG = rowBG
		}
		g.Fill(r.X, min(textX, r.X+contentW), y, gStyle)
		if isCur && m.Focus == FocusDiff && !vl.cont {
			mk := gStyle
			if st.Accent.BG != nil {
				mk.FG = st.Accent.BG
			}
			mk.Bold = true
			g.Set(r.X, y, "▌", mk)
		}
		if !vl.cont {
			oldStr := formatLineNum(ur.OldLine, digits)
			newStr := formatLineNum(ur.NewLine, digits)
			g.Put(r.X+1, y, min(textX, r.X+contentW), oldStr+" "+newStr+" ", gStyle)
		}
		sign, signStyle := signForRow(ur.Kind, vl.cont, gStyle, st)
		signX := r.X + 1 + digits + 1 + digits + 1
		if signX < r.X+contentW {
			g.Put(signX, y, min(textX, r.X+contentW), sign, signStyle)
		}

		// Code text area.
		if textW > 0 {
			fillStyle := st.Text
			if rowBG != nil {
				fillStyle.BG = rowBG
			}
			g.Fill(textX, r.X+contentW, y, fillStyle)
			hscroll := 0
			if !m.Wrap {
				hscroll = m.HScroll
			}
			m.paintCodeSegment(g, textX, y, textW, ur.Text, vl.startByte, vl.endByte, hscroll, ur.Spans, ur.Highlights, vl.rowIdx, 0, rowBG, hlBG, st)
		}
	}

	m.paintScrollbar(g, r.X+r.W-1, r.Y, viewRows, len(vis), m.Scroll, st, func(vi int) RowKind {
		return m.unified[vis[vi].rowIdx].Kind
	})
}

func (m *Model) paintSplit(g *grid.Grid, r DiffRect, viewRows int, st Styles) {
	total := len(m.split)
	if total == 0 {
		return
	}
	maxScroll := max(total-viewRows, 0)
	if m.Scroll > maxScroll {
		m.Scroll = maxScroll
	}
	if m.Scroll < 0 {
		m.Scroll = 0
	}

	contentW := max(r.W-1, 1) // reserve 1 col for scrollbar
	leftW := (contentW - 1) / 2
	divX := r.X + leftW
	rightX := divX + 1
	rightW := contentW - leftW - 1
	gutterW := m.splitGutterWidth()
	digits := gutterW - 4

	for row := range viewRows {
		idx := m.Scroll + row
		if idx >= total {
			break
		}
		y := r.Y + row
		sr := m.split[idx]
		isCur := idx == m.Cursor

		if sr.Kind == RowHunkHeader {
			hs := st.HunkHeader
			if isCur && m.Focus == FocusDiff {
				hs = st.HunkHeaderCur
			}
			g.Fill(r.X, r.X+contentW, y, hs)
			if isCur && m.Focus == FocusDiff {
				g.Set(r.X, y, "▌", hs)
			}
			g.Put(r.X+2, y, r.X+contentW, space.End(sr.HeaderText, max(contentW-3, 0)), hs)
			continue
		}
		if sr.Kind == RowInfo {
			is := st.Muted
			if isCur && m.Focus == FocusDiff {
				is = st.SelectionBlur
			}
			g.Fill(r.X, r.X+contentW, y, is)
			g.Put(r.X+2, y, r.X+contentW, space.End(sr.HeaderText, max(contentW-3, 0)), is)
			continue
		}

		m.paintSplitHalf(g, r.X, y, leftW, gutterW, digits, sr.Left, idx, 0, isCur && m.Focus == FocusDiff, st)
		g.Set(divX, y, "│", st.SplitBorder)
		m.paintSplitHalf(g, rightX, y, rightW, gutterW, digits, sr.Right, idx, 1, isCur && m.Focus == FocusDiff, st)
	}

	m.paintScrollbar(g, r.X+r.W-1, r.Y, viewRows, total, m.Scroll, st, func(idx int) RowKind {
		return m.split[idx].Kind
	})
}

func (m *Model) paintSplitHalf(g *grid.Grid, x, y, w, gutterW, digits int, cell SplitCell, rowIdx, side int, isCur bool, st Styles) {
	if w <= 0 {
		return
	}
	if cell.Kind == RowFiller {
		fs := st.Filler
		if isCur && st.CurrentLine.BG != nil {
			fs.BG = st.CurrentLine.BG
		}
		g.Fill(x, x+w, y, fs)
		return
	}

	rowBG := diffLineBG(cell.Kind, isCur, st)
	hlBG := diffHighlightBG(cell.Kind, st)

	gStyle := st.Gutter
	if rowBG != nil {
		gStyle.BG = rowBG
	}
	gEnd := min(x+gutterW, x+w)
	g.Fill(x, gEnd, y, gStyle)
	if isCur && side == 0 {
		mk := gStyle
		if st.Accent.BG != nil {
			mk.FG = st.Accent.BG
		}
		mk.Bold = true
		g.Set(x, y, "▌", mk)
	}
	g.Put(x+1, y, gEnd, formatLineNum(cell.Line, digits)+" ", gStyle)
	sign, signStyle := signForRow(cell.Kind, false, gStyle, st)
	if sx := x + 1 + digits + 1; sx < gEnd {
		g.Put(sx, y, gEnd, sign, signStyle)
	}

	textX := x + gutterW
	textW := x + w - textX
	if textW <= 0 {
		return
	}
	fillStyle := st.Text
	if rowBG != nil {
		fillStyle.BG = rowBG
	}
	g.Fill(textX, x+w, y, fillStyle)
	m.paintCodeSegment(g, textX, y, textW, cell.Text, 0, len(cell.Text), m.HScroll, cell.Spans, cell.Highlights, rowIdx, side, rowBG, hlBG, st)
}

// paintCodeSegment renders [startByte, endByte) of line with syntax colors,
// diff line/word backgrounds, search match highlights, tab expansion, and
// horizontal scroll clipping.
func (m *Model) paintCodeSegment(
	g *grid.Grid,
	x, y, width int,
	line string,
	startByte, endByte int,
	hscroll int,
	spans []syntax.Span,
	highlights []ByteRange,
	rowIdx, side int,
	rowBG, hlBG color.Color,
	st Styles,
) {
	if width <= 0 || startByte >= endByte || startByte >= len(line) {
		return
	}
	if endByte > len(line) {
		endByte = len(line)
	}
	tabSize := m.TabSize
	if tabSize <= 0 {
		tabSize = 4
	}

	// Collect search matches on this row/side (typically 0 or very few).
	var matches []SearchMatch
	curMatchIdx := -1
	for mi, mt := range m.SearchMatches {
		if mt.RowIdx == rowIdx && mt.Side == side {
			if mi == m.SearchIdx {
				curMatchIdx = len(matches)
			}
			matches = append(matches, mt)
		}
	}

	spanIdx := 0
	for spanIdx < len(spans) && spans[spanIdx].End <= startByte {
		spanIdx++
	}
	hlIdx := 0
	for hlIdx < len(highlights) && highlights[hlIdx].End <= startByte {
		hlIdx++
	}

	col := 0 // display column within this segment
	bytePos := startByte
	rest := line[startByte:endByte]
	state := -1
	for len(rest) > 0 {
		cluster, next, _, nextState := uniseg.FirstGraphemeClusterInString(rest, state)
		cw := clusterWidth(cluster, col, tabSize)

		// Resolve syntax style at bytePos.
		for spanIdx < len(spans) && spans[spanIdx].End <= bytePos {
			spanIdx++
		}
		cellStyle := st.Text
		if spanIdx < len(spans) && spans[spanIdx].Start <= bytePos && bytePos < spans[spanIdx].End {
			k := spans[spanIdx].Kind
			if int(k) >= 0 && int(k) < len(st.Syntax) {
				cellStyle = st.Syntax[k]
			}
		}
		if rowBG != nil {
			cellStyle.BG = rowBG
		}

		// Inline word-diff highlight.
		for hlIdx < len(highlights) && highlights[hlIdx].End <= bytePos {
			hlIdx++
		}
		if hlIdx < len(highlights) && highlights[hlIdx].Start <= bytePos && bytePos < highlights[hlIdx].End {
			if hlBG != nil {
				cellStyle.BG = hlBG
			} else {
				cellStyle.Bold = true
				cellStyle.Underline = true
			}
		}

		// Search match highlight beats diff background so matches stand out.
		for mi, mt := range matches {
			if bytePos >= mt.ByteStart && bytePos < mt.ByteEnd {
				if mi == curMatchIdx {
					cellStyle = st.CurrentMatch
				} else {
					if st.Match.BG != nil {
						cellStyle.BG = st.Match.BG
					}
					if st.Match.FG != nil {
						cellStyle.FG = st.Match.FG
					}
				}
				break
			}
		}

		// Emit visible cells after applying hscroll.
		if cluster == "\t" {
			for k := range cw {
				vc := col + k - hscroll
				if vc >= 0 && vc < width {
					g.Set(x+vc, y, " ", cellStyle)
				}
			}
		} else if cw > 0 {
			vc := col - hscroll
			if vc >= 0 && vc+cw <= width {
				g.Put(x+vc, y, x+width, cluster, cellStyle)
			} else if vc >= width {
				break
			}
		}

		col += cw
		bytePos += len(cluster)
		rest = next
		state = nextState
	}
}

func (m *Model) paintScrollbar(g *grid.Grid, x, y, viewRows, total, scroll int, st Styles, kindAt func(int) RowKind) {
	if viewRows <= 0 {
		return
	}
	if total <= viewRows {
		// Still paint diff overview markers even when content fits, so hunks stand out.
		for row := range viewRows {
			g.Set(x, y+row, " ", st.Text)
		}
		return
	}
	thumbTop, thumbLen := scrollbarThumb(total, viewRows, scroll)

	// Map changed rows onto overview track ticks (added=green, removed=red, modify=amber).
	marks := make([]color.Color, viewRows)
	for i := range total {
		k := kindAt(i)
		var c color.Color
		switch k {
		case RowAdd:
			c = st.AddFG
		case RowDelete:
			c = st.RemoveFG
		case RowModify:
			c = st.ModifyFG
		}
		if c == nil {
			continue
		}
		r := i * viewRows / total
		if r >= 0 && r < viewRows {
			// Modify or Delete overwrites Add if mixed in the same bucket.
			if marks[r] == nil || k == RowModify {
				marks[r] = c
			}
		}
	}

	for row := range viewRows {
		inThumb := row >= thumbTop && row < thumbTop+thumbLen
		if marks[row] != nil {
			ms := st.Scrollbar
			if inThumb {
				ms = st.ScrollbarThumb
			}
			ms.FG = marks[row]
			if inThumb {
				g.Set(x, y+row, "┃", ms)
			} else {
				g.Set(x, y+row, "▪", ms)
			}
			continue
		}
		if inThumb {
			g.Set(x, y+row, "┃", st.ScrollbarThumb)
		} else {
			g.Set(x, y+row, "│", st.Scrollbar)
		}
	}
}

func scrollbarThumb(total, viewRows, scroll int) (top, length int) {
	if total <= viewRows || viewRows <= 0 {
		return 0, viewRows
	}
	length = max(viewRows*viewRows/total, 1)
	maxScroll := total - viewRows
	if maxScroll <= 0 {
		return 0, length
	}
	top = scroll * (viewRows - length) / maxScroll
	return min(max(top, 0), viewRows-length), length
}

func diffLineBG(kind RowKind, isCur bool, st Styles) color.Color {
	switch kind {
	case RowAdd:
		if st.DiffAddBG != nil {
			return st.DiffAddBG
		}
	case RowDelete:
		if st.DiffRemoveBG != nil {
			return st.DiffRemoveBG
		}
	case RowModify:
		if st.DiffModifyBG != nil {
			return st.DiffModifyBG
		}
	}
	if isCur && st.CurrentLine.BG != nil {
		return st.CurrentLine.BG
	}
	return nil
}

func diffHighlightBG(kind RowKind, st Styles) color.Color {
	switch kind {
	case RowAdd:
		return st.DiffAddHighlight
	case RowDelete:
		return st.DiffRemoveHighlight
	}
	return nil
}

func signForRow(kind RowKind, cont bool, base grid.Style, st Styles) (string, grid.Style) {
	if cont {
		s := base
		s.Faint = true
		return "↳", s
	}
	switch kind {
	case RowAdd:
		s := base
		s.FG = st.AddFG
		s.Bold = true
		return "+", s
	case RowDelete:
		s := base
		s.FG = st.RemoveFG
		s.Bold = true
		return "-", s
	default:
		return " ", base
	}
}

func formatLineNum(n, width int) string {
	if n <= 0 {
		return strings.Repeat(" ", width)
	}
	s := strconv.Itoa(n)
	if len(s) >= width {
		return s[len(s)-width:]
	}
	return strings.Repeat(" ", width-len(s)) + s
}
