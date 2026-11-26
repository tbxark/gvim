package reviewdiff

import (
	"slices"
	"strings"

	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/syntax"
)

// Model holds all UI state for Review Diff Mode: the changed-file list, filter,
// active file diff, view options, context expansion state, scroll/cursor state,
// and in-diff search matches.
type Model struct {
	BaseRef string
	Branch  string

	Files        []FileEntry
	Filtered     []int
	FilterQuery  string
	FilterActive bool
	Selected     int
	FileScroll   int
	SelectedPath string

	LoadingFiles bool
	LoadingDiff  bool
	FilesErr     string
	DiffErr      string
	NotRepo      bool

	Diff            *FileDiff
	UnsavedInEditor bool

	ViewMode ViewMode
	FullFile bool
	Wrap     bool
	TabSize  int
	Focus    PaneFocus

	Gaps    map[int]int
	Cursor  int // logical row index in active diff
	Scroll  int // first visible visual row
	HScroll int // horizontal scroll offset in cells (unwrapped/split)

	SearchActive  bool
	SearchQuery   string
	SearchMatches []SearchMatch
	SearchIdx     int
	searchMode    ViewMode

	HeaderHover HeaderAction

	// Cached syntax spans and built logical rows for Diff.
	oldLines  []string
	newLines  []string
	oldSpans  [][]syntax.Span
	newSpans  [][]syntax.Span
	unified   []UnifiedRow
	split     []SplitRow
	uHunkRows []int
	sHunkRows []int
	rowGen    uint64

	// Cached visual-line table for Unified mode wrapping.
	visGen   uint64
	visWrap  bool
	visTextW int
	visLines []visualLine
}

// NewModel returns a Review Diff Mode model with default settings (vs HEAD,
// Unified view, soft wrap on, focus on file list).
func NewModel() Model {
	return Model{
		BaseRef: "HEAD",
		Wrap:    true,
		TabSize: 4,
		Focus:   FocusFiles,
	}
}

// EffectiveViewMode returns ViewSplit only when ViewSplit is selected and the
// diff viewport is wide enough to render two readable columns.
func (m *Model) EffectiveViewMode(diffW int) ViewMode {
	if m.ViewMode == ViewSplit && diffW >= MinSplitWidth {
		return ViewSplit
	}
	return ViewUnified
}

// SetFiles updates the changed-file list while keeping the previously selected
// file highlighted if it still exists.
func (m *Model) SetFiles(files []FileEntry, branch string, errMsg string, notRepo bool) {
	m.LoadingFiles = false
	m.Branch = branch
	m.FilesErr = errMsg
	m.NotRepo = notRepo
	m.Files = slices.Clone(files)
	m.rebuildFilter()
}

// SetFilter updates the sidebar file filter and preserves the selected path
// when it still matches.
func (m *Model) SetFilter(query string) {
	if m.FilterQuery == query {
		return
	}
	m.FilterQuery = query
	m.rebuildFilter()
}

func (m *Model) rebuildFilter() {
	q := strings.ToLower(strings.TrimSpace(m.FilterQuery))
	m.Filtered = m.Filtered[:0]
	for i, f := range m.Files {
		if q == "" || strings.Contains(strings.ToLower(f.Path), q) ||
			(f.OldPath != "" && strings.Contains(strings.ToLower(f.OldPath), q)) {
			m.Filtered = append(m.Filtered, i)
		}
	}
	if len(m.Filtered) == 0 {
		m.Selected = 0
		m.FileScroll = 0
		return
	}
	if m.SelectedPath != "" {
		for fi, idx := range m.Filtered {
			if m.Files[idx].Path == m.SelectedPath {
				m.Selected = fi
				return
			}
		}
	}
	if m.Selected >= len(m.Filtered) {
		m.Selected = len(m.Filtered) - 1
	}
	if m.Selected < 0 {
		m.Selected = 0
	}
	m.SelectedPath = m.Files[m.Filtered[m.Selected]].Path
}

