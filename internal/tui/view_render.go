package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
)

// defaultGuideGlyph is the indentation guide when editor.indentation_guide_glyph is empty.
const defaultGuideGlyph = '▏'

// docLanguageSettings returns the effective settings of doc's language: its name and
// path go to the resolver. A document that has neither gets the language-less settings.
func (m *Model) docLanguageSettings(doc Document) editor.LanguageSettings {
	var language, path string
	if l, ok := doc.(interface{ Language() string }); ok {
		language = l.Language()
	}
	if p, ok := doc.(pathEditor); ok {
		path = p.FilePath()
	}
	return m.languageSettings(language, path)
}

// viewRender sets the editor.* display options on the pane of doc: relative numbers,
// the hanging indent and wrap column, the indentation guides, the whitespace marks and
// the bracket highlights. s are the document's own view settings, which win over the
// config.
func (m *Model) viewRender(p editorpane.Pane, doc Document, s editor.ViewSettings) editorpane.Pane {
	o := m.view.opts
	ls := m.docLanguageSettings(doc)
	p.Opts.RelativeLineNumbers = o.RelativeLineNumbers
	p.Opts.WrapIndent = o.WrapIndent
	p.Opts.WrapColumn = ls.WrapColumn
	p.Opts.HighlightBrackets = o.HighlightMatchingBrackets
	p.Opts.RainbowBrackets = o.HighlightMatchingBrackets && o.RainbowBrackets

	mode := editor.ResolveIndentGuide(o.IndentationGuideMode, s.IndentGuides, ls.IndentationGuide)
	p.IndentGuides = mode != editor.GuidesNone
	p.Opts.GuideActive = mode == editor.GuidesActive
	p.Opts.RainbowGuides = o.RainbowIndentation
	p.Opts.GuideGlyph = defaultGuideGlyph
	if g := strings.TrimSpace(o.IndentationGuideGlyph); g != "" {
		p.Opts.GuideGlyph, _ = utf8.DecodeRuneInString(g)
	}

	ws := editorpane.Whitespace{InSelection: o.Whitespace.InSelection}
	if p.VisibleWhitespace {
		ws.SpacesLeading, ws.SpacesInner, ws.SpacesTrailing = o.Whitespace.SpacesLeading, o.Whitespace.SpacesInner, o.Whitespace.SpacesTrailing
		ws.TabsLeading, ws.TabsInner, ws.TabsTrailing = ls.WhitespaceTabsLeading, ls.WhitespaceTabsInner, ls.WhitespaceTabsTrailing
		ws.Newlines, ws.CarriageReturns = o.Whitespace.Newlines, o.Whitespace.CarriageReturn
		if s.Whitespace != nil && *s.Whitespace && !m.view.whitespace {
			// The buffer switched the marks on over a config that has them off: mark every
			// space, or the switch would look inert in a space-indented file.
			ws.SpacesLeading, ws.SpacesInner, ws.SpacesTrailing = true, true, true
		}
	}
	p.Opts.Whitespace = &ws
	return p
}
