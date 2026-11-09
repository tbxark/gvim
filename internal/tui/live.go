package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/textsafe"
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
	return m.openLiveGrepResume(query, nil)
}

// openLiveGrepResume opens Live Grep and, when resume is not nil, hands the palette
// the previous session's cached state. The palette restores the cached rows only if
// the file-index generation still matches; otherwise it keeps the query/modes/scopes
// and runs the search in a tea.Cmd as usual.
func (m *Model) openLiveGrepResume(query string, resume *palette.LiveResume) tea.Cmd {
	if m.fileRoot != "" {
		// A remote launch: the Grep port is the local live grep and the palette root
		// would be the local process root, so searching here would silently search the
		// LOCAL project for remote-looking rows. Refuse explicitly until a remote
		// search through the authority is wired (docs/architecture.md §8.1).
		m.notice = "Live Grep searches the local workspace; it is not available in a remote workspace yet"
		return nil
	}
	g := m.root.ports.Grep
	if g == nil {
		m.notice = "Live Grep is not supported in this port"
		return nil
	}
	// The sheet is modal, so the buffers cannot change while it is open: one snapshot
	// of them serves every search. Without the port the Buffers scope is not offered.
	p := palette.New(palette.Options{
		Query:           query,
		Resume:          resume,
		Root:            m.rootDir(),
		Files:           m.files,
		Live:            true,
		Grep:            g,
		LiveSearch:      m.liveSearchOptions(),
		SourceBuffers:   m.bufferSources(),
		SourceTerminals: m.terminalSources(),
		Theme:           m.theme,
	})
	m.pushOverlay(p)
	return p.Init()
}

// terminalSources snapshots the open dock terminals for Live Grep's Terminals scope.
// It runs on the UI goroutine and only reads in-memory terminal state (no IO, no PTY,
// no process is started): Model.TerminalState already bounds each tab's transcript to
// terminal.SavedState's limits, and this applies Live Grep's smaller total snapshot
// caps as well (≤GrepTerminalsMaxTabs tabs, the newest ≤GrepTerminalsMaxRows /
// ≤GrepTerminalsMaxTabBytes per tab, ≤GrepTerminalsMaxBytes overall). A nil result
// means there is no dock to search; a non-nil (possibly empty) snapshot offers the
// scope. Only the newest transcript window is available: gvim has no Fresh-style
// closed-terminal backing file, so closed terminals are not searched.
func (m *Model) terminalSources() []palette.TerminalSource {
	st := m.TerminalState()
	if len(st.Tabs) == 0 {
		return nil
	}
	out := make([]palette.TerminalSource, 0, min(len(st.Tabs), palette.GrepTerminalsMaxTabs))
	total := 0
	for i, tab := range st.Tabs {
		if len(out) >= palette.GrepTerminalsMaxTabs || total >= palette.GrepTerminalsMaxBytes {
			break
		}
		lines := terminalSourceLines(tab.Rows)
		size := 0
		for _, line := range lines {
			size += len(line) + 1
		}
		if total+size > palette.GrepTerminalsMaxBytes {
			break
		}
		total += size
		out = append(out, palette.TerminalSource{Name: terminalSourceName(tab), Index: i, Lines: lines})
	}
	return out
}

// terminalSourceLines keeps the newest bounded transcript window of one dock tab:
// at most GrepTerminalsMaxRows rows and GrepTerminalsMaxTabBytes of text (the tail is
// nearest the live screen).
func terminalSourceLines(rows []string) []string {
	start, count, size := len(rows), 0, 0
	for start > 0 && count < palette.GrepTerminalsMaxRows {
		line := rows[start-1]
		if size+len(line)+1 > palette.GrepTerminalsMaxTabBytes {
			break
		}
		size += len(line) + 1
		count++
		start--
	}
	return append([]string(nil), rows[start:]...)
}

