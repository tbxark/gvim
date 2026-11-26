package reviewdiff

import (
	"image/color"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// Default semantic accent colors for diff gutter signs and file-status badges
// when the active theme does not provide an explicit override.
var (
	defaultAddFG       = color.RGBA{R: 80, G: 200, B: 120, A: 255}
	defaultRemoveFG    = color.RGBA{R: 220, G: 90, B: 90, A: 255}
	defaultModifyFG    = color.RGBA{R: 220, G: 160, B: 90, A: 255}
	defaultRenameFG    = color.RGBA{R: 100, G: 180, B: 240, A: 255}
	defaultUntrackedFG = color.RGBA{R: 120, G: 200, B: 140, A: 255}
	defaultConflictFG  = color.RGBA{R: 240, G: 100, B: 100, A: 255}
)

// Styles holds every cell style the Review Diff Mode panels paint with.
// NewStyles resolves them once from a theme.Theme.
type Styles struct {
	Chrome style.Styles

	Text           grid.Style
	Muted          grid.Style
	Gutter         grid.Style
	CurrentLine    grid.Style
	Selection      grid.Style
	SelectionBlur  grid.Style
	Accent         grid.Style
	HeaderBar      grid.Style
	HeaderPill     grid.Style
	HeaderPillHot  grid.Style
	HeaderWarning  grid.Style
	HunkHeader     grid.Style
	HunkHeaderCur  grid.Style
	Filler         grid.Style
	SplitBorder    grid.Style
	Scrollbar      grid.Style
	ScrollbarThumb grid.Style
	Match          grid.Style
	CurrentMatch   grid.Style
	InputField     grid.Style
	InputCaret     grid.Style

	DiffAddBG          color.Color
	DiffRemoveBG       color.Color
	DiffAddHighlight   color.Color
	DiffRemoveHighlight color.Color
	DiffModifyBG       color.Color

	AddFG       color.Color
	RemoveFG    color.Color
	ModifyFG    color.Color
	RenameFG    color.Color
	UntrackedFG color.Color
	ConflictFG  color.Color

	Syntax [syntax.KindCount]grid.Style
}

// NewStyles resolves Review Diff Mode styles from th.
func NewStyles(th theme.Theme) Styles {
	chrome := style.New(th)
	text := style.TaggedCell(chrome.Editor, "editor", "fg", "editor", "bg")
	gutter := style.TaggedCell(chrome.Gutter, "editor", "line_number_fg", "editor", "line_number_bg")
	curLine := style.TaggedCell(chrome.CurrentLine, "", "", "editor", "current_line_bg")
	accent := style.TaggedCell(chrome.Accent, "ui", "accent_fg", "ui", "accent_bg")
	tabBar := style.TaggedCell(chrome.TabBar, "ui", "tab_inactive_fg", "ui", "tab_separator_bg")
	tabActive := style.TaggedCell(chrome.TabActive, "ui", "tab_active_fg", "ui", "tab_active_bg")
	if tabBar.FG == nil {
		tabBar.FG = text.FG
	}

	muted := text
	muted.Faint = true

	selBlur := text
	if curLine.BG != nil {
		selBlur.BG = curLine.BG
		selBlur.BGKey = curLine.BGKey
	} else {
		selBlur.Reverse = true
	}

	hunkHeader := gutter
	if sticky := style.Cell(chrome.Sticky); sticky.BG != nil {
		hunkHeader.BG = sticky.BG
	}
	if hunkHeader.FG == nil {
		hunkHeader.FG = text.FG
		hunkHeader.Faint = true
	}
	hunkHeaderCur := hunkHeader
	hunkHeaderCur.Bold = true
	if accent.BG != nil {
		hunkHeaderCur.FG = accent.BG
	}

	filler := gutter
	filler.BG = text.BG
	filler.Faint = true

	splitBorder := gutter
	splitBorder.BG = text.BG

	headerPill := tabBar
	headerPill.BG = tabActive.BG
	if headerPill.FG == nil {
		headerPill.FG = tabActive.FG
	}
	headerPillHot := accent
	headerPillHot.Bold = true

	headerWarn := tabBar
	if fg := style.Cell(chrome.DiagWarning).FG; fg != nil {
		headerWarn.FG = fg
	}
	headerWarn.Bold = true

	inputField := chrome.Overlay.Idle
	if inputField.BG == nil {
		inputField = selBlur
	}
	inputCaret := inputField
	inputCaret.Reverse = !inputCaret.Reverse

	st := Styles{
		Chrome:         chrome,
		Text:           text,
		Muted:          muted,
		Gutter:         gutter,
		CurrentLine:    curLine,
		Selection:      accent,
		SelectionBlur:  selBlur,
		Accent:         accent,
		HeaderBar:      tabBar,
		HeaderPill:     headerPill,
		HeaderPillHot:  headerPillHot,
		HeaderWarning:  headerWarn,
		HunkHeader:     hunkHeader,
		HunkHeaderCur:  hunkHeaderCur,
		Filler:         filler,
		SplitBorder:    splitBorder,
		Scrollbar:      style.TaggedCell(chrome.Scrollbar, "ui", "scrollbar_track_fg", "editor", "bg"),
		ScrollbarThumb: style.TaggedCell(chrome.ScrollbarThumb, "ui", "scrollbar_thumb_fg", "editor", "bg"),
		Match:          style.TaggedCell(chrome.Match, "search", "match_fg", "search", "match_bg"),
		CurrentMatch:   style.TaggedCell(chrome.CurrentMatch, "search", "current_match_fg", "search", "current_match_bg"),
		InputField:     inputField,
		InputCaret:     inputCaret,

		DiffAddBG:           style.Color(th, "editor", "diff_add_bg"),
		DiffRemoveBG:        style.Color(th, "editor", "diff_remove_bg"),
		DiffAddHighlight:    style.Color(th, "editor", "diff_add_highlight_bg"),
		DiffRemoveHighlight: style.Color(th, "editor", "diff_remove_highlight_bg"),
		DiffModifyBG:        style.Color(th, "editor", "diff_modify_bg"),

		AddFG:       colorOr(style.Color(th, "ui", "file_status_added_fg"), defaultAddFG),
		RemoveFG:    colorOr(style.Color(th, "ui", "file_status_deleted_fg"), defaultRemoveFG),
		ModifyFG:    colorOr(style.Color(th, "ui", "file_status_modified_fg"), defaultModifyFG),
		RenameFG:    colorOr(style.Color(th, "ui", "file_status_renamed_fg"), defaultRenameFG),
		UntrackedFG: colorOr(style.Color(th, "ui", "file_status_untracked_fg"), defaultUntrackedFG),
		ConflictFG:  colorOr(style.Color(th, "ui", "file_status_conflicted_fg"), defaultConflictFG),
	}

	for k := range st.Syntax {
		kind := syntax.Kind(k)
		st.Syntax[k] = text
		if kind == syntax.KindPlain || kind == syntax.KindDiffAdd || kind == syntax.KindDiffRemove {
			continue
		}
		st.Syntax[k].FG = style.SyntaxFG(th, kind, text.FG)
		attrs := kind.Attrs()
		st.Syntax[k].Bold = attrs&syntax.AttrBold != 0
		st.Syntax[k].Italic = attrs&syntax.AttrItalic != 0
		st.Syntax[k].Underline = attrs&syntax.AttrUnderline != 0
		st.Syntax[k].Strike = attrs&syntax.AttrStrike != 0
		if c, ok := th.Syntax(kind.ThemeKey()); ok {
			style.ApplyAttributeNames(&st.Syntax[k], c.Modifiers)
		}
	}
	return st
}

// StatusFG returns the foreground color for a changed file status badge.
func (s Styles) StatusFG(st FileStatus) color.Color {
	switch st {
	case StatusAdded:
		return s.AddFG
	case StatusDeleted:
		return s.RemoveFG
	case StatusModified:
		return s.ModifyFG
	case StatusRenamed:
		return s.RenameFG
	case StatusUntracked:
		return s.UntrackedFG
	case StatusConflicted:
		return s.ConflictFG
	default:
		return s.Text.FG
	}
}

func colorOr(c, fallback color.Color) color.Color {
	if c != nil {
		return c
	}
	return fallback
}
