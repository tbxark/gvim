package ui

// hitKind names the part of the screen under the pointer.
type hitKind int

const (
	hitNone hitKind = iota
	hitMenuLabel
	hitTab
	hitTabClose
	hitTabNew
	hitTabScrollBefore // the "<" marker: scrolls the tab strip one tab left
	hitTabScrollAfter  // the ">" marker: scrolls the tab strip one tab right
	hitExplorerRow
	hitExplorerTitleReload
	hitExplorerTitleClose
	hitExplorerGrip
	hitExplorerBody
	hitEditorText
	hitGutter
	hitScrollbarTrack
	hitStatusPalette
	hitStatusSegment
	hitWelcomeRow // a row of the welcome screen: a door or a recent folder
)

// hitTarget is the result of a hit test. Index depends on Kind. Menu labels,
// tabs, tab closes and welcome rows use their list index. Explorer rows, the editor text,
// the gutter and the scrollbar use the row offset from the top of their area:
// body row 0 is screen row 2 for the editor and the explorer. The caller maps
// that offset to a line or a tree row.
type hitTarget struct {
	Kind  hitKind
	Index int
}

// hit returns what is at column x, row y. Targets are checked in paint order,
// top-most first: status row, menu bar, tab strip, explorer, editor. Under the
// gate there are no targets.
func (l layout) hit(x, y int) hitTarget {
	none := hitTarget{Kind: hitNone}
	if l.gated || x < 0 || y < 0 || x >= l.w || y >= l.h {
		return none
	}
	if y == l.statusY {
		return hitRuns(l.status, x, none)
	}
	if y == 0 {
		return hitRuns(l.menuRuns, x, none)
	}
	if y == 1 {
		if l.explorerOpen && x < l.ex {
			return l.hitExplorerTitle(x)
		}
		return hitRuns(l.strip.runs, x, none)
	}
	if l.explorerOpen && x < l.ex && y <= l.exBottom {
		return l.hitExplorer(x, y)
	}
	if y >= l.bodyTop && y <= l.bodyBottom && x >= l.editorX {
		if l.welcomeOn {
			return l.hitWelcome(x, y)
		}
		return l.hitEditor(x, y)
	}
	return none
}

// hitRuns returns the target of the run under column x, or def when none covers it.
func hitRuns(runs []run, x int, def hitTarget) hitTarget {
	for _, r := range runs {
		if r.covers(x) {
			return hitTarget{Kind: r.kind, Index: r.index}
		}
	}
	return def
}

// hitExplorerTitle returns the title buttons. The rest of row 1 in the panel is
// the panel body.
func (l layout) hitExplorerTitle(x int) hitTarget {
	switch {
	case l.exReloadX >= 0 && x == l.exReloadX:
		return hitTarget{Kind: hitExplorerTitleReload}
	case l.exCloseX >= 0 && x == l.exCloseX:
		return hitTarget{Kind: hitExplorerTitleClose}
	}
	return hitTarget{Kind: hitExplorerBody}
}

// hitExplorer covers the panel below the title: the grip on the right border,
// the rows inside the border, and the empty body.
func (l layout) hitExplorer(x, y int) hitTarget {
	switch {
	case x == l.ex-1:
		return hitTarget{Kind: hitExplorerGrip}
	case x == 0 || y == l.exBottom:
		return hitTarget{Kind: hitExplorerBody}
	}
	if row := y - l.bodyTop; row < l.exRows {
		return hitTarget{Kind: hitExplorerRow, Index: row}
	}
	return hitTarget{Kind: hitExplorerBody}
}

// hitEditor covers the editor body: the gutter, the text, and the last column
// when the content overflows. The last column reports the track here.
func (l layout) hitEditor(x, y int) hitTarget {
	row := y - l.bodyTop
	switch {
	case x < l.textX:
		return hitTarget{Kind: hitGutter, Index: row}
	case l.scrollbar && x == l.w-1:
		return hitTarget{Kind: hitScrollbarTrack, Index: row}
	}
	return hitTarget{Kind: hitEditorText, Index: row}
}

// hitWelcome covers the welcome screen in the editor pane: its rows are targets,
// and the rest of the pane is not.
func (l layout) hitWelcome(x, y int) hitTarget {
	for _, r := range l.welcome {
		if r.item >= 0 && r.y == y && x >= r.x && x < r.x+r.w {
			return hitTarget{Kind: hitWelcomeRow, Index: r.item}
		}
	}
	return hitTarget{Kind: hitNone}
}