// terminalSourceName labels a terminal hit row: the tab's explicit name, then its OSC
// title, then the recorded launch program's basename, then "terminal" (Fresh labels a
// nameless terminal "terminal" too).
func terminalSourceName(tab terminal.SavedTab) string {
	if tab.Name != "" {
		return tab.Name
	}
	if tab.Title != "" {
		return tab.Title
	}
	if len(tab.LaunchArgv) > 0 {
		program := tab.LaunchArgv[0]
		if i := strings.LastIndexAny(program, "/\\"); i >= 0 {
			program = program[i+1:]
		}
		if program != "" {
			return program
		}
	}
	return "terminal"
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

// openLiveGrepResultsList opens a Live Grep export as the same filterable list overlay
// the project-search export uses: Enter jumps to a row and keeps the list open, so the
// set can be walked; Esc closes it. This is gvim's mapping of Fresh's Export to
// Quickfix (a shared list, not a persistent quickfix stack). The palette caps the rows;
// a capped export carries a notice here.
func (m *Model) openLiveGrepResultsList(msg palette.GrepExportMsg) tea.Cmd {
	if len(msg.Hits) == 0 {
		m.notice = uiText("gvim.palette.live_export_empty", "No Live Grep results to export")
		return nil
	}
	rows := make([]list.Row, 0, len(msg.Hits))
	for _, hit := range msg.Hits {
		// Only the display fields are sanitized; Value keeps the raw path/index the row
		// action opens or focuses, so a cleaned label can never redirect the target.
		label := textsafe.Clean(hit.Rel)
		text := textsafe.Clean(strings.ReplaceAll(hit.Text, "\t", " "))
		rows = append(rows, list.Row{
			Left:  fmt.Sprintf("%s:%d  %s", label, hit.Line, strings.TrimSpace(text)),
			Right: filepath.Base(label),
			Value: liveGrepExportValue(hit),
		})
	}
	title := fmt.Sprintf("Live Grep: %s (%d matches)", textsafe.Clean(msg.Query), len(msg.Hits))
	v := list.New(title, rows, m.listChrome(), func(_ *list.View, r list.Row) tea.Cmd {
		kind, name, lineStr, colStr, ok := parseLiveGrepExportValue(r.Value)
		if !ok {
			return nil
		}
		line, _ := strconv.Atoi(lineStr)
		col, _ := strconv.Atoi(colStr)
		switch kind {
		case "f":
			m.focusEditor()
			return m.openFileCmd(filepath.Join(msg.Root, name), false, line, col)
		case "b":
			index, _ := strconv.Atoi(name)
			m.activateTab(index)
			m.gotoLine(line, col)
			m.focusEditor()
		case "t":
			// The four fields are t, dock tab index, label, transcript line: only the
			// tab index locates the terminal. The list stays open for the next row.
			index, _ := strconv.Atoi(name)
			return func() tea.Msg { return palette.OpenTerminalMsg{Index: index} }
		}
		return nil
	})
	m.pushOverlay(v)
	if msg.Total > len(msg.Hits) {
		m.notice = uiText("gvim.palette.live_export_truncated", "Live Grep export limited to the first 100 matches")
	}
	return nil
}

// liveGrepExportValue encodes one hit for the list row: kind, name, line and column,
// tab separated (the path is kept as the second field, the positions as numbers).
func liveGrepExportValue(hit palette.GrepHit) string {
	switch hit.Source {
	case palette.GrepBuffers:
		return fmt.Sprintf("b\t%d\t%d\t%d", hit.Index, hit.Line, hit.Col)
	case palette.GrepTerminals:
		// t, dock tab index, terminal label, transcript line. The callback focuses the
		// existing dock tab; it never opens a file or starts a shell.
		return fmt.Sprintf("t\t%d\t%s\t%d", hit.Index, hit.Rel, hit.Line)
	default:
		return fmt.Sprintf("f\t%s\t%d\t%d", hit.Rel, hit.Line, hit.Col)
	}
}

// parseLiveGrepExportValue decodes a liveGrepExportValue.
func parseLiveGrepExportValue(value string) (kind, name, line, col string, ok bool) {
	parts := strings.SplitN(value, "\t", 4)
	if len(parts) != 4 {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[2], parts[3], true
}
