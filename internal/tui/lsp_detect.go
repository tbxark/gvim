package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// LSPServerState says what the configuration does with the language server of one language.
type LSPServerState int

// The states of the server of a language.
const (
	LSPServerNone        LSPServerState = iota // the configuration has no server for the language
	LSPServerOn                                // a server is configured: it starts when its program is installed
	LSPServerManual                            // a built-in server that does not start on its own
	LSPServerOffLanguage                       // turned off for this language ("enabled": false)
	LSPServerOffAll                            // turned off for every language (lsp_enabled)
)

// LSPServerInfo is what the port knows about the server of a language from the
// configuration alone, with no lookup of the program. The messages use it.
type LSPServerInfo struct {
	State    LSPServerState
	Language string // the name the configuration knows the language by
	Command  string // the program
	Install  string // how to get it, or empty
}

// lspDescriber is the optional part of LSPSession that describes the server of a language.
// The app adapter implements it. Without it the messages name only the config key.
type lspDescriber interface {
	Server(languageID string) LSPServerInfo
}

// lspFormatter is the optional part of LSPSession that formats a whole document: the
// textDocument/formatting request. text is the text of the document now, which the port
// sends to the server first if the server holds an older one, so the edits fit it. The edits
// are in rune columns of text.
type lspFormatter interface {
	Format(path, text string, tabSize int, insertSpaces bool) ([]editor.LSPEdit, error)
}

// LSPUnavailableError is what LSPSession.Open returns when no server can run for the
// language: the program is not installed, or it was launched and failed its handshake.
// The root remembers it for the language, so it does not start the program again for every
// document and every edit, and it shows the message once.
type LSPUnavailableError struct {
	Language string
	Command  string // the program, or the command line of a server that failed
	Install  string // how to get the program when it is missing
	Missing  bool   // the program is not on PATH
	Err      error  // the message: it names the server, the command and what the server said
}

// Error is the message of the failure.
func (e *LSPUnavailableError) Error() string { return e.Err.Error() }

// Unwrap returns the failure.
func (e *LSPUnavailableError) Unwrap() error { return e.Err }

// lspOpenFailed handles a document that could not be opened on its server. A server that
// cannot run for the language (LSPUnavailableError) is remembered, so no document of the
// language is sent again until lsp_restart, and its message is the notice the first time. Any
// other failure is the notice, and the next sync tries again.
func (m *Model) lspOpenFailed(tr *lspTrack, err error) {
	s := &m.lsp
	var ue *LSPUnavailableError
	if !errors.As(err, &ue) {
		m.notice = err.Error()
		return
	}
	if s.unavailable == nil {
		s.unavailable = make(map[string]*LSPUnavailableError)
	}
	s.unavailable[tr.lang] = ue
	s.stale = true // the documents of the language leave the tracks
	if s.noted[tr.lang] {
		return
	}
	if s.noted == nil {
		s.noted = make(map[string]bool)
	}
	s.noted[tr.lang] = true
	m.notice = ue.Error()
}

// lspNoServer is the notice for an LSP action on a language that has no running server. It
// says why: the program is missing (with the install command), it failed to start, the
// configuration turned it off, or it does not exist for the language.
func (m *Model) lspNoServer(lang string) string {
	s := &m.lsp
	if ue := s.unavailable[lang]; ue != nil {
		return ue.Error()
	}
	d, ok := s.port.(lspDescriber)
	if !ok {
		return noServerNotice(lang)
	}
	info := d.Server(lang)
	switch info.State {
	case LSPServerOffAll:
		return "Language servers are turned off: lsp_enabled is false in config.json (Start/Restart LSP Server starts this one anyway)"
	case LSPServerOffLanguage:
		return fmt.Sprintf("The language server for %s is turned off: remove \"enabled\": false from lsp.%s in config.json, or run Start/Restart LSP Server", lang, info.Language)
	case LSPServerManual:
		hint := ""
		if info.Install != "" {
			hint = " (" + info.Command + ": " + info.Install + ")"
		}
		return fmt.Sprintf("%s does not start on its own: run Start/Restart LSP Server, or set lsp.%s.auto_start to true in config.json%s", info.Command, info.Language, hint)
	}
	return noServerNotice(lang)
}

