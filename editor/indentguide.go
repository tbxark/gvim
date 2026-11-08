package editor

// The values of editor.indentation_guide.
const (
	GuidesNone   = "none"
	GuidesAll    = "all"
	GuidesActive = "active"
)

// ResolveIndentGuide returns the indentation guide mode that a document draws: the
// global mode (empty reads as none), the per-buffer override of ViewSettings.IndentGuides,
// and whether the document's language allows guides (LanguageSettings.IndentationGuide).
// An override of true turns guides on in a buffer where the global mode is none, as
// every level; an override of false turns them off. Without an override, a language
// that does not allow guides, such as plain text, draws none.
func ResolveIndentGuide(global string, override *bool, languageAllows bool) string {
	switch global {
	case GuidesAll, GuidesActive:
	default:
		global = GuidesNone
	}
	switch {
	case override != nil && *override:
		if global == GuidesNone {
			return GuidesAll
		}
		return global
	case override != nil, !languageAllows:
		return GuidesNone
	}
	return global
}