// SelectedFile returns the currently highlighted file entry in the sidebar.
func (m *Model) SelectedFile() (FileEntry, bool) {
	if m.Selected < 0 || m.Selected >= len(m.Filtered) {
		return FileEntry{}, false
	}
	return m.Files[m.Filtered[m.Selected]], true
}

// SelectFile moves the sidebar selection to filtered index idx. It returns the
// newly selected path and whether the selected file changed.
func (m *Model) SelectFile(idx int, visibleRows int) (FileEntry, bool) {
	if len(m.Filtered) == 0 {
		return FileEntry{}, false
	}
	idx = min(max(idx, 0), len(m.Filtered)-1)
	prevPath := m.SelectedPath
	m.Selected = idx
	f := m.Files[m.Filtered[idx]]
	m.SelectedPath = f.Path
	m.ensureFileVisible(visibleRows)
	return f, f.Path != prevPath
}

// MoveFile shifts the sidebar selection by delta rows.
func (m *Model) MoveFile(delta, visibleRows int) (FileEntry, bool) {
	if len(m.Filtered) == 0 {
		return FileEntry{}, false
	}
	return m.SelectFile(m.Selected+delta, visibleRows)
}

// ScrollFiles scrolls the sidebar viewport without changing the selection.
func (m *Model) ScrollFiles(delta, visibleRows int) {
	if len(m.Filtered) == 0 || visibleRows <= 0 {
		m.FileScroll = 0
		return
	}
	maxScroll := max(len(m.Filtered)-visibleRows, 0)
	m.FileScroll = min(max(m.FileScroll+delta, 0), maxScroll)
}

func (m *Model) ensureFileVisible(visibleRows int) {
	if visibleRows <= 0 {
		return
	}
	if m.Selected < m.FileScroll {
		m.FileScroll = m.Selected
	} else if m.Selected >= m.FileScroll+visibleRows {
		m.FileScroll = m.Selected - visibleRows + 1
	}
	maxScroll := max(len(m.Filtered)-visibleRows, 0)
	if m.FileScroll > maxScroll {
		m.FileScroll = maxScroll
	}
	if m.FileScroll < 0 {
		m.FileScroll = 0
	}
}

// SetDiff installs the loaded diff for one file. If the same file is reloaded
// (e.g. after auto-refresh), cursor, scroll, and gap expansions are preserved.
func (m *Model) SetDiff(d *FileDiff, errMsg string) {
	m.LoadingDiff = false
	m.DiffErr = errMsg
	if d == nil {
		m.Diff = nil
		m.oldLines, m.newLines = nil, nil
		m.oldSpans, m.newSpans = nil, nil
		m.unified, m.split = nil, nil
		m.uHunkRows, m.sHunkRows = nil, nil
		m.Cursor, m.Scroll, m.HScroll = 0, 0, 0
		m.Gaps = nil
		m.SearchMatches = nil
		m.SearchIdx = 0
		m.rowGen++
		return
	}
	sameFile := m.Diff != nil && m.Diff.Path == d.Path && m.Diff.BaseRef == d.BaseRef
	oldLines := sanitizeLines(d.OldLines)
	newLines := sanitizeLines(d.NewLines)
	if len(d.Hunks) == 0 && !d.Binary && (len(oldLines) > 0 || len(newLines) > 0) {
		d.Hunks = diff.Lines(oldLines, newLines)
	}
	m.Diff = d
	m.oldLines = oldLines
	m.newLines = newLines
	if !d.Binary {
		m.oldSpans = lexSide(d.Path, oldLines)
		m.newSpans = lexSide(d.Path, newLines)
	} else {
		m.oldSpans, m.newSpans = nil, nil
	}
	if !sameFile {
		m.Gaps = nil
		m.Cursor = 0
		m.Scroll = 0
		m.HScroll = 0
	}
	m.rebuildRows()
	if !sameFile {
		m.jumpToFirstHunk()
	}
}

