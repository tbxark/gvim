package tui

import (
	"slices"
	"strings"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/split"
)

// stripTabs is the editor's tabs with the file type icon of the icon set filled in,
// for the strip to place and draw. Plain mode has no file icons, so the tabs pass
// through as they are.
func (m *Model) stripTabs() []TabInfo {
	tabs := m.ed.Tabs()
	plain := m.view.icons.FileIcon("", false, false) == ""
	pair := m.splits.comparing && !m.splits.Root.Leaf()
	if plain && !pair {
		return tabs
	}
	out := slices.Clone(tabs)
	for i := range out {
		if !plain {
			out[i].Icon = m.view.icons.FileIcon(strings.TrimSuffix(out[i].Name, frame.PreviewSuffix), false, false)
		}
	}
	if pair {
		// The two files of a comparison are two tabs, so the strip says they belong together.
		for _, n := range []*split.Node{m.splits.Root.A, m.splits.Root.B} {
			if n.Tab >= 0 && n.Tab < len(out) {
				out[n.Tab].Name = frame.ComparePairMark + " " + out[n.Tab].Name
			}
		}
	}
	return out
}
