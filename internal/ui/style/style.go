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

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// Styles is the set of chrome roles. Each field's comment names the theme keys
// it reads, as "section.key" for the foreground and background.
type Styles struct {
	Editor         lipgloss.Style // editor.fg on editor.bg
	Selection      lipgloss.Style // editor.fg on editor.selection_bg
	Gutter         lipgloss.Style // editor.line_number_fg on editor.line_number_bg
	Menu           lipgloss.Style // ui.menu_fg on ui.menu_bg
	MenuActive     lipgloss.Style // ui.menu_active_fg on ui.menu_active_bg
	TabBar         lipgloss.Style // ui.tab_separator_bg, the strip behind the tabs
	Tab            lipgloss.Style // ui.tab_inactive_fg on ui.tab_inactive_bg
	TabActive      lipgloss.Style // ui.tab_active_fg on ui.tab_active_bg
	Explorer       lipgloss.Style // editor.fg on editor.bg, the panel body
	ExplorerTitle  lipgloss.Style // ui.menu_fg on ui.menu_bg, the panel header
	Status         lipgloss.Style // ui.status_bar_fg on ui.status_bar_bg
	StatusPalette  lipgloss.Style // ui.status_palette_fg on ui.status_palette_bg
	Scrollbar      lipgloss.Style // ui.scrollbar_track_fg on editor.bg
	ScrollbarThumb lipgloss.Style // ui.scrollbar_thumb_fg on editor.bg
}

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
		MenuActive:     build(th, ref{"ui", "menu_active_fg"}, ref{"ui", "menu_active_bg"}),
		TabBar:         build(th, ref{}, ref{"ui", "tab_separator_bg"}),
		Tab:            build(th, ref{"ui", "tab_inactive_fg"}, ref{"ui", "tab_inactive_bg"}),
		TabActive:      build(th, ref{"ui", "tab_active_fg"}, ref{"ui", "tab_active_bg"}),
		Explorer:       build(th, ref{"editor", "fg"}, ed),
		ExplorerTitle:  build(th, ref{"ui", "menu_fg"}, ref{"ui", "menu_bg"}),
		Status:         build(th, ref{"ui", "status_bar_fg"}, ref{"ui", "status_bar_bg"}),
		StatusPalette:  build(th, ref{"ui", "status_palette_fg"}, ref{"ui", "status_palette_bg"}),
		Scrollbar:      build(th, ref{"ui", "scrollbar_track_fg"}, ed),
		ScrollbarThumb: build(th, ref{"ui", "scrollbar_thumb_fg"}, ed),
	}
}

// build returns a style with the foreground from fg, the background from bg,
// and the modifiers of the foreground value ("bold" and so on).
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
		if col := Color(th, bg.section, bg.key); col != nil {
			st = st.Background(col)
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
