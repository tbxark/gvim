package tui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/dock"
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

// openLiveGrepResume opens Live Grep and hands the palette the previous controls when
// resume is not nil. The palette always runs the search again (Resume never restores
// cached hit text: file contents/remote state may have changed).
func (m *Model) openLiveGrepResume(query string, resume *palette.LiveResume) tea.Cmd {
	g := m.root.ports.Grep
	if g == nil {
		m.notice = "Live Grep is not supported in this port"
		return nil
	}
	// A remote launch keeps the remote root and joins rows with slashes; its Grep port is
	// the authority-backed adapter, and the preview port is the remote reader. The local
	// root/slash separator and the local previewer are never used for remote rows.
	root, remote := m.rootDir(), false
	if m.fileRoot != "" {
		root, remote = m.fileRoot, true
	}
	// The sheet is modal, so the buffers cannot change while it is open: one snapshot
	// of them serves every search. Without the port the Buffers scope is not offered.
	p := palette.New(palette.Options{
		Query:             query,
		Owner:             m.workspaceOwner,
		Resume:            resume,
		Root:              root,
		Remote:            remote,
		Files:             m.files,
		Live:              true,
		Grep:              g,
		Preview:           m.root.ports.Preview,
		LiveSearch:        m.liveSearchOptions(),
		SourceBuffers:     m.bufferSources(),
		SourceTerminals:   m.terminalSources(),
		SourceDiagnostics: m.diagnosticSources(),
		Theme:             m.theme,
	})
	m.pushOverlay(p)
	return p.Init()
}

// diagnosticSources snapshots the workspace diagnostics for Live Grep's Diagnostics
// scope. It reads the in-memory diagnostic store on the UI goroutine; a nil result means
// no diagnostic set was published, so the scope is not offered.
func (m *Model) diagnosticSources() []palette.DiagnosticSource {
	var out []palette.DiagnosticSource
	for _, file := range m.lsp.wsDiags {
		rel := m.relativePath(file.Path)
		for _, d := range file.Items {
			out = append(out, palette.DiagnosticSource{
				Path: file.Path, Rel: rel,
				Line: d.Start.Line + 1, Col: d.Start.Col + 1,
				Text: d.Message, Severity: d.Severity,
			})
		}
	}
	return out
}

// terminalSources snapshots the open dock terminals for Live Grep's Terminals scope.
// It runs on the UI goroutine and only reads in-memory terminal state (no IO, no PTY,
// no process is started): the dock supplies the transcript window and its combined-row
// provenance, and this applies Live Grep's snapshot caps (≤GrepTerminalsMaxTabs tabs,
// the newest ≤GrepTerminalsMaxRows / ≤GrepTerminalsMaxTabBytes per tab,
// ≤GrepTerminalsMaxBytes overall) while keeping First in step with the cropped tail. A
// nil result means there is no dock to search; a non-nil snapshot offers the scope.
// Only the newest transcript window is available: gvim has no Fresh-style
// closed-terminal backing file, so closed terminals are not searched.
func (m *Model) terminalSources() []palette.TerminalSource {
	tabs := m.dock.Transcripts()
	if len(tabs) == 0 {
		return nil
	}
	out := make([]palette.TerminalSource, 0, min(len(tabs), palette.GrepTerminalsMaxTabs))
	total := 0
	for i, tab := range tabs {
		if len(out) >= palette.GrepTerminalsMaxTabs || total >= palette.GrepTerminalsMaxBytes {
			break
		}
		lines, first := terminalSourceLines(tab.Lines, tab.First)
		size := 0
		for _, line := range lines {
			size += len(line) + 1
		}
		if total+size > palette.GrepTerminalsMaxBytes {
			break
		}
		total += size
		out = append(out, palette.TerminalSource{
			ID: tab.ID, Name: terminalSourceName(tab), Index: i, First: first, Total: tab.Total, Lines: lines,
		})
	}
	return out
}

// terminalSourceLines keeps the newest bounded transcript window of one dock tab:
// at most GrepTerminalsMaxRows rows and GrepTerminalsMaxTabBytes of text (the tail is
// nearest the live screen), advancing First by the rows dropped from the front so a
// matched line still maps to its combined scrollback row.
func terminalSourceLines(rows []string, first int) ([]string, int) {
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
	return append([]string(nil), rows[start:]...), first + start
}

