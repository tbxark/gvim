package main

import (
	"fmt"
	"html"
	"image/color"
	"strings"

	"github.com/tbxark/gvim/internal/terminal"
)

// Default colours of a cell that gvim did not colour: the dark theme's defaults.
var (
	defaultFg = color.RGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	defaultBg = color.RGBA{R: 0x10, G: 0x10, B: 0x12, A: 0xff}
)

// frameHTML renders a screen as one line of spans per row. A run of cells with the
// same style is one span, and every colour is written out in full. The cursor cell is
// drawn inverted while the cursor is visible.
func frameHTML(sc terminal.Screen) string {
	var b strings.Builder
	for row := range sc.Rows {
		open := ""
		for col := range sc.Cols {
			c := sc.Cell(row, col)
			cursor := sc.Cursor.Visible && sc.Cursor.Row == row && sc.Cursor.Col == col
			style := cellCSS(c, cursor)
			if style != open {
				if open != "" {
					b.WriteString("</span>")
				}
				b.WriteString(`<span style="` + style + `">`)
				open = style
			}
			// A cell with no rune is drawn as a space. That includes the second cell
			// of a wide character, so wide characters are not aligned yet.
			text := " "
			if c.Rune != 0 {
				text = string(c.Rune)
			}
			b.WriteString(html.EscapeString(text))
		}
		if open != "" {
			b.WriteString("</span>")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// cellCSS is the inline style of a cell: its colours, bold and underline. Reverse
// video and the cursor both swap the colours, so both together swap them back.
func cellCSS(c terminal.Cell, cursor bool) string {
	fg, bg := colorOf(c.Style.Fg, defaultFg), colorOf(c.Style.Bg, defaultBg)
	if c.Style.Reverse != cursor {
		fg, bg = bg, fg
	}
	s := fmt.Sprintf("color:%s;background:%s;", hexColor(fg), hexColor(bg))
	if c.Style.Bold {
		s += "font-weight:bold;"
	}
	if c.Style.Underline {
		s += "text-decoration:underline;"
	}
	return s
}

// colorOf converts a cell colour to RGBA. A nil colour is the terminal default, def.
func colorOf(c color.Color, def color.RGBA) color.RGBA {
	if c == nil {
		return def
	}
	return color.RGBAModel.Convert(c).(color.RGBA)
}

// hexColor writes c as a CSS hex colour.
func hexColor(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
