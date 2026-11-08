package frame

// HitKind names the part of the screen under the pointer.
type HitKind int

const (
	HitNone HitKind = iota
	HitMenuLabel
	HitTab
	HitTabClose
	HitTabNew
	HitTabScrollBefore // the "<" marker: scrolls the tab strip one tab left
	HitTabScrollAfter  // the ">" marker: scrolls the tab strip one tab right
	HitExplorerRow
	HitExplorerTitleReload
	HitExplorerTitleClose
	HitExplorerGrip
	HitExplorerBody
	HitEditorText
	HitGutter
	HitScrollbarTrack
	HitStatusPalette
	HitStatusSegment
	HitWelcomeRow // a row of the welcome screen: a door or a recent folder

	// HitSplitBorder is the line between two editor panes. Index is its index in
	// Layout.borders.
	HitSplitBorder

	// HitStatusPlugin is a clickable status item of a plugin. Index is its place in the
	// list plugStatusItems returns.
	HitStatusPlugin
)

// The Index of a hitStatusSegment target: the line ending, the encoding, the language or
// the caret position of the status row.
const (
	StatusSegmentEOL = iota
	StatusSegmentEncoding
	StatusSegmentLanguage
	StatusSegmentPosition // the line and column of the caret: opens Go to Line
	StatusSegmentLSP      // the state of the language server: shows its status
)

// HitTarget is the result of a hit test. Index depends on Kind. Menu labels,
// tabs, tab closes and welcome rows use their list index. Explorer rows, the editor text,
// the gutter and the scrollbar use the row offset from the top of their pane. A pane's
// row 0 is its top row, and explorer tree row 0 is screen row Layout.ExBodyTop. The
// caller maps that offset to a line or a tree row. Pane is the index of the editor pane
// under a press, into Layout.panes.
type HitTarget struct {
	Kind  HitKind
	Index int
	Pane  int
}

// Hit returns what is at column x, row y. Targets are checked in paint order,
// top-most first: status row, menu bar, explorer title, tab strip, explorer,
// editor. Under the gate there are no targets.
func (l Layout) Hit(x, y int) HitTarget {
	none := HitTarget{Kind: HitNone}
	if l.Gated || x < 0 || y < 0 || x >= l.W || y >= l.H {
		return none
	}
	if l.StatusOn && y == l.StatusY {
		return hitRuns(l.Status, x, none)
	}
	if l.OnNotice(x, y) {
		return none // the notice band covers the body rows under it
	}
	if l.MenuOn && y == l.MenuLabelY {
		return hitRuns(l.MenuRuns, x, none)
	}
	if l.ExplorerOpen && x < l.Ex && y == l.ExTitleY {
		return l.hitExplorerTitle(x)
	}
	if y == l.StripY {
		return hitRuns(l.Strip.Runs, x, none)
	}
	if l.ExplorerOpen && x < l.Ex && y >= l.ExBodyTop && y <= l.ExBottom {
		return l.hitExplorer(x, y)
	}
	if y >= l.BodyTop && y <= l.BodyBottom && x >= l.EditorX {
		if l.WelcomeOn {
			return l.hitWelcome(x, y)
		}
		return l.hitEditor(x, y)
	}
	return none
}

// hitRuns returns the target of the Run under column x, or def when none covers it.
func hitRuns(runs []Run, x int, def HitTarget) HitTarget {
	for _, r := range runs {
		if r.Covers(x) {
			return HitTarget{Kind: r.Kind, Index: r.Index}
		}
	}
	return def
}

// hitExplorerTitle returns the title buttons. The rest of the title row in the
// panel is the panel body.
func (l Layout) hitExplorerTitle(x int) HitTarget {
	switch {
	case l.ExReloadX >= 0 && x == l.ExReloadX:
		return HitTarget{Kind: HitExplorerTitleReload}
	case l.ExCloseX >= 0 && x == l.ExCloseX:
		return HitTarget{Kind: HitExplorerTitleClose}
	}
	return HitTarget{Kind: HitExplorerBody}
}

// hitExplorer covers the panel below the title: the grip on the right border,
// the rows inside the border, and the empty body.
func (l Layout) hitExplorer(x, y int) HitTarget {
	switch {
	case x == l.Ex-1:
		return HitTarget{Kind: HitExplorerGrip}
	case x == 0 || y == l.ExBottom:
		return HitTarget{Kind: HitExplorerBody}
	}
	if row := y - l.ExBodyTop; row >= 0 && row < l.ExRows {
		return HitTarget{Kind: HitExplorerRow, Index: row}
	}
	return HitTarget{Kind: HitExplorerBody}
}

// hitEditor covers the editor panes: the gutter, the text, and the last column of a
// pane whose content overflows, which reports the track. A line between two panes is
// hitSplitBorder and takes no press.
func (l Layout) hitEditor(x, y int) HitTarget {
	for i, p := range l.Panes {
		if !p.Contains(x, y) {
			continue
		}
		row := y - p.Y
		switch {
		case x < p.TextX:
			return HitTarget{Kind: HitGutter, Index: row, Pane: i}
		case p.Scrollbar && x == p.X+p.W-1:
			return HitTarget{Kind: HitScrollbarTrack, Index: row, Pane: i}
		}
		return HitTarget{Kind: HitEditorText, Index: row, Pane: i}
	}
	for i, b := range l.Borders {
		if b.Contains(x, y) {
			return HitTarget{Kind: HitSplitBorder, Index: i}
		}
	}
	return HitTarget{Kind: HitNone}
}

// hitWelcome covers the welcome screen in the editor pane: its rows are targets,
// and the rest of the pane is not.
func (l Layout) hitWelcome(x, y int) HitTarget {
	for _, r := range l.Welcome {
		if r.Item >= 0 && r.Y == y && x >= r.X && x < r.X+r.W {
			return HitTarget{Kind: HitWelcomeRow, Index: r.Item}
		}
	}
	return HitTarget{Kind: HitNone}
}

// onNotice reports whether cell (x, y) is on the notice band, the rows that a long
// status message covers above the status row. The band starts at the editor's left edge,
// so the explorer beside it is not covered.
func (l Layout) OnNotice(x, y int) bool {
	return !l.Gated && len(l.Notice) > 0 && x >= l.NoticeX && x < l.W && y >= l.NoticeY && y < l.StatusY
}

// BandTop is the first row the notice band covers, or the frame height when there is
// no band.
func (l Layout) BandTop() int {
	if len(l.Notice) == 0 {
		return l.H
	}
	return l.NoticeY
}