// ClearDiff removes any displayed diff without altering the file list.
func (m *Model) ClearDiff() {
	m.SetDiff(nil, "")
}

func (m *Model) rebuildRows() {
	m.unified, m.split, m.uHunkRows, m.sHunkRows = buildRows(
		m.Diff, m.oldLines, m.newLines, m.oldSpans, m.newSpans, m.FullFile, m.Gaps,
	)
	m.rowGen++
	m.clampCursor()
	m.recomputeSearch()
}

func (m *Model) jumpToFirstHunk() {
	hunks := m.activeHunkRows(MinSplitWidth)
	if len(hunks) > 0 {
		// Land on the first changed line (or its hunk header directly above).
		m.Cursor = hunks[0]
		if m.Cursor > 0 {
			m.Scroll = max(m.Cursor-1, 0)
		} else {
			m.Scroll = 0
		}
	}
}

// LogicalRowCount returns the number of logical rows in the active view mode.
func (m *Model) LogicalRowCount(diffW int) int {
	if m.EffectiveViewMode(diffW) == ViewSplit {
		return len(m.split)
	}
	return len(m.unified)
}

func (m *Model) activeHunkRows(diffW int) []int {
	if m.EffectiveViewMode(diffW) == ViewSplit {
		return m.sHunkRows
	}
	return m.uHunkRows
}

func (m *Model) clampCursor() {
	n := len(m.unified)
	if m.ViewMode == ViewSplit && len(m.split) > 0 {
		n = len(m.split)
	}
	if n == 0 {
		m.Cursor = 0
		m.Scroll = 0
		return
	}
	m.Cursor = min(max(m.Cursor, 0), n-1)
}

// ToggleViewMode switches between Unified and Side-by-Side diff rendering.
func (m *Model) ToggleViewMode() {
	if m.ViewMode == ViewUnified {
		m.ViewMode = ViewSplit
	} else {
		m.ViewMode = ViewUnified
	}
	m.HScroll = 0
	m.clampCursor()
	m.recomputeSearch()
}

// ToggleFullFile switches between hunk-only context and full-file diff view.
func (m *Model) ToggleFullFile() {
	m.FullFile = !m.FullFile
	m.rebuildRows()
}

// ToggleWrap flips soft line wrapping for Unified mode.
func (m *Model) ToggleWrap() {
	m.Wrap = !m.Wrap
	if m.Wrap {
		m.HScroll = 0
	}
	m.visGen = 0
}

// MoveCursor moves the active diff cursor by delta logical rows and keeps it
// visible in the viewport.
func (m *Model) MoveCursor(delta, diffW, viewRows int) {
	n := m.LogicalRowCount(diffW)
	if n == 0 {
		return
	}
	m.Cursor = min(max(m.Cursor+delta, 0), n-1)
	m.ensureCursorVisible(diffW, viewRows)
}

// PageMove shifts the cursor and viewport by delta pages.
func (m *Model) PageMove(pages, diffW, viewRows int) {
	step := max(viewRows-2, 1) * pages
	m.MoveCursor(step, diffW, viewRows)
}

// JumpTop moves to the first diff row.
func (m *Model) JumpTop() {
	m.Cursor = 0
	m.Scroll = 0
}

// JumpBottom moves to the last diff row.
func (m *Model) JumpBottom(diffW, viewRows int) {
	n := m.LogicalRowCount(diffW)
	if n == 0 {
		return
	}
	m.Cursor = n - 1
	m.ensureCursorVisible(diffW, viewRows)
}

// ScrollDiff scrolls the diff viewport by delta visual rows without moving the
// cursor unless it leaves the viewport bounds.
func (m *Model) ScrollDiff(delta, diffW, viewRows int) {
	total := m.visualRowCount(diffW)
	if total == 0 || viewRows <= 0 {
		m.Scroll = 0
		return
	}
	maxScroll := max(total-viewRows, 0)
	m.Scroll = min(max(m.Scroll+delta, 0), maxScroll)
}

