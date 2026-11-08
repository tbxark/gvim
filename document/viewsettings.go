package document

// ViewSettings are the display settings of one document, changed by the
// per-buffer view actions. A flag is an override: a nil pointer means the
// document follows the UI's global setting, or the editor's default where the
// UI has none (see the accessors below).
type ViewSettings struct {
	LineNumbers    *bool // the line-number gutter
	LineWrap       *bool
	CurrentLine    *bool // the tint of the caret line
	Occurrences    *bool // the highlight of the word's other occurrences
	Whitespace     *bool // dots for spaces and arrows for tabs
	IndentGuides   *bool // default off
	FoldIndicators *bool // default on
	TabIndicators  *bool // default on; arrows for tabs, under Whitespace
	VirtualSpace   *bool // default off; the caret may sit past the end of lines (see virtualspace.go)
	IndentTabs     *bool // true: Tab inserts tabs; nil follows Options.UseTabs
	// PageView is stored only: the pane does not draw the page view. The page
	// view belongs to the Markdown compose/rendered view, which is deferred to its
	// own round (docs/parity.md backlog #12), so the flag is kept for the config
	// and the toggle but has no paint.
	PageView bool
	// PageWidth is the width of the page view in columns. Zero means the default.
	PageWidth int
	// Rulers are the 1-based columns that carry a ruler, in the order added.
	Rulers []int
	// TabSize is the columns per tab stop of this document. Zero means
	// Options.TabSize.
	TabSize int
}

// Bounds of the values the per-buffer view actions accept.
const (
	MaxViewTabSize   = 16
	MaxViewColumn    = 1000
	DefaultPageWidth = 80
)

// OverrideOr returns the value of an override, or def when there is none.
func OverrideOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// IndentGuidesOn reports whether indentation guides are drawn. The default is off.
func (s ViewSettings) IndentGuidesOn() bool { return OverrideOr(s.IndentGuides, false) }

// FoldIndicatorsOn reports whether the fold arrows are drawn. The default is on.
func (s ViewSettings) FoldIndicatorsOn() bool { return OverrideOr(s.FoldIndicators, true) }

// TabIndicatorsOn reports whether tabs are drawn as arrows. The default is on.
func (s ViewSettings) TabIndicatorsOn() bool { return OverrideOr(s.TabIndicators, true) }

// VirtualSpaceOn reports the per-buffer virtual space switch. The default is off;
// when it is unset the editor's editor.virtual_space decides (see
// document/virtualspace.go and editor/virtualspace.go).
func (s ViewSettings) VirtualSpaceOn() bool { return OverrideOr(s.VirtualSpace, false) }

// ViewSettings returns the display settings of the document. The flags are
// shared with the document, so the caller reads them and does not change them.
func (d *Document) ViewSettings() ViewSettings {
	return d.view
}
