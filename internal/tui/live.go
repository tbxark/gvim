package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// openLiveGrep shows the palette in Live Grep mode, with query as its text, and
// returns the command that searches it. Without a Grep port it shows a notice.
func (m *Model) openLiveGrep(query string) tea.Cmd {
	g := m.root.ports.Grep
	if g == nil {
		m.notice = "Live Grep is not supported in this port"
		return nil
	}
	p := palette.New(palette.Options{
		Query: query,
		Root:  m.rootDir(),
		Files: m.files,
		Live:  true,
		Grep:  g,
		Theme: m.theme,
	})
	m.pushOverlay(paletteOverlay{p: p})
	return p.Init()
}
