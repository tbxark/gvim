package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
)

// BufferText is one open buffer's text, as the root snapshots it for Live Grep's
// Buffers scope. The editor adapter implements bufferTextsEditor over the editor.
type BufferText struct {
	Name string // the tab's label
	Path string // the buffer's file, when it has one
	Text string // the buffer's text, LF-normalised
	// Partial reports that the buffer is longer than BufferTextBytes and Text is only
	// its head, so a miss in the rest is possible.
	Partial bool
}

// BufferTextBytes is the most text one buffer contributes to Live Grep's Buffers
// scope: 1 MiB. A longer buffer is searched at its head only.
const BufferTextBytes = 1 << 20

// bufferTextsEditor is the optional editor port that snapshots the open buffers for
// Live Grep's Buffers scope. The app adapter implements it. An editor without it leaves
// the scope unavailable: its toggle is not painted and it stays off.
type bufferTextsEditor interface {
	// BufferTexts returns the text of every open buffer, in tab order, each at most
	// BufferTextBytes. An error means the snapshot could not be taken.
	BufferTexts() ([]BufferText, error)
}

// openLiveGrep shows the palette in Live Grep mode, with query as its text, and
// returns the command that searches it. Without a Grep port it shows a notice.
func (m *Model) openLiveGrep(query string) tea.Cmd {
	g := m.root.ports.Grep
	if g == nil {
		m.notice = "Live Grep is not supported in this port"
		return nil
	}
	// The sheet is modal, so the buffers cannot change while it is open: one snapshot
	// of them serves every search. Without the port the Buffers scope is not offered.
	p := palette.New(palette.Options{
		Query:         query,
		Root:          m.rootDir(),
		Files:         m.files,
		Live:          true,
		Grep:          g,
		LiveSearch:    m.liveSearchOptions(),
		SourceBuffers: m.bufferSources(),
		Theme:         m.theme,
	})
	m.pushOverlay(p)
	return p.Init()
}

// liveSearchOptions is the starting state of Live Grep's match modes: case sensitivity
// comes from editor.search.case_sensitive, regex starts on as Fresh's Live Grep does,
// and whole word starts off.
func (m *Model) liveSearchOptions() *palette.SearchOptions {
	return &palette.SearchOptions{
		CaseSensitive: m.view.opts.Search.CaseSensitive,
		Regex:         true,
		WholeWord:     false,
	}
}

// bufferSources snapshots the open buffers for Live Grep's Buffers scope, or nil when
// the editor does not offer them. The indices line up with palette.Options.Buffers, so
// a hit can activate its tab.
func (m *Model) bufferSources() []palette.BufferSource {
	ed, ok := m.ed.(bufferTextsEditor)
	if !ok {
		return nil
	}
	texts, err := ed.BufferTexts()
	if err != nil {
		return nil
	}
	out := make([]palette.BufferSource, len(texts))
	for i, t := range texts {
		out[i] = palette.BufferSource{Name: t.Name, Path: t.Path, Index: i, Text: t.Text}
	}
	return out
}