// lspFormat runs Format Buffer through the language server when the editor has no formatter
// for the buffer, as Fresh does: it formats with the formatter of the config and falls back
// to LSP formatting when none is set. It reports false when the editor has a formatter, or
// cannot say, and the editor's own action runs. Otherwise it returns the command that asks
// the server, or sets the notice that says why nothing can format the buffer.
func (m *Model) lspFormat() (tea.Cmd, bool) {
	fe, ok := m.ed.(formatterEditor)
	doc := m.ed.Active()
	if !ok || doc == nil || fe.HasFormatter() {
		return nil, false
	}
	lang := m.lspLanguage(doc)
	path := docPath(doc)
	fm, canFormat := m.lsp.port.(lspFormatter)
	tr := m.lsp.tracks[path]
	switch {
	case path == "":
		m.notice = "Format Buffer: no formatter is set for " + lang + ", and a language server needs a file name: save the file first"
	case !canFormat || !m.lsp.enabled(lang):
		m.notice = "Format Buffer: no formatter is set for " + lang + " (set formatters." + lang + " in config.json), and " + lowerFirst(m.lspNoServer(lang))
	case tr == nil || tr.state != lspOpen:
		m.notice = "Format Buffer: no formatter is set for " + lang + ", and the language server is not ready yet: try again in a moment"
	default:
		text := lspDocText(doc)
		tab, spaces := m.util.settings.TabSize, m.util.settings.InsertSpaces
		if tab <= 0 {
			tab = 4
		}
		return func() tea.Msg {
			edits, err := fm.Format(path, text, tab, spaces)
			return lspFormatMsg{path: path, text: text, edits: edits, err: err}
		}, true
	}
	return nil, true
}

// lowerFirst returns s with its first letter in lower case, for a message that continues a
// sentence.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// lspFormatMsg is the answer of a formatting request. text is the text the edits fit.
type lspFormatMsg struct {
	path, text string
	edits      []editor.LSPEdit
	err        error
}

// lspFormatted applies the edits of the server to the document they were made for. They fit
// the text that was sent, so they are dropped, with a notice, when the document changed
// since.
func (m *Model) lspFormatted(msg lspFormatMsg) {
	if msg.err != nil {
		m.notice = "Format Buffer: " + msg.err.Error()
		return
	}
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != msg.path {
		m.notice = "Format Buffer: the document changed while the server formatted it"
		return
	}
	if lspDocText(doc) != msg.text {
		m.notice = "Format Buffer: the text changed while the server formatted it: run it again"
		return
	}
	if len(msg.edits) == 0 {
		m.notice = "Format Buffer: the language server has nothing to change"
		return
	}
	if err := m.applyLSPEdits(msg.path, msg.edits); err != nil {
		m.notice = "Format Buffer: " + err.Error()
		return
	}
	m.syncViewport()
	m.notice = "Formatted with the language server"
}

// diagLine is the worst severity of the diagnostics that start on each line of a
// document, the lower the number the worse.
func diagLines(ds []Diagnostic) map[int]int {
	out := make(map[int]int, len(ds))
	for _, d := range ds {
		if sev, ok := out[d.Start.Line]; !ok || d.Severity < sev {
			out[d.Start.Line] = d.Severity
		}
	}
	return out
}

// diagGutterGlyph is the mark in the gutter of a line that has a diagnostic.
const diagGutterGlyph = "●"

// lspGutter marks the lines of doc that start a diagnostic with a dot in the gutter, in the
// color of the worst severity there. The mark takes the place of the change bar, and a mark
// that a plugin set on the same line gives way.
func (m *Model) lspGutter(pane editorpane.Pane, doc Document) editorpane.Pane {
	ds := m.lsp.diags[docPath(doc)]
	if len(ds) == 0 {
		return pane
	}
	lines := diagLines(ds)
	colors := map[int]grid.Style{
		editorpane.SeverityError:   {FG: m.panes.DiagError.FG},
		editorpane.SeverityWarning: {FG: m.panes.DiagWarning.FG},
		editorpane.SeverityInfo:    {FG: m.panes.DiagInfo.FG},
		editorpane.SeverityHint:    {FG: m.panes.DiagHint.FG},
	}
	inner := pane.Gutter
	pane.Gutter = func(line int) (editorpane.GutterMark, bool) {
		if sev, ok := lines[line]; ok {
			if st, ok := colors[sev]; ok && st.FG != nil {
				return editorpane.GutterMark{Text: diagGutterGlyph, Style: st}, true
			}
		}
		if inner != nil {
			return inner(line)
		}
		return editorpane.GutterMark{}, false
	}
	return pane
}

// diagLabel names a severity.
func diagLabel(sev int) string {
	switch sev {
	case editorpane.SeverityError:
		return "error"
	case editorpane.SeverityWarning:
		return "warning"
	case editorpane.SeverityInfo:
		return "info"
	}
	return "hint"
}

// diagText is a diagnostic as one line of text: its severity, its source and its message,
// with the message cut to its first line.
func diagText(d Diagnostic) string {
	msg, _, _ := strings.Cut(strings.TrimSpace(d.Message), "\n")
	out := diagLabel(d.Severity) + ": "
	if d.Source != "" {
		out += d.Source + ": "
	}
	return out + msg
}

