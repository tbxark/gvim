package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
)

// bookmarksOverlay is the picker overlay for List Bookmarks.
type bookmarksOverlay struct {
	p     *picker.Picker
	slots []string // slot "0".."9" indexed by item ID
}

func (o *bookmarksOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := o.cmdFor(o.p.Update(msg))
	if !o.p.Active() {
		return nil, cmd
	}
	return o, cmd
}

func (o *bookmarksOverlay) Draw(g *grid.Grid) { o.p.Draw(g) }
func (o *bookmarksOverlay) Active() bool      { return o.p.Active() }

// ExclusiveOverlay marks the picker as a transient interaction. See
// overlay.Exclusive.
func (*bookmarksOverlay) ExclusiveOverlay() {}

type (
	bookmarkPickMsg struct{ slot string }
	bookmarkRankMsg struct {
		ov  *bookmarksOverlay
		msg tea.Msg
	}
)

func (o *bookmarksOverlay) cmdFor(r picker.Result) tea.Cmd {
	var cmds []tea.Cmd
	if r.Cmd != nil {
		cmds = append(cmds, r.Cmd)
	}
	if r.Rank != nil {
		rank := r.Rank
		cmds = append(cmds, func() tea.Msg { return bookmarkRankMsg{ov: o, msg: rank()} })
	}
	for _, e := range r.Events {
		if c, ok := e.(picker.Chosen); ok && c.ID >= 0 && c.ID < len(o.slots) {
			slot := o.slots[c.ID]
			cmds = append(cmds, func() tea.Msg { return bookmarkPickMsg{slot: slot} })
		}
	}
	return tea.Batch(cmds...)
}

// openBookmarksPicker opens the interactive picker of List Bookmarks, or sets a notice
// when no bookmarks are set or the editor does not list bookmarks.
func (m *Model) openBookmarksPicker() tea.Cmd {
	bme, ok := m.ed.(BookmarkEditor)
	if !ok {
		m.notice = "Bookmarks are not supported by the editor"
		return nil
	}
	bms := bme.Bookmarks()
	if len(bms) == 0 {
		m.notice = "No bookmarks set"
		return nil
	}
	items := make([]picker.Item, len(bms))
	slots := make([]string, len(bms))
	for i, bm := range bms {
		slots[i] = bm.Slot
		text := fmt.Sprintf("[%s] %s:%d:%d", bm.Slot, bm.Name, bm.Line, bm.Col)
		snippet := strings.TrimSpace(bm.Snippet)
		items[i] = picker.Item{
			ID:     i,
			Text:   text,
			Detail: snippet,
		}
	}
	p := picker.New(m.styles, picker.Options{
		Title:       "Bookmarks",
		Footer:      "Enter: jump    Esc: close",
		Placeholder: "Filter bookmarks",
		Items:       items,
		Fuzzy:       true,
	})
	ov := &bookmarksOverlay{p: p, slots: slots}
	m.pushOverlay(ov)
	return ov.cmdFor(p.Init())
}
