package ui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui/editorpane"
)

// The language-server part of the plugin host (stage S23): the diagnostics a plugin reads
// (gvim.lsp.diagnostics), the requests it sends (gvim.lsp.request) and the
// diagnostics_updated event. Both read the state the root keeps for its own LSP features
// (lspState), so a plugin sees what the user sees.

// maxPluginDiagnostics bounds the list one gvim.lsp.diagnostics call returns.
const maxPluginDiagnostics = 5000

// waitLSP is the wait kind of a request that Host.LSPRequest started.
const waitLSP = "lsp"

// pluginLSPMsg is the answer of a request Host.LSPRequest sent. c carries the wait token.
type pluginLSPMsg struct{ c pluginapi.Completion }

// plugSeverity names an LSP severity number the way a plugin sees it.
func plugSeverity(n int) string {
	switch n {
	case editorpane.SeverityError:
		return "error"
	case editorpane.SeverityWarning:
		return "warning"
	case editorpane.SeverityInfo:
		return "info"
	}
	return "hint"
}

// Diagnostics returns the diagnostics the servers published for buffer id, with byte
// columns. A buffer without a file, or one no server has reported on, has none.
func (h *pluginHost) Diagnostics(id pluginapi.BufferID) ([]pluginapi.DiagnosticInfo, error) {
	m := h.m
	if m.lsp.port == nil {
		return nil, pluginapi.Errf(pluginapi.CodeUnsupported, "no language server is configured")
	}
	info, ok := h.ed().Buffer(id)
	if !ok {
		return nil, pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	items := m.lsp.diags[info.Path]
	if info.Path == "" || len(items) == 0 {
		return nil, nil
	}
	items = items[:min(len(items), maxPluginDiagnostics)]
	lines := map[int]string{}
	byteCol := func(line, col int) (int, error) {
		text, ok := lines[line]
		if !ok {
			got, err := h.ed().Lines(id, line, 1)
			if err != nil {
				return 0, err
			}
			if len(got) > 0 {
				text = got[0]
			}
			lines[line] = text
		}
		return runeToByteCol(text, col), nil
	}
	out := make([]pluginapi.DiagnosticInfo, 0, len(items))
	for _, d := range items {
		fc, err := byteCol(d.Start.Line, d.Start.Col)
		if err != nil {
			return nil, err
		}
		tc, err := byteCol(d.End.Line, d.End.Col)
		if err != nil {
			return nil, err
		}
		out = append(out, pluginapi.DiagnosticInfo{
			Range:    pluginapi.Range{From: pluginapi.Pos{Line: d.Start.Line, Col: fc}, To: pluginapi.Pos{Line: d.End.Line, Col: tc}},
			Severity: plugSeverity(d.Severity),
			Message:  d.Message,
			Source:   d.Source,
		})
	}
	return out, nil
}

// runeToByteCol converts a rune column of line to a byte column. A column past the end of
// the line is the end of the line.
func runeToByteCol(line string, col int) int {
	if col <= 0 {
		return 0
	}
	n := 0
	for i := range line {
		if n == col {
			return i
		}
		n++
	}
	return len(line)
}

// LSPRequest sends a request of the protocol for buffer id to the server that has it open.
// It validates and defers: the request runs in a command, and the answer resolves t. The
// buffer must be a file that a server has open, since the server answers about the text it
// holds.
func (h *pluginHost) LSPRequest(t pluginapi.Token, id pluginapi.BufferID, method string, params any) error {
	m := h.m
	port := m.lsp.port
	if port == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "no language server is configured")
	}
	info, ok := h.ed().Buffer(id)
	switch {
	case !ok:
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	case info.Path == "" || info.Virtual:
		return pluginapi.Errf(pluginapi.CodeInvalid, "buffer %d has no file, so no language server has it", id)
	}
	path := info.Path
	if tr := m.lsp.tracks[path]; tr == nil || tr.state != lspOpen {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no language server has %s open", path)
	}
	if err := m.plugWait(t, waitLSP); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd {
		return func() tea.Msg {
			res, err := port.Request(path, method, params)
			c := pluginapi.Completion{Token: t}
			var coded *pluginapi.Error
			switch {
			case errors.As(err, &coded):
				c.Err = coded.Error()
			case err != nil:
				c.Err = string(pluginapi.CodeIO) + ": " + err.Error()
			default:
				c.Values = []any{res}
			}
			return pluginLSPMsg{c: c}
		}
	})
	return nil
}

// plugDiagnosticsChanged raises diagnostics_updated for a document whose diagnostics
// changed, with the counts per severity. old is what the root held before.
func (m *Model) plugDiagnosticsChanged(path string, old, items []Diagnostic) {
	p := &m.plug
	if p.rt == nil || p.pe == nil || slices.Equal(old, items) {
		return
	}
	id := p.pe.FindBuffer(path)
	if id == 0 {
		return
	}
	counts := map[string]any{"error": 0, "warning": 0, "info": 0, "hint": 0}
	for _, d := range items {
		k := plugSeverity(d.Severity)
		counts[k] = counts[k].(int) + 1
	}
	m.plugEmit("diagnostics_updated", map[string]any{"buf": id, "path": path, "counts": counts})
}

// pluginLSPDone delivers the answer of a request to the plugin that is waiting for it.
func (m *Model) pluginLSPDone(msg pluginLSPMsg) tea.Cmd { return m.plugFinish(msg.c) }
