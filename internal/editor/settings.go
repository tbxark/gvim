package editor

import "slices"

// Values of Options.VirtualSpace, as the config key editor.virtual_space writes them.
const (
	VirtualSpaceOff   = "off"
	VirtualSpaceBlock = "block"
	VirtualSpaceOn    = "on"
)

// LanguageSettings are the effective editing and display settings of one document:
// the global config options with the override of the document's language applied
// (config.Config.EditorFor). The app builds them; the editor does not read config.
//
// The editor consumes TabSize, UseTabs, AutoIndent, AutoClose, AutoSurround and
// CommentPrefix; the other fields are not consumed yet. SettingsFor says which layer
// wins when a document also has a per-buffer override in ViewSettings.
type LanguageSettings struct {
	// Language is the language id the settings were resolved for, such as "go" or
	// "text". It is lowercase and may be empty.
	Language string

	TabSize      int  // columns per tab stop
	UseTabs      bool // Tab inserts a tab character
	AutoIndent   bool // insert_newline indents the new line
	AutoClose    bool // typing an opener inserts its closer
	AutoSurround bool // typing an opener over a selection wraps the selection

	LineWrap   bool
	WrapColumn int // text column at which lines wrap; 0 means the viewport width
	PageView   bool
	PageWidth  int // page view width; 0 means the viewport width

	// WhitespaceTabs* say whether tab marks show at the start, inside and at the end of
	// a line once whitespace display is on. They are all false for a language that sets
	// show_whitespace_tabs to false, such as Go.
	WhitespaceTabsLeading  bool
	WhitespaceTabsInner    bool
	WhitespaceTabsTrailing bool

	// IndentationGuide is false when indentation guides are suppressed for this
	// document whatever the global mode is. Plain text defaults to false.
	IndentationGuide bool

	CommentPrefix  string // line comment prefix; empty means the editor's own table
	WordCharacters string // extra word characters; empty means letters, digits and "_"

	FormatOnSave bool
	Formatter    *FormatterSpec
	OnSave       []OnSaveAction
	Indent       *IndentRules
}

// FormatterSpec is languages.<id>.formatter.
type FormatterSpec struct {
	Command   string
	Args      []string
	Stdin     bool
	TimeoutMS int
}

// OnSaveAction is one entry of languages.<id>.on_save.
type OnSaveAction struct {
	Command    string
	Args       []string
	WorkingDir string
	Stdin      bool
	TimeoutMS  int
	Enabled    bool
}

// IndentRules is languages.<id>.indent: regular expressions for the indentation of the
// next line. An empty pattern is unset.
type IndentRules struct {
	IncreaseIndentPattern string
	DecreaseIndentPattern string
	IndentNextLinePattern string
	DedentNextLinePattern string
	SelfClosePattern      string
}

// SetLanguageSettings sets the resolver that gives the per-language settings of a
// document (config languages.<id> merged over the global options). Nil means no language
// overrides: SettingsFor then returns the global options.
func (e *Editor) SetLanguageSettings(r SettingsResolver) { e.languages = r }

// SetEphemeralFilePatterns sets editor.ephemeral_file_patterns (Fresh default
// ["**/.git/**"]): the globs of files that the session and recovery do not keep. The
// slice is copied.
func (e *Editor) SetEphemeralFilePatterns(patterns []string) {
	e.ephemeral = append([]string(nil), patterns...)
}

// EphemeralFilePatterns returns the patterns that SetEphemeralFilePatterns set. The
// caller must not change the slice.
func (e *Editor) EphemeralFilePatterns() []string { return e.ephemeral }

// SetRulers sets editor.rulers: the 1-based columns that every text buffer starts with, the
// ones open already included. A buffer may then remove one of them, as Fresh's per-buffer
// rulers allow. The slice is copied.
func (e *Editor) SetRulers(cols []int) {
	e.rulers = slices.Clone(cols)
	for _, d := range e.docs {
		e.seedRulers(d)
	}
}

// seedRulers gives a text buffer the configured rulers, as its own starting list. A
// virtual buffer gets none: Fresh draws no rulers on those.
func (e *Editor) seedRulers(d *Document) {
	if !d.ext.virtual {
		d.view.Rulers = slices.Clone(e.rulers)
	}
}

// SettingsResolver returns the settings for a document with the given language name
// (Document.Language, which may be empty) and file path (which may be empty).
type SettingsResolver func(language, path string) LanguageSettings

// defaultLanguageSettings returns the settings of a document when SetLanguageSettings is
// nil: the global Options with no language override.
func (e *Editor) defaultLanguageSettings() LanguageSettings {
	return LanguageSettings{
		TabSize:                e.opts.TabSize,
		UseTabs:                e.opts.UseTabs,
		AutoIndent:             e.opts.AutoIndent,
		AutoClose:              e.opts.AutoClose,
		AutoSurround:           e.opts.AutoSurround,
		LineWrap:               true,
		PageWidth:              DefaultPageWidth,
		WhitespaceTabsLeading:  true,
		WhitespaceTabsInner:    true,
		WhitespaceTabsTrailing: true,
		IndentationGuide:       true,
	}
}

// SettingsFor returns the effective settings of d. The layers, lowest first, are the
// global Options, the override of the document's language (SetLanguageSettings, called with
// the document's language name and path), and the document's own ViewSettings. The last
// one replaces TabSize (when non-zero), UseTabs (IndentTabs), LineWrap, PageView and
// PageWidth (when non-zero) where the user changed them for this buffer with a view
// action. A nil d gives the language-less settings.
func (e *Editor) SettingsFor(d *Document) LanguageSettings {
	var s LanguageSettings
	switch {
	case e.languages == nil:
		s = e.defaultLanguageSettings()
	case d == nil:
		s = e.languages("", "")
	default:
		s = e.languages(d.language, d.Path)
	}
	if d == nil {
		return s
	}
	if d.view.TabSize > 0 {
		s.TabSize = d.view.TabSize
	}
	if d.view.IndentTabs != nil {
		s.UseTabs = *d.view.IndentTabs
	}
	if d.view.LineWrap != nil {
		s.LineWrap = *d.view.LineWrap
	}
	if d.view.PageView {
		s.PageView = true
	}
	if d.view.PageWidth > 0 {
		s.PageWidth = d.view.PageWidth
	}
	return s
}
