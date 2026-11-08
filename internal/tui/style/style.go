// Package style builds the chrome styles of gvim from an internal/theme value.
// It is the only package besides the components that turns theme keys into
// colors. Each role in Styles names the theme keys it reads. A key that is
// missing, or that names the terminal default, leaves that side of the style to
// the terminal.
//
// Components paint with lipgloss styles. Cell converts one of them into grid
// attributes for the cell surface.
package style

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/theme"
)

// Styles is the set of chrome roles. Each field's comment names the theme keys
// it reads, as "section.key" for the foreground and background.
type Styles struct {
	Editor         lipgloss.Style // editor.fg on editor.bg
	Selection      lipgloss.Style // editor.fg on editor.selection_bg
	Gutter         lipgloss.Style // editor.line_number_fg on editor.line_number_bg
	Menu           lipgloss.Style // ui.menu_fg on ui.menu_bg
	Accent         lipgloss.Style // ui.accent_fg on ui.accent_bg, the one accent of the theme
	TabActive      lipgloss.Style // ui.tab_active_fg on ui.tab_active_bg, the active tab
	Overlay        Overlay        // the overlay boxes, from ui.popup and the accent
	TabBar         lipgloss.Style // ui.tab_separator_bg, the strip behind the tabs
	Tab            lipgloss.Style // ui.tab_inactive_fg on ui.tab_inactive_bg
	Explorer       lipgloss.Style // editor.fg on editor.bg, the panel body
	ExplorerTitle  lipgloss.Style // ui.menu_fg on ui.menu_bg, the panel header
	Status         lipgloss.Style // ui.status_bar_fg on ui.status_bar_bg
	Scrollbar      lipgloss.Style // ui.scrollbar_track_fg on editor.bg
	ScrollbarThumb lipgloss.Style // ui.scrollbar_thumb_fg on editor.bg
	Match          lipgloss.Style // search.match_bg, the background only
	CurrentMatch   lipgloss.Style // search.current_match_fg on search.current_match_bg
	CurrentLine    lipgloss.Style // editor.current_line_bg, the background only
	SecondaryCaret lipgloss.Style // editor.fg on editor.inactive_cursor
	Ruler          lipgloss.Style // editor.ruler_bg, the background only
	Whitespace     lipgloss.Style // editor.whitespace_indicator_fg on editor.bg
	Occurrence     lipgloss.Style // ui.semantic_highlight_bg, the background only
	DiagError      lipgloss.Style // diagnostic.error_fg, the foreground only
	DiagWarning    lipgloss.Style // diagnostic.warning_fg, the foreground only
	DiagInfo       lipgloss.Style // diagnostic.info_fg, the foreground only
	DiagHint       lipgloss.Style // diagnostic.hint_fg, the foreground only
	Decor          Decor          // guides, bracket highlights and selected whitespace marks
}

// Theme keys of the roles that the editor pane and the status bar paint with.
// The bundled themes must define each one, or the role paints with the
// terminal default. style_test.go checks that they do.
var (
	refCurrentLine    = ref{"editor", "current_line_bg"}
	refInactiveCursor = ref{"editor", "inactive_cursor"}
	refRuler          = ref{"editor", "ruler_bg"}
	refWhitespace     = ref{"editor", "whitespace_indicator_fg"}
	refOccurrence     = ref{"ui", "semantic_highlight_bg"}
	refDiagError      = ref{"diagnostic", "error_fg"}
	refDiagWarning    = ref{"diagnostic", "warning_fg"}
	refDiagInfo       = ref{"diagnostic", "info_fg"}
	refDiagHint       = ref{"diagnostic", "hint_fg"}
	refPopupText      = ref{"ui", "popup_text_fg"}
	refPopupBG        = ref{"ui", "popup_bg"}
	refAccentFG       = ref{"ui", "accent_fg"}
	refAccentBG       = ref{"ui", "accent_bg"}
)

// ref names one theme value: a section and a key. An empty key means none.
type ref struct{ section, key string }