// ScrollHorizontal pans horizontally by delta cells when unwrapped or in Split mode.
func (m *Model) ScrollHorizontal(delta int) {
	m.HScroll = max(m.HScroll+delta, 0)
}

// NextHunk jumps to the next diff hunk in the current file. If already on or
// past the last hunk, it returns (false, true) so the caller can advance to the
// next changed file.
func (m *Model) NextHunk(diffW, viewRows int) (jumped bool, needNextFile bool) {
	hunks := m.activeHunkRows(diffW)
	for _, row := range hunks {
		if row > m.Cursor {
			m.Cursor = row
			m.centerCursor(diffW, viewRows)
			return true, false
		}
	}
	return false, true
}

// PrevHunk jumps to the previous diff hunk in the current file. If already at
// or before the first hunk, it returns (false, true) so the caller can move to
// the previous changed file.
func (m *Model) PrevHunk(diffW, viewRows int) (jumped bool, needPrevFile bool) {
	hunks := m.activeHunkRows(diffW)
	for i := len(hunks) - 1; i >= 0; i-- {
		if hunks[i] < m.Cursor {
			m.Cursor = hunks[i]
			m.centerCursor(diffW, viewRows)
			return true, false
		}
	}
	return false, true
}

// JumpLastHunk places the cursor on the last hunk of the loaded file (used when
// crossing backward into the previous changed file).
func (m *Model) JumpLastHunk(diffW, viewRows int) {
	hunks := m.activeHunkRows(diffW)
	if len(hunks) == 0 {
		return
	}
	m.Cursor = hunks[len(hunks)-1]
	m.centerCursor(diffW, viewRows)
}

// CurrentHunkPos returns the 1-based hunk index at or before Cursor and the
// total number of hunks in the loaded diff.
func (m *Model) CurrentHunkPos(diffW int) (current, total int) {
	hunks := m.activeHunkRows(diffW)
	total = len(hunks)
	if total == 0 {
		return 0, 0
	}
	current = 1
	for i, row := range hunks {
		if row <= m.Cursor {
			current = i + 1
		} else {
			break
		}
	}
	return current, total
}

// ExpandAtCursor expands the collapsed context gap if Cursor stands on an
// expandable RowHunkHeader.
func (m *Model) ExpandAtCursor(diffW int) bool {
	gapID := m.gapIDAt(m.Cursor, diffW)
	if gapID < 0 {
		return false
	}
	return m.ExpandGap(gapID)
}

// ExpandGap reveals ExpandStep more hidden lines for gapID and rebuilds rows.
func (m *Model) ExpandGap(gapID int) bool {
	if gapID < 0 || m.FullFile || m.Diff == nil {
		return false
	}
	if m.Gaps == nil {
		m.Gaps = make(map[int]int)
	}
	m.Gaps[gapID] += ExpandStep
	m.rebuildRows()
	return true
}

func (m *Model) gapIDAt(rowIdx, diffW int) int {
	if m.EffectiveViewMode(diffW) == ViewSplit {
		if rowIdx < 0 || rowIdx >= len(m.split) {
			return -1
		}
		if m.split[rowIdx].Kind != RowHunkHeader {
			return -1
		}
		return m.split[rowIdx].GapID
	}
	if rowIdx < 0 || rowIdx >= len(m.unified) {
		return -1
	}
	if m.unified[rowIdx].Kind != RowHunkHeader {
		return -1
	}
	return m.unified[rowIdx].GapID
}