// terminalSourceName labels a terminal hit row: the tab's explicit name, then its OSC
// title, then the recorded launch program's basename, then "terminal" (Fresh labels a
// nameless terminal "terminal" too).
func terminalSourceName(tab dock.TranscriptTab) string {
	if tab.Name != "" {
		return tab.Name
	}
	if tab.Title != "" {
		return tab.Title
	}
	if tab.Program != "" {
		return tab.Program
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
	rows := make([]list.Row, 0, len(msg.Hits)+1)
	// The exported list must not look complete when the search was truncated or degraded:
	// a header row states the adapter's notice (or the bare incomplete state) before the
	// hits, and the title carries the same signal.
	if msg.Notice != "" || msg.Truncated {
		notice := textsafe.Clean(msg.Notice)
		if notice == "" {
			notice = "result is incomplete"
		}
		rows = append(rows, list.Row{Header: true, Left: "… " + notice})
	}
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
	if msg.Truncated {
		title += " · incomplete"
	} else if msg.Notice != "" {
		title += " · notice"
	}
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
			return m.openFileCmd(liveGrepExportPath(msg.Root, name, msg.Remote), false, line, col)
		case "b":
			index, _ := strconv.Atoi(name)
			m.activateTab(index)
			m.gotoLine(line, col)
			m.focusEditor()
		case "d":
			line, _ := strconv.Atoi(lineStr)
			col, _ := strconv.Atoi(colStr)
			return m.lspGoto(LSPLocation{Path: name, Start: buffer.Pos{Line: line - 1, Col: col - 1}})
		case "T":
			// T carries the stable tab id, the absolute transcript row and a snapshot
			// payload (scrollback length + the expected line/neighbours), so a later
			// close/reorder/output drift cannot focus or land on a different terminal line.
			n, _ := strconv.ParseUint(name, 10, 64)
			row, _ := strconv.Atoi(lineStr)
			payload, valid := decodeTerminalExportPayload(colStr)
			return func() tea.Msg {
				return palette.OpenTerminalMsg{
					ID: n, Row: row, Total: payload.Total,
					Text: payload.Text, Before: payload.Before, After: payload.After,
					BeforeKnown: payload.BeforeKnown, AfterKnown: payload.AfterKnown,
					Snapshot: true, Valid: valid,
					Owner: m.workspaceOwner,
				}
			}
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
// terminalExportPayload is the snapshot context a terminal export row keeps so the
// quickfix callback can validate/relocate the transcript row later, exactly like the
// direct result. It is base64(JSON) in the value's fourth field, so tabs/newlines in
// terminal output can never break the wire format.
type terminalExportPayload struct {
	Total       int    `json:"total"`
	Text        string `json:"text"`
	Before      string `json:"before,omitempty"`
	After       string `json:"after,omitempty"`
	BeforeKnown bool   `json:"before_known,omitempty"`
	AfterKnown  bool   `json:"after_known,omitempty"`
}

// encodeTerminalExportPayload and decodeTerminalExportPayload are the wire pair for
// terminal quickfix rows; a decode failure is the legacy row-only form.
func encodeTerminalExportPayload(p terminalExportPayload) string {
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(data)
}

func decodeTerminalExportPayload(encoded string) (terminalExportPayload, bool) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return terminalExportPayload{}, false
	}
	var p terminalExportPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return terminalExportPayload{}, false
	}
	return p, true
}

func liveGrepExportValue(hit palette.GrepHit) string {
	switch hit.Source {
	case palette.GrepBuffers:
		return fmt.Sprintf("b	%d	%d	%d", hit.Index, hit.Line, hit.Col)
	case palette.GrepDiagnostics:
		return fmt.Sprintf("d\t%s\t%d\t%d", hit.Path, hit.Line, hit.Col)
	case palette.GrepTerminals:
		// T, stable dock tab id, absolute transcript row, snapshot payload. The callback
		// focuses and positions the existing dock tab (never a file or a shell); the id
		// survives closes/reorders and the payload validates/relocates the row.
		payload := encodeTerminalExportPayload(terminalExportPayload{
			Total: hit.TerminalTotal, Text: hit.Text,
			Before: hit.TerminalBefore, After: hit.TerminalAfter,
			BeforeKnown: hit.TerminalBeforeKnown, AfterKnown: hit.TerminalAfterKnown,
		})
		return fmt.Sprintf("T	%d	%d	%s", hit.TerminalID, hit.TranscriptFirst+hit.Line-1, payload)
	default:
		return fmt.Sprintf("f	%s	%d	%d", hit.Rel, hit.Line, hit.Col)
	}
}

// liveGrepExportPath joins an exported file hit's relative name to the palette root:
// remote rows keep the remote '/' separator and are opened through the remote document
// access; local rows use the platform join. It never resolves a remote name against a
// local path.
func liveGrepExportPath(root, name string, remote bool) string {
	if remote {
		return strings.TrimSuffix(root, "/") + "/" + strings.TrimPrefix(name, "/")
	}
	return filepath.Join(root, name)
}

// parseLiveGrepExportValue decodes a liveGrepExportValue.
func parseLiveGrepExportValue(value string) (kind, name, line, col string, ok bool) {
	parts := strings.SplitN(value, "\t", 4)
	if len(parts) != 4 {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[2], parts[3], true
}
