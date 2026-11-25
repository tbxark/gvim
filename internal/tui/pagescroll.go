package tui

import (
	"slices"

	"github.com/tbxark/gvim/internal/tui/list"
)

// pageScroll is the selection-plus-scroll state shared by the settings, settings
// structure and theme editor pages: sel is the highlighted row, top the first
// drawn row and page the rows the last draw fit (0 before it).
type pageScroll struct {
	sel  int
	top  int
	page int
}

// pageCount is the number of rows the last draw showed, for PageUp and PageDown.
func (s *pageScroll) pageCount() int {
	if s.page > 0 {
		return s.page
	}
	return list.DefaultPage
}

// keepVisible scrolls so that sel is inside the page over n rows.
func (s *pageScroll) keepVisible(n int) {
	page := s.pageCount()
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+page {
		s.top = s.sel - page + 1
	}
	s.top = max(0, min(s.top, n-page))
}

// pageRow is a page row that can be a section header.
type pageRow interface {
	isHeader() bool
}

// moveSel shifts sel by d rows, stopping at the ends and skipping headers, and
// scrolls to keep the selection visible.
func moveSel[T pageRow](s *pageScroll, d int, rows []T) {
	step, n := 1, d
	if d < 0 {
		step, n = -1, -d
	}
	for range n {
		next := s.sel + step
		for next >= 0 && next < len(rows) && rows[next].isHeader() {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		s.sel = next
	}
	s.keepVisible(len(rows))
}

// firstRowOf is the index of the first non-header row, or 0.
func firstRowOf[T pageRow](rows []T) int {
	for i, row := range rows {
		if !row.isHeader() {
			return i
		}
	}
	return 0
}

// lastRowOf is the index of the last non-header row, or 0.
func lastRowOf[T pageRow](rows []T) int {
	for i, row := range slices.Backward(rows) {
		if !row.isHeader() {
			return i
		}
	}
	return 0
}