// New returns the chrome styles for th. A zero theme gives unstyled roles.
func New(th theme.Theme) Styles {
	ed := ref{"editor", "bg"}
	return Styles{
		Editor:         build(th, ref{"editor", "fg"}, ed),
		Selection:      build(th, ref{"editor", "fg"}, ref{"editor", "selection_bg"}),
		Gutter:         build(th, ref{"editor", "line_number_fg"}, ref{"editor", "line_number_bg"}),
		Menu:           build(th, ref{"ui", "menu_fg"}, ref{"ui", "menu_bg"}),
		Accent:         build(th, refAccentFG, refAccentBG),
		TabActive:      build(th, ref{"ui", "tab_active_fg"}, ref{"ui", "tab_active_bg"}),
		Overlay:        NewOverlay(th),
		TabBar:         build(th, ref{}, ref{"ui", "tab_separator_bg"}),
		Tab:            build(th, ref{"ui", "tab_inactive_fg"}, ref{"ui", "tab_inactive_bg"}),
		Explorer:       build(th, ref{"editor", "fg"}, ed),
		ExplorerTitle:  build(th, ref{"ui", "menu_fg"}, ref{"ui", "menu_bg"}),
		Status:         build(th, ref{"ui", "status_bar_fg"}, ref{"ui", "status_bar_bg"}),
		Scrollbar:      build(th, ref{"ui", "scrollbar_track_fg"}, ed),
		ScrollbarThumb: build(th, ref{"ui", "scrollbar_thumb_fg"}, ed),
		Match:          build(th, ref{}, ref{"search", "match_bg"}),
		CurrentMatch:   build(th, ref{"search", "current_match_fg"}, ref{"search", "current_match_bg"}),
		CurrentLine:    build(th, ref{}, refCurrentLine),
		SecondaryCaret: build(th, ref{"editor", "fg"}, refInactiveCursor),
		Ruler:          build(th, ref{}, refRuler),
		Whitespace:     build(th, refWhitespace, ed),
		Occurrence:     build(th, ref{}, refOccurrence),
		DiagError:      build(th, refDiagError, ref{}),
		DiagWarning:    build(th, refDiagWarning, ref{}),
		DiagInfo:       build(th, refDiagInfo, ref{}),
		DiagHint:       build(th, refDiagHint, ref{}),
		Decor:          newDecor(th),
	}
}

// build returns a style with the foreground from fg, the background from bg,
// and the modifiers of both values: a theme value's attributes apply to the
// text drawn over its color, so an attribute on a *_bg color reaches the cell
// too.
func build(th theme.Theme, fg, bg ref) lipgloss.Style {
	st := lipgloss.NewStyle()
	if fg.key != "" {
		if c, ok := th.Value(fg.section, fg.key); ok {
			if col := toColor(c); col != nil {
				st = st.Foreground(col)
			}
			st = modifiers(st, c.Modifiers)
		}
	}
	if bg.key != "" {
		if c, ok := th.Value(bg.section, bg.key); ok {
			if col := toColor(c); col != nil {
				st = st.Background(col)
			}
			st = modifiers(st, c.Modifiers)
		}
	}
	return st
}

// modifiers applies the style modifiers a theme value carries. Names that are
// not known are ignored.
func modifiers(st lipgloss.Style, names []string) lipgloss.Style {
	for _, n := range names {
		switch strings.ToLower(n) {
		case "bold":
			st = st.Bold(true)
		case "italic":
			st = st.Italic(true)
		case "underline", "underlined":
			st = st.Underline(true)
		case "reverse", "reversed":
			st = st.Reverse(true)
		case "dim", "dimmed", "faint":
			st = st.Faint(true)
		}
	}
	return st
}

// Color returns the color of section and key in th. It returns nil when the key
// is missing, names the terminal default, or names a color that is not one of
// the sixteen ANSI names.
func Color(th theme.Theme, section, key string) color.Color {
	c, ok := th.Value(section, key)
	if !ok {
		return nil
	}
	return toColor(c)
}

// RenderedColor returns the color a theme value paints as: the RGB for a fixed
// color, the ANSI palette entry for a named one, nil for the terminal default.
func RenderedColor(c theme.Color) color.Color { return toColor(c) }