// EditTargetAtCursor returns the repository-relative path and 1-based line in
// the working file corresponding to Cursor. ok is false for headers, info rows,
// binary files, or deleted files.
func (m *Model) EditTargetAtCursor(diffW int) (path string, line int, ok bool) {
	if m.Diff == nil || m.Diff.Status == StatusDeleted {
		return "", 0, false
	}
	if m.EffectiveViewMode(diffW) == ViewSplit {
		if m.Cursor < 0 || m.Cursor >= len(m.split) {
			return "", 0, false
		}
		sr := m.split[m.Cursor]
		if sr.Kind == RowHunkHeader || sr.Kind == RowInfo || sr.EditLine <= 0 {
			return "", 0, false
		}
		return m.Diff.Path, sr.EditLine, true
	}
	if m.Cursor < 0 || m.Cursor >= len(m.unified) {
		return "", 0, false
	}
	ur := m.unified[m.Cursor]
	if ur.Kind == RowHunkHeader || ur.Kind == RowInfo || ur.EditLine <= 0 {
		return "", 0, false
	}
	return m.Diff.Path, ur.EditLine, true
}

// SetSearch updates the in-diff search query and jumps to the first match at
// or after the current cursor.
func (m *Model) SetSearch(query string, diffW, viewRows int) {
	m.SearchQuery = query
	m.recomputeSearchFor(m.EffectiveViewMode(diffW))
	if len(m.SearchMatches) == 0 {
		return
	}
	// Pick first match at or after cursor, otherwise wrap to 0.
	m.SearchIdx = 0
	for i, mt := range m.SearchMatches {
		if mt.RowIdx >= m.Cursor {
			m.SearchIdx = i
			break
		}
	}
	m.Cursor = m.SearchMatches[m.SearchIdx].RowIdx
	m.centerCursor(diffW, viewRows)
}

// ClearSearch removes the active in-diff search query and highlights.
func (m *Model) ClearSearch() {
	m.SearchActive = false
	m.SearchQuery = ""
	m.SearchMatches = nil
	m.SearchIdx = 0
}

// NextMatch jumps to the next in-diff search match (wrapping at the end).
func (m *Model) NextMatch(diffW, viewRows int) bool {
	m.ensureSearchMode(diffW)
	if len(m.SearchMatches) == 0 {
		return false
	}
	m.SearchIdx = (m.SearchIdx + 1) % len(m.SearchMatches)
	m.Cursor = m.SearchMatches[m.SearchIdx].RowIdx
	m.centerCursor(diffW, viewRows)
	return true
}

// PrevMatch jumps to the previous in-diff search match (wrapping to the end).
func (m *Model) PrevMatch(diffW, viewRows int) bool {
	m.ensureSearchMode(diffW)
	if len(m.SearchMatches) == 0 {
		return false
	}
	m.SearchIdx = (m.SearchIdx - 1 + len(m.SearchMatches)) % len(m.SearchMatches)
	m.Cursor = m.SearchMatches[m.SearchIdx].RowIdx
	m.centerCursor(diffW, viewRows)
	return true
}

// ScrollToTrackRow jumps the diff scroll position to correspond to clicking row
// trackRow in a viewRows-tall scrollbar track.
func (m *Model) ScrollToTrackRow(trackRow, diffW, viewRows int) {
	total := m.visualRowCount(diffW)
	if total <= viewRows || viewRows <= 1 {
		m.Scroll = 0
		return
	}
	clamped := min(max(trackRow, 0), viewRows-1)
	maxScroll := total - viewRows
	m.Scroll = clamped * maxScroll / (viewRows - 1)
}

func (m *Model) ensureSearchMode(diffW int) {
	mode := m.EffectiveViewMode(diffW)
	if m.SearchQuery != "" && m.searchMode != mode {
		m.recomputeSearchFor(mode)
	}
}

func (m *Model) recomputeSearch() {
	m.recomputeSearchFor(m.ViewMode)
}

