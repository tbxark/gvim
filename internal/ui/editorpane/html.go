package editorpane

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// maxHTMLBytes bounds the HTML a copy builds. A selection whose text is longer is
// copied as plain text.
const maxHTMLBytes = 4 << 20

// SelectionHTML renders text as an HTML <pre> block with the syntax colours of st, for
// pasting into rich text editors, as Fresh's copy with formatting does. The colours come
// from the selection of doc when the text it covers is text. Without a selection, or when
// the text differs (several carets, another line ending), the text is not coloured.
// ok is false when the text is empty or too long.
func SelectionHTML(doc Document, st Styles, text string) (html string, ok bool) {
	if text == "" || len(text) > maxHTMLBytes {
		return "", false
	}
	kinds := selectionKinds(doc, text)
	var b strings.Builder
	fg, bg := cssColor(st.Text.FG, "#d4d4d4"), cssColor(st.Text.BG, "#1e1e1e")
	fmt.Fprintf(&b, "<pre style=\"background-color:%s;color:%s;font-family:'Fira Mono','Fira Code',Consolas,'Courier New',monospace;font-size:14px;padding:12px 16px;border-radius:6px;margin:0;white-space:pre;overflow-x:auto;\">", bg, fg)
	cur := syntax.KindPlain
	open := false
	closeSpan := func() {
		if open {
			b.WriteString("</span>")
			open = false
		}
	}
	i := 0
	for _, r := range text {
		k := syntax.KindPlain
		if i < len(kinds) {
			k = kinds[i]
		}
		i++
		if k != cur {
			closeSpan()
			cur = k
			if k != syntax.KindPlain {
				fmt.Fprintf(&b, "<span style=\"%s\">", kindCSS(st.Syntax[k], fg))
				open = true
			}
		}
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	closeSpan()
	b.WriteString("</pre>")
	return b.String(), true
}

// selectionKinds returns the syntax kind of each rune of text when text is what the
// primary selection of doc covers, and nil otherwise.
func selectionKinds(doc Document, text string) []syntax.Kind {
	if doc == nil {
		return nil
	}
	a, h, ok := doc.Selection()
	if !ok {
		return nil
	}
	if h.Less(a) {
		a, h = h, a
	}
	if a.Line < 0 || h.Line >= doc.LineCount() {
		return nil
	}
	var plain strings.Builder
	var kinds []syntax.Kind
	for i := a.Line; i <= h.Line; i++ {
		line := doc.Line(i)
		runes := []rune(line)
		from, to := 0, len(runes)
		if i == a.Line {
			from = min(max(a.Col, 0), len(runes))
		}
		if i == h.Line {
			to = min(max(h.Col, from), len(runes))
		}
		lineKinds := kindsPerRune(line, doc.LineSpans(i))
		kinds = append(kinds, lineKinds[from:to]...)
		plain.WriteString(string(runes[from:to]))
		if i < h.Line {
			plain.WriteByte('\n')
			kinds = append(kinds, syntax.KindPlain)
		}
		if plain.Len() > maxHTMLBytes {
			return nil
		}
	}
	if plain.String() != strings.ReplaceAll(text, "\r\n", "\n") {
		return nil
	}
	if strings.Contains(text, "\r\n") {
		// The text keeps its CRLF, so each break is two runes.
		out := make([]syntax.Kind, 0, len(kinds)+8)
		for j, r := range []rune(plain.String()) {
			out = append(out, kinds[j])
			if r == '\n' {
				out = append(out, syntax.KindPlain)
			}
		}
		return out
	}
	return kinds
}

// kindCSS is the inline style of a syntax kind: its colour, and its weight and slant.
func kindCSS(st grid.Style, def string) string {
	css := "color:" + cssColor(st.FG, def) + ";"
	if st.Bold {
		css += "font-weight:bold;"
	}
	if st.Italic {
		css += "font-style:italic;"
	}
	if st.Underline {
		css += "text-decoration:underline;"
	}
	return css
}

// cssColor is c as #rrggbb, or def for the terminal default (nil).
func cssColor(c color.Color, def string) string {
	if c == nil {
		return def
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
