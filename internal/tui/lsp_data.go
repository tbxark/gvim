package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
)

// The LSP data refreshes are debounced: an edit, an open and a viewport move arm a
// timer, and only the newest timer asks the servers. The values are variables so
// tests can shorten them.
var (
	lspHintDebounce  = 150 * time.Millisecond
	lspTokenDebounce = 100 * time.Millisecond
)

// lspDataMsg arms one LSP data refresh of the active document. seq orders the
// timers of a kind: an older timer that fires late does nothing.
type lspDataMsg struct {
	kind lspDataKind
	seq  int
}

// lspDataKind is the data a refresh asks for.
type lspDataKind int

const (
	lspDataHints lspDataKind = iota
	lspDataTokens
	lspDataFolds
)

// lspHintsDoneMsg is the inlay hints of the document at path.
type lspHintsDoneMsg struct {
	path  string
	hints []InlayHint
	err   error
}

// lspTokensDoneMsg is the semantic tokens of the document at path.
type lspTokensDoneMsg struct {
	path   string
	tokens []SemanticToken
	err    error
}

// lspFoldsDoneMsg is the folding ranges of the document at path.
type lspFoldsDoneMsg struct {
	path   string
	ranges []FoldRange
	err    error
}

// lspDataArm starts the debounce timer of one LSP data refresh. A newer timer wins;
// the sequence numbers are kept apart from the text debounce's so the two cannot
// consume each other's ticks.
func (m *Model) lspDataArm(kind lspDataKind) tea.Cmd {
	s := &m.lsp
	if s.port == nil {
		return nil
	}
	doc := m.ed.Active()
	if doc == nil || docPath(doc) == "" || isLargeDoc(doc) {
		return nil
	}
	tr := s.tracks[docPath(doc)]
	if tr == nil || tr.state != lspOpen {
		return nil
	}
	if kind == lspDataHints {
		if !s.inlay || s.hintErr[m.lspLanguage(doc)] != "" {
			return nil
		}
		s.hintSeq++
		seq := s.hintSeq
		return tea.Tick(lspHintDebounce, func(time.Time) tea.Msg { return lspDataMsg{kind: lspDataHints, seq: seq} })
	}
	if kind == lspDataFolds {
		if s.foldErr[m.lspLanguage(doc)] != "" {
			return nil
		}
		s.foldSeq++
		seq := s.foldSeq
		return tea.Tick(lspHintDebounce, func(time.Time) tea.Msg { return lspDataMsg{kind: lspDataFolds, seq: seq} })
	}
	s.tokenSeq++
	seq := s.tokenSeq
	return tea.Tick(lspTokenDebounce, func(time.Time) tea.Msg { return lspDataMsg{kind: lspDataTokens, seq: seq} })
}

// lspDataRequest asks the port for the data the armed timer wanted. It runs in a
// command, because the port may wait on a server.
func (m *Model) lspDataRequest(kind lspDataKind) tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		return nil
	}
	path := docPath(doc)
	port := m.lsp.port
	switch kind {
	case lspDataHints:
		return func() tea.Msg {
			hints, err := port.InlayHints(path)
			return lspHintsDoneMsg{path: path, hints: hints, err: err}
		}
	case lspDataTokens:
		// Ask for the visible lines with a little padding, so a small scroll does
		// not need another round trip.
		top := doc.Top()
		first := max(top-lspTokenPadding, 0)
		last := top + m.focusedBodyRows() + lspTokenPadding
		return func() tea.Msg {
			tokens, err := port.SemanticTokens(path, first, last)
			return lspTokensDoneMsg{path: path, tokens: tokens, err: err}
		}
	case lspDataFolds:
		return func() tea.Msg {
			ranges, err := port.FoldingRanges(path)
			return lspFoldsDoneMsg{path: path, ranges: ranges, err: err}
		}
	}
	return nil
}

// lspTokenPadding is the number of lines a semantic-token request asks for above
// and below the viewport.
const lspTokenPadding = 20