// diagsAt returns the diagnostics of path that cover the position at, the most severe first.
// A diagnostic ends where its range ends, and an empty range covers its own position.
func (m *Model) diagsAt(path string, at buffer.Pos) []Diagnostic {
	var out []Diagnostic
	for _, d := range m.lsp.diags[path] {
		end := d.End
		if !d.Start.Less(end) {
			end = buffer.Pos{Line: d.Start.Line, Col: d.Start.Col + 1}
		}
		if !at.Less(d.Start) && at.Less(end) {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int { return a.Severity - b.Severity })
	return out
}

// diagPosition is "n/total" for the diagnostic d among the errors and warnings list.
func diagPosition(list []Diagnostic, d Diagnostic) string {
	i := slices.IndexFunc(list, func(o Diagnostic) bool { return o.Start == d.Start && o.Message == d.Message })
	return strconv.Itoa(i+1) + "/" + strconv.Itoa(len(list))
}

// lspShowsDiags reports whether the status row keeps a slot for the diagnostics counter:
// the active document is open on a server, or already has diagnostics. A document with no
// server, a language whose program is missing, a plain text file, shows no slot, so the
// message column keeps its width.
func (m *Model) lspShowsDiags() bool {
	doc := m.ed.Active()
	if m.lsp.port == nil || doc == nil {
		return false
	}
	if tr := m.lsp.tracks[docPath(doc)]; tr != nil && tr.state == lspOpen {
		return true
	}
	return m.lspDiagCounts().Any()
}

// lspCanFormat reports whether the language server of the active document can format it now:
// the document is open on a server whose port formats. It reads the root's own state only.
func (m *Model) lspCanFormat() bool {
	doc := m.ed.Active()
	if doc == nil {
		return false
	}
	if _, ok := m.lsp.port.(lspFormatter); !ok {
		return false
	}
	tr := m.lsp.tracks[docPath(doc)]
	return tr != nil && tr.state == lspOpen
}

// lspPill is the language-server pill of the status row for the active document. Its states
// follow Fresh's LSP indicator, in Fresh's priority order, with gvim's more precise wording
// where the program itself is the problem:
//
//	"LSP (n/a)"     the buffer-level skip (a large file, LSP off for the buffer) while the
//	                language's server runs for another buffer
//	"LSP (missing)" the program is not installed
//	"LSP (failed)"  the program started and failed its handshake
//	"LSP …"         the server is starting (Fresh shows a spinner here)
//	"LSP"           the document is open on its server
//	"LSP (off)"     a server is configured for the language but none runs: it is switched
//	                off, does not start on its own, or was stopped
//
// It is "" for a document whose language has no server. It reads the root's own state and
// the port's table only, so it can run on every frame.
func (m *Model) lspPill() string {
	s := &m.lsp
	doc := m.ed.Active()
	if s.port == nil || doc == nil || docPath(doc) == "" || isVirtualDoc(doc) {
		return ""
	}
	lang := m.lspLanguage(doc)
	path := docPath(doc)
	// Fresh's compose_lsp_status puts the per-buffer skip first, but only when it is a
	// mismatch with a running language: a document whose server was stopped shows the
	// language-level state ("off"), which is the more useful one.
	if (isLargeDoc(doc) || s.off[path]) && s.runningFor(lang) {
		return "LSP (n/a)"
	}
	if ue := s.unavailable[lang]; ue != nil {
		if ue.Missing {
			return "LSP (missing)"
		}
		return "LSP (failed)"
	}
	if s.enabled(lang) && !isLargeDoc(doc) && !s.off[path] {
		switch tr := s.tracks[path]; {
		case tr != nil && tr.state == lspOpen:
			return "LSP"
		case tr != nil:
			return "LSP …"
		}
	}
	// Nothing runs for the language. A configured server — switched off, dormant,
	// stopped, or built in but never started — is Fresh's "LSP (off)": it tells the
	// user there is a server to start, unlike a language with no server at all.
	if d, ok := s.port.(lspDescriber); ok {
		if info := d.Server(lang); info.State != LSPServerNone && info.Command != "" {
			return "LSP (off)"
		}
	}
	return ""
}

// runningFor reports whether the server of language lang is running: a document of the
// language is open on it, or its open is in flight. Fresh counts its Starting and
// Initializing states as running for the per-buffer skip.
func (s *lspState) runningFor(lang string) bool {
	for _, tr := range s.tracks {
		if tr.lang == lang && tr.state != lspClosed {
			return true
		}
	}
	return false
}