func (m *Model) recomputeSearchFor(mode ViewMode) {
	m.searchMode = mode
	m.SearchMatches = nil
	m.SearchIdx = 0
	if m.SearchQuery == "" {
		return
	}
	needle := strings.ToLower(m.SearchQuery)
	scanLine := func(rowIdx, side int, text string) {
		if text == "" {
			return
		}
		lower := strings.ToLower(text)
		// Guard against rare Unicode case folding changing byte length.
		if len(lower) != len(text) {
			if idx := strings.Index(lower, needle); idx >= 0 {
				m.SearchMatches = append(m.SearchMatches, SearchMatch{RowIdx: rowIdx, Side: side, ByteStart: 0, ByteEnd: len(text)})
			}
			return
		}
		off := 0
		for off <= len(lower)-len(needle) {
			idx := strings.Index(lower[off:], needle)
			if idx < 0 {
				break
			}
			start := off + idx
			m.SearchMatches = append(m.SearchMatches, SearchMatch{
				RowIdx:    rowIdx,
				Side:      side,
				ByteStart: start,
				ByteEnd:   start + len(needle),
			})
			off = start + max(len(needle), 1)
		}
	}
	if mode == ViewSplit {
		for i, r := range m.split {
			if r.Kind == RowHunkHeader || r.Kind == RowInfo {
				continue
			}
			if r.Left.Kind != RowFiller {
				scanLine(i, 0, r.Left.Text)
			}
			if r.Right.Kind != RowFiller {
				scanLine(i, 1, r.Right.Text)
			}
		}
		return
	}
	for i, r := range m.unified {
		if r.Kind == RowHunkHeader || r.Kind == RowInfo {
			continue
		}
		scanLine(i, 0, r.Text)
	}
}

func (m *Model) unifiedGutterWidth() int {
	maxLine := max(len(m.oldLines), len(m.newLines))
	digits := digitCount(maxLine)
	if digits < 3 {
		digits = 3
	}
	// " " + oldDigits + " " + newDigits + " " + sign + " "
	return 1 + digits + 1 + digits + 3
}

func (m *Model) splitGutterWidth() int {
	maxLine := max(len(m.oldLines), len(m.newLines))
	digits := digitCount(maxLine)
	if digits < 3 {
		digits = 3
	}
	// " " + digits + " " + sign + " "
	return 1 + digits + 3
}

func digitCount(n int) int {
	if n < 10 {
		return 1
	}
	d := 0
	for n > 0 {
		d++
		n /= 10
	}
	return d
}

func (m *Model) ensureVisualLines(diffW int) []visualLine {
	textW := max(diffW-m.unifiedGutterWidth()-1, 1)
	if m.visGen == m.rowGen && m.visWrap == m.Wrap && m.visTextW == textW {
		return m.visLines
	}
	m.visLines = buildVisualLines(m.unified, m.Wrap, textW, m.TabSize)
	m.visGen = m.rowGen
	m.visWrap = m.Wrap
	m.visTextW = textW
	return m.visLines
}

func (m *Model) visualRowCount(diffW int) int {
	if m.EffectiveViewMode(diffW) == ViewSplit {
		return len(m.split)
	}
	return len(m.ensureVisualLines(diffW))
}

func (m *Model) visualRowOfCursor(diffW int) int {
	if m.EffectiveViewMode(diffW) == ViewSplit {
		return m.Cursor
	}
	vis := m.ensureVisualLines(diffW)
	for i, vl := range vis {
		if vl.rowIdx == m.Cursor {
			return i
		}
	}
	return 0
}

func (m *Model) ensureCursorVisible(diffW, viewRows int) {
	if viewRows <= 0 {
		return
	}
	vRow := m.visualRowOfCursor(diffW)
	if vRow < m.Scroll {
		m.Scroll = vRow
	} else if vRow >= m.Scroll+viewRows {
		m.Scroll = vRow - viewRows + 1
	}
	maxScroll := max(m.visualRowCount(diffW)-viewRows, 0)
	m.Scroll = min(max(m.Scroll, 0), maxScroll)
}

func (m *Model) centerCursor(diffW, viewRows int) {
	if viewRows <= 0 {
		return
	}
	vRow := m.visualRowOfCursor(diffW)
	maxScroll := max(m.visualRowCount(diffW)-viewRows, 0)
	m.Scroll = min(max(vRow-viewRows/3, 0), maxScroll)
}