// lspHints stores the hints of the document they were asked for. A server that does
// not serve inlay hints stops the refreshes for its language, and the toggle says
// why once.
func (m *Model) lspHints(msg lspHintsDoneMsg) {
	s := &m.lsp
	if msg.err != nil {
		doc := m.ed.Active()
		lang := ""
		if doc != nil {
			lang = m.lspLanguage(doc)
		}
		if lang == "" || s.hintErr == nil {
			s.hintErr = make(map[string]string)
		}
		if s.hintErr[lang] == "" {
			s.hintErr[lang] = msg.err.Error()
			m.notice = "Inlay hints: " + msg.err.Error()
		}
		return
	}
	if !s.inlay {
		return
	}
	if s.hints == nil {
		s.hints = make(map[string][]InlayHint)
	}
	if len(msg.hints) == 0 {
		delete(s.hints, msg.path)
		return
	}
	s.hints[msg.path] = msg.hints
}

// lspTokens stores the tokens of the document they were asked for. The reply of a
// document that is no longer open is dropped.
func (m *Model) lspTokens(msg lspTokensDoneMsg) {
	s := &m.lsp
	if msg.err != nil {
		doc := m.ed.Active()
		lang := ""
		if doc != nil {
			lang = m.lspLanguage(doc)
		}
		if lang == "" || s.tokenErr == nil {
			s.tokenErr = make(map[string]string)
		}
		s.tokenErr[lang] = msg.err.Error()
		return
	}
	if _, ok := m.lspDocFor(msg.path); !ok {
		return
	}
	if s.tokens == nil {
		s.tokens = make(map[string][]SemanticToken)
	}
	if len(msg.tokens) == 0 {
		delete(s.tokens, msg.path)
		return
	}
	s.tokens[msg.path] = msg.tokens
}

// lspDocFor reports whether path is an open tab of the editor.
func (m *Model) lspDocFor(path string) (Document, bool) {
	tabs, ok := m.ed.(tabDocs)
	if !ok {
		return nil, false
	}
	for i := range len(m.ed.Tabs()) {
		doc := tabs.DocAt(i)
		if doc != nil && docPath(doc) == path {
			return doc, true
		}
	}
	return nil, false
}

// lspDataRefresh is the armed refresh timer firing: the newest timer of its kind
// starts the request, an older one does nothing.
func (m *Model) lspDataRefresh(msg lspDataMsg) tea.Cmd {
	switch msg.kind {
	case lspDataHints:
		if !m.lsp.inlay || msg.seq != m.lsp.hintSeq {
			return nil
		}
	case lspDataTokens:
		if msg.seq != m.lsp.tokenSeq {
			return nil
		}
	case lspDataFolds:
		if msg.seq != m.lsp.foldSeq {
			return nil
		}
	}
	return m.lspDataRequest(msg.kind)
}

// lspInlayText is the end-of-line text of the inlay hints of a line: the hints
// joined by a middle dot, each preceded by a blank, so they sit beside the text
// and never over it.
func lspInlayText(hints []InlayHint) string {
	if len(hints) == 0 {
		return ""
	}
	text := ""
	for i, h := range hints {
		if i > 0 {
			text += " · "
		}
		text += h.Text
	}
	return text
}

// lspDataDecoration wraps the pane's overlay hooks with the LSP data of doc: inlay
// hints as end-of-line virtual text and semantic tokens as styled spans. The hooks
// of the pane are composed, never replaced, so a plugin's decorations keep
// painting. The spans have priority -1: they paint below the plugin decorations
// (priority 0 and up) and above the syntax colors.
func (m *Model) lspData(pane editorpane.Pane, doc Document) editorpane.Pane {
	path := docPath(doc)
	hints := m.lsp.hints[path]
	tokens := m.lsp.tokens[path]
	if len(hints) == 0 && len(tokens) == 0 {
		return pane
	}
	hintByLine := make(map[int][]InlayHint, len(hints))
	for _, h := range hints {
		hintByLine[h.Line] = append(hintByLine[h.Line], h)
	}
	tokenByLine := make(map[int][]editorpane.StyledSpan, len(tokens))
	for _, tok := range tokens {
		tokenByLine[tok.Line] = append(tokenByLine[tok.Line], editorpane.StyledSpan{
			Start: tok.Start, End: tok.End, Style: m.semanticTokenStyle(tok), Priority: -1,
		})
	}
	if len(hints) > 0 {
		inner := pane.EOL
		hintStyle := m.panes.Text
		hintStyle.Faint = true
		pane.EOL = func(line int) []editorpane.VirtualText {
			var out []editorpane.VirtualText
			if inner != nil {
				out = append(out, inner(line)...)
			}
			if text := lspInlayText(hintByLine[line]); text != "" {
				out = append(out, editorpane.VirtualText{Text: " " + text, Style: hintStyle})
			}
			return out
		}
	}
	if len(tokens) > 0 {
		inner := pane.Overlays
		pane.Overlays = func(line int) []editorpane.StyledSpan {
			var out []editorpane.StyledSpan
			if inner != nil {
				out = append(out, inner(line)...)
			}
			return append(out, tokenByLine[line]...)
		}
	}
	return pane
}