// MatchesRendered reports whether c is what value paints as. The theme inspector
// uses it to name the keys behind a frame cell.
func MatchesRendered(c color.Color, value theme.Color) bool {
	rendered := toColor(value)
	if rendered == nil || c == nil {
		return rendered == nil && c == nil
	}
	return rendered == c
}

// toColor converts one theme value. RGB values become opaque colors. Names
// become the ANSI palette entry the terminal draws in its own palette.
func toColor(c theme.Color) color.Color {
	if c.IsRGB {
		return color.RGBA{R: c.RGB.R, G: c.RGB.G, B: c.RGB.B, A: 0xff}
	}
	if c.IsTerminalDefault() {
		return nil
	}
	if i, ok := ansiNames[c.Name]; ok {
		return lipgloss.Color(strconv.Itoa(i))
	}
	return nil
}

// ansiNames maps the color names of ratatui, which theme files use, to the
// sixteen ANSI palette indexes. "Gray" is index 7 and "White" is index 15, as
// ratatui numbers them.
var ansiNames = map[string]int{
	"Black":        0,
	"Red":          1,
	"Green":        2,
	"Yellow":       3,
	"Blue":         4,
	"Magenta":      5,
	"Cyan":         6,
	"Gray":         7,
	"DarkGray":     8,
	"LightRed":     9,
	"LightGreen":   10,
	"LightYellow":  11,
	"LightBlue":    12,
	"LightMagenta": 13,
	"LightCyan":    14,
	"White":        15,
}

// Cell converts a lipgloss style into the attributes of a grid cell. A color
// the style does not set becomes nil, which the terminal draws in its default.
func Cell(st lipgloss.Style) grid.Style {
	return grid.Style{
		FG:        definedColor(st.GetForeground()),
		BG:        definedColor(st.GetBackground()),
		Bold:      st.GetBold(),
		Faint:     st.GetFaint(),
		Italic:    st.GetItalic(),
		Underline: st.GetUnderline(),
		Reverse:   st.GetReverse(),
	}
}

// definedColor maps lipgloss's "no color" value to nil.
func definedColor(c color.Color) color.Color {
	if c == nil {
		return nil
	}
	if _, none := c.(lipgloss.NoColor); none {
		return nil
	}
	return c
}

// Overlay is the grid styles of the overlay boxes: the dialogs, the lists, the
// dropdowns, the popups, the palette and the browser. The box takes the theme's
// popup colors, and the highlight and the border take its accent, so every box
// looks alike within a theme.
type Overlay struct {
	Box      grid.Style // the fill of a box, and its text
	Muted    grid.Style // secondary text: hints, labels and shortcuts
	Error    grid.Style // an error message
	Border   grid.Style // the rounded border of a box, and the fill of its title
	Title    grid.Style // the title of a box
	Selected grid.Style // the highlighted row
	Focus    grid.Style // the focused button
	Idle     grid.Style // an unfocused button, a text field and a blurred selection
	Hover    grid.Style // a button under the pointer
}

// NewOverlay returns the overlay styles for th. The secondary roles are mixed from
// the popup text and fill when both are RGB. Otherwise they are drawn with
// attributes, so a theme in terminal colors still shows them.
func NewOverlay(th theme.Theme) Overlay {
	box := TaggedStyle(Cell(build(th, refPopupText, refPopupBG)), "ui", "popup_text_fg", "ui", "popup_bg")
	accent := TaggedStyle(Cell(build(th, refAccentFG, refAccentBG)), "ui", "accent_fg", "ui", "accent_bg")
	ov := Overlay{
		Box:      box,
		Error:    TaggedStyle(Cell(build(th, refDiagError, refPopupBG)), "diagnostic", "error_fg", "ui", "popup_bg"),
		Border:   TaggedStyle(Cell(build(th, refAccentBG, refPopupBG)), "ui", "accent_bg", "ui", "popup_bg"),
		Title:    box,
		Selected: gridStyleWithKeys(accent, KeyIDFor("ui", "accent_fg"), KeyIDFor("ui", "accent_bg")),
		Focus:    gridStyleWithKeys(focusAccent(accent), KeyIDFor("ui", "accent_fg"), KeyIDFor("ui", "accent_bg")),
	}
	ov.Title.Bold = true
	fill, okFill := rgbOf(Color(th, "ui", "popup_bg"))
	text, okText := rgbOf(Color(th, "ui", "popup_text_fg"))
	if okFill && okText {
		ov.Muted = gridStyleWithKeys(grid.Style{FG: toward(text, fill, 0.45), BG: fill}, KeyIDFor("ui", "popup_text_fg"), KeyIDFor("ui", "popup_bg"))
		ov.Idle = gridStyleWithKeys(grid.Style{FG: text, BG: toward(fill, text, 0.25)}, KeyIDFor("ui", "popup_text_fg"), KeyIDFor("ui", "popup_bg"))
		ov.Hover = gridStyleWithKeys(grid.Style{FG: text, BG: toward(fill, text, 0.4)}, KeyIDFor("ui", "popup_text_fg"), KeyIDFor("ui", "popup_bg"))
	} else {
		ov.Muted = box
		ov.Muted.Faint = true
		ov.Idle = box
		ov.Idle.Reverse = true
		ov.Hover = box
		ov.Hover.Reverse = true
		ov.Hover.Bold = true
	}
	return ov
}

