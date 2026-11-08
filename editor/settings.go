package editor

import (
	"slices"

	"github.com/tbxark/gvim/document"
)

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
	if !d.Ext().Virtual {
		d.View().Rulers = slices.Clone(e.rulers)
	}
}

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
		s = e.languages(d.Language(), d.Path)
	}
	if d == nil {
		return s
	}
	if d.View().TabSize > 0 {
		s.TabSize = d.View().TabSize
	}
	if d.View().IndentTabs != nil {
		s.UseTabs = *d.View().IndentTabs
	}
	if d.View().LineWrap != nil {
		s.LineWrap = *d.View().LineWrap
	}
	if d.View().PageView {
		s.PageView = true
	}
	if d.View().PageWidth > 0 {
		s.PageWidth = d.View().PageWidth
	}
	return s
}

// documentSettings returns the document half of the options: the values a
// Document reads while it edits and saves.
func (o Options) documentSettings() document.Settings {
	return document.Settings{
		TrimTrailingWhitespace:  o.TrimTrailingWhitespace,
		EnsureFinalNewline:      o.EnsureFinalNewline,
		UseTabs:                 o.UseTabs,
		TabSize:                 o.TabSize,
		AutoIndent:              o.AutoIndent,
		AutoClose:               o.AutoClose,
		AutoSurround:            o.AutoSurround,
		DefaultLineEnding:       o.DefaultLineEnding,
		AutoReadOnly:            o.AutoReadOnly,
		SnapshotInterval:        o.SnapshotInterval,
		LargeFileThresholdBytes: o.LargeFileThresholdBytes,
		EstimatedLineLength:     o.EstimatedLineLength,
		LargeFileMode:           o.LargeFileMode,
	}
}