// semanticTokenStyle maps a semantic token to the cell style, using the theme's
// syntax colors (the pane's syntax styles) as Fresh does: the token type picks the
// color, and the modifiers adjust the face. A deprecated token takes the warning
// color, a declaration is bold, a readonly one is underlined and an async one is
// italic. These are the same theme keys the syntax highlighter uses, so a theme
// needs nothing new.
func (m *Model) semanticTokenStyle(tok SemanticToken) grid.Style {
	kind := syntax.KindVariable
	switch tok.Type {
	case "keyword", "modifier":
		kind = syntax.KindKeyword
	case "function", "method", "macro", "decorator":
		kind = syntax.KindFunction
	case "parameter", "variable", "property", "enumMember", "event", "label":
		kind = syntax.KindVariable
	case "type", "class", "interface", "struct", "typeParameter", "namespace", "enum":
		kind = syntax.KindType
	case "number":
		kind = syntax.KindConstant
	case "string", "regexp":
		kind = syntax.KindString
	case "operator":
		kind = syntax.KindOperator
	case "comment":
		kind = syntax.KindComment
	}
	st := m.panes.Syntax[kind]
	for _, mod := range tok.Modifiers {
		switch mod {
		case "deprecated":
			if m.panes.DiagWarning.FG != nil {
				st.FG = m.panes.DiagWarning.FG
			}
		case "declaration":
			st.Bold = true
		case "readonly":
			st.Underline = true
		case "async":
			st.Italic = true
		}
	}
	return st
}

// lspFolds stores the server's folding ranges and pushes them into the editor,
// which merges them with its indentation folds. A language whose server does not
// serve folding ranges stops the refreshes for that language.
func (m *Model) lspFolds(msg lspFoldsDoneMsg) {
	s := &m.lsp
	if msg.err != nil {
		lang := ""
		if doc := m.ed.Active(); doc != nil {
			lang = m.lspLanguage(doc)
		}
		if s.foldErr == nil {
			s.foldErr = make(map[string]string)
		}
		if s.foldErr[lang] == "" {
			s.foldErr[lang] = msg.err.Error()
		}
		return
	}
	if s.folds == nil {
		s.folds = make(map[string][]FoldRange)
	}
	s.folds[msg.path] = msg.ranges
	setter, ok := m.ed.(externalFoldSetter)
	if !ok {
		return
	}
	ranges := make([]fold.Range, 0, len(msg.ranges))
	for _, r := range msg.ranges {
		ranges = append(ranges, fold.Range{Start: r.StartLine, End: r.EndLine})
	}
	setter.SetExternalFolds(msg.path, ranges)
}

// externalFoldSetter is the optional editor part that takes a language server's
// folding ranges. The app adapter implements it; the editor merges them with its
// indentation folds, so the gutter and the fold actions see one set.
type externalFoldSetter interface {
	SetExternalFolds(path string, ranges []fold.Range)
}

// lspDropData forgets the LSP paint data of a document that closed: its inlay
// hints, semantic tokens and external folding ranges. A later open asks for them
// again.
func (m *Model) lspDropData(path string) {
	delete(m.lsp.hints, path)
	delete(m.lsp.tokens, path)
	delete(m.lsp.folds, path)
	if setter, ok := m.ed.(externalFoldSetter); ok {
		setter.SetExternalFolds(path, nil)
	}
}