// rgbOf returns c as an RGB color. It is false for a nil color and for an ANSI
// name, which cannot be mixed with another color.
func rgbOf(c color.Color) (color.RGBA, bool) {
	rgb, ok := c.(color.RGBA)
	return rgb, ok
}

// toward moves from t of the way toward to. t is 0 at from and 1 at to.
func toward(from, to color.RGBA, t float64) color.RGBA {
	mix := func(a, b uint8) uint8 {
		return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5)
	}
	return color.RGBA{R: mix(from.R, to.R), G: mix(from.G, to.G), B: mix(from.B, to.B), A: 0xFF}
}

// PaintBox draws an overlay box of w by h cells at (x, y). The box is filled with
// Box and framed by a rounded Border. Its title is written on the top border, after
// the first cell; the rest of the border stays the plain rounded rule. The rows
// inside the border are the caller's.
//
// The title is kept bare: any surrounding blanks are trimmed, and the cell after
// the corner stays a padding cell. A title too wide for the box is cut with an
// ellipsis so it never runs over the right corner. An untitled box keeps the plain
// rule all the way across.
func (o Overlay) PaintBox(g *grid.Grid, x, y, w, h int, title string) {
	if w < 2 || h < 1 {
		return
	}
	for row := y; row < y+h; row++ {
		g.Fill(x, x+w, row, o.Box)
	}
	b := lipgloss.RoundedBorder()
	right, bottom := x+w-1, y+h-1
	for cx := x + 1; cx < right; cx++ {
		g.Set(cx, y, b.Top, o.Border)
		g.Set(cx, bottom, b.Bottom, o.Border)
	}
	for ry := y + 1; ry < bottom; ry++ {
		g.Set(x, ry, b.Left, o.Border)
		g.Set(right, ry, b.Right, o.Border)
	}
	g.Set(x, y, b.TopLeft, o.Border)
	g.Set(right, y, b.TopRight, o.Border)
	g.Set(x, bottom, b.BottomLeft, o.Border)
	g.Set(right, bottom, b.BottomRight, o.Border)

	// The title sits on the plain rule: an untitled menu and a titled card read as the
	// same frame, without an extra decoration on the top border.
	title = strings.TrimSpace(title)
	if title != "" {
		title = ansi.Truncate(title, max(w-4, 1), "…")
		g.Put(x+2, y, right, " "+title+" ", o.Title)
	}
}

// PaintFooter writes hints on the bottom border row of a box x columns wide at
// column x. The hints start after the same one-cell padding as the title and
// stop before the right corner, in the Muted style; a hint too long for the box
// is cut with an ellipsis.
//
// Cards put their key hints here instead of on a row of their own, so every
// floating surface has the same frame: a title on the top border, the content,
// and the hints on the bottom border.
func (o Overlay) PaintFooter(g *grid.Grid, x, y, w int, hints string) {
	hints = strings.TrimSpace(hints)
	if hints == "" || w <= 4 {
		return
	}
	room := w - 3 // from x+2 up to the cell before the right corner
	g.Put(x+2, y, x+w-1, space.End(" "+hints+" ", room), o.Muted)
}
