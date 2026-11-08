package editor

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/buffer"
)

// markdownTabSize is the indent unit of Fresh's markdown_source plugin: the plugin
// hardcodes four spaces (TAB_SIZE) and ignores tab_size and use_tabs, and so does
// gvim here, so a Markdown list matches Fresh's even under a language entry that
// changes the indent.
const markdownTabSize = 4

// The list markers Fresh's parseListMarker recognises. The checkbox and unordered
// forms need the blank after the bullet, and the checkbox one the blank after the
// bracket, as the plugin's expressions do.
var (
	markdownCheckboxRe  = regexp.MustCompile(`^([-*+]) \[([ xX])\] (.*)$`)
	markdownOrderedRe   = regexp.MustCompile(`^(\d+)\. (.*)$`)
	markdownUnorderedRe = regexp.MustCompile(`^([-*+]) (.*)$`)
)

// markdownListKind is the kind of a list item, Fresh's ListMarkerInfo type.
type markdownListKind int

const (
	markdownUnordered markdownListKind = iota
	markdownOrdered
	markdownCheckbox
)

// markdownList is a list marker parsed from the text before the caret, and the
// content after it that the text held, Fresh's ListMarkerInfo.
type markdownList struct {
	kind    markdownListKind
	indent  string // the leading blanks
	bullet  rune   // '-' '*' '+' for an unordered item or a checkbox
	number  int    // the number of an ordered item
	checked bool   // the state of a checkbox
	content string // the text after the marker, which may be empty
}

// markdownSource reports whether d gets the smart editing of Fresh's
// markdown_source plugin: a Markdown file by the plugin's suffixes, or a buffer
// whose language id is markdown or multimarkdown. The plugin runs in source view
// only, and gvim has no other view.
func (e *Editor) markdownSource(d *Document) bool {
	if d == nil {
		return false
	}
	if p := d.Path; strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".markdown") || strings.HasSuffix(p, ".mdx") {
		return true
	}
	switch e.langOf(e.SettingsFor(d)) {
	case "markdown", "multimarkdown":
		return true
	}
	return false
}

// parseMarkdownList parses the text of a line, from its start up to the caret, as a
// list item. It mirrors Fresh's parseListMarker: the leading blanks are the indent
// and the rest of the text must start with a checkbox, ordered or unordered marker.
func parseMarkdownList(prefix string) (markdownList, bool) {
	indent := prefix[:len(prefix)-len(strings.TrimLeft(prefix, " \t"))]
	rest := prefix[len(indent):]
	if m := markdownCheckboxRe.FindStringSubmatch(rest); m != nil {
		return markdownList{kind: markdownCheckbox, indent: indent, bullet: rune(m[1][0]), checked: m[2] != " ", content: m[3]}, true
	}
	if m := markdownOrderedRe.FindStringSubmatch(rest); m != nil {
		number, err := strconv.Atoi(m[1])
		if err != nil {
			return markdownList{}, false
		}
		return markdownList{kind: markdownOrdered, indent: indent, number: number, content: m[2]}, true
	}
	if m := markdownUnorderedRe.FindStringSubmatch(rest); m != nil {
		return markdownList{kind: markdownUnordered, indent: indent, bullet: rune(m[1][0]), content: m[2]}, true
	}
	return markdownList{}, false
}

// blank reports whether the line holds only its marker, the plugin's test for an
// empty list item: the content before the caret trims to nothing, and so does the
// text after it.
func (m markdownList) blank(after string) bool {
	return strings.TrimSpace(m.content) == "" && strings.TrimSpace(after) == ""
}

// nextMarker is the marker Enter inserts for the item's continuation: the same
// indent and bullet, one more number for an ordered item, and an unchecked checkbox
// for a checkbox.
func (m markdownList) nextMarker() string {
	switch m.kind {
	case markdownOrdered:
		return m.indent + strconv.Itoa(m.number+1) + ". "
	case markdownCheckbox:
		return m.indent + string(m.bullet) + " [ ] "
	default:
		return m.indent + string(m.bullet) + " "
	}
}

// bulletText is a rewritten marker with bullet written: an unordered item's
// "B ", or a checkbox's "B [x] "/"B [ ] " that keeps the checked state.
func (m markdownList) bulletText(bullet rune) string {
	if m.kind != markdownCheckbox {
		return string(bullet) + " "
	}
	check := " "
	if m.checked {
		check = "x"
	}
	return string(bullet) + " [" + check + "] "
}

// cycleMarkdownBullet moves a bullet through Fresh's cycle: * -> - -> + -> *
// forward, and * -> + -> - -> * in reverse.
func cycleMarkdownBullet(bullet rune, forward bool) rune {
	if forward {
		switch bullet {
		case '*':
			return '-'
		case '-':
			return '+'
		case '+':
			return '*'
		}
		return '-'
	}
	switch bullet {
	case '*':
		return '+'
	case '+':
		return '-'
	case '-':
		return '*'
	}
	return '-'
}

// markdownLineAt splits the caret's line at col into its runes, the text before the
// caret and the text after it.
func markdownLineAt(d *Document, col int) (line []rune, before, after string) {
	line = []rune(d.Buffer.Line(d.Cursor.Line))
	col = min(max(col, 0), len(line))
	return line, string(line[:col]), string(line[col:])
}

// leadingWhitespace returns the blanks at the start of s.
func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

// replaceMarkdownLine replaces the caret's line's text with text and leaves the
// caret at its end, as the plugin's deleteRange, insertText and setBufferCursor do.
func replaceMarkdownLine(d *Document, text string) {
	d.EditAtCarets(func(c caret) edit {
		from := buffer.Pos{Line: c.Head.Line}
		to := buffer.Pos{Line: c.Head.Line, Col: len([]rune(d.Buffer.Line(c.Head.Line)))}
		return edit{From: from, To: to, Text: text}
	})
}

// markdownEnter runs the markdown-source Enter rule and reports whether it handled
// the line break. With a selection or extra carets it steps aside: the plugin falls
// back to the built-in insert_newline for multiple cursors, and its marker parse
// only knows the primary caret's line.
func (e *Editor) markdownEnter(d *Document) bool {
	if len(d.Extra) > 0 || d.HasSelection() || !e.markdownSource(d) {
		return false
	}
	line, before, after := markdownLineAt(d, d.Cursor.Col)
	if m, ok := parseMarkdownList(before); ok {
		if m.blank(after) {
			// Enter on an empty list item removes the marker, leaving a blank line.
			d.EditAtCarets(func(c caret) edit {
				from := buffer.Pos{Line: c.Head.Line}
				return edit{From: from, To: buffer.Pos{Line: c.Head.Line, Col: len(line)}}
			})
			return true
		}
		d.InsertText("\n" + m.nextMarker())
		return true
	}
	d.InsertText("\n" + leadingWhitespace(before))
	return true
}

// markdownTab runs the markdown-source Tab rule and reports whether it handled the
// key. On a blank unordered item or checkbox it indents the marker by four spaces and
// cycles the bullet forward; anywhere else it inserts four spaces. A selection or
// extra carets are handled by the caller with the built-in Tab, as the plugin does.
func (e *Editor) markdownTab(d *Document) bool {
	if len(d.Extra) > 0 || d.VirtualColumns() > 0 || !e.markdownSource(d) {
		return false
	}
	_, before, after := markdownLineAt(d, d.Cursor.Col)
	if m, ok := parseMarkdownList(before); ok && m.kind != markdownOrdered && m.blank(after) {
		text := m.indent + strings.Repeat(" ", markdownTabSize) + m.bulletText(cycleMarkdownBullet(m.bullet, true))
		replaceMarkdownLine(d, text)
		return true
	}
	d.InsertText(strings.Repeat(" ", markdownTabSize))
	return true
}

// markdownDedent runs the markdown-source Shift+Tab rule and reports whether it
// handled the key: on a blank unordered item or checkbox with an indent of at least
// four blanks it removes four blanks and cycles the bullet in reverse. Everywhere
// else the caller falls through to dedent_selection. The plugin does not guard this
// one for extra carets or a selection; gvim steps aside then, because the rewrite
// only names the caret's line.
func (e *Editor) markdownDedent(d *Document) bool {
	if len(d.Extra) > 0 || d.HasSelection() || d.VirtualColumns() > 0 || !e.markdownSource(d) {
		return false
	}
	_, before, after := markdownLineAt(d, d.Cursor.Col)
	m, ok := parseMarkdownList(before)
	if !ok || m.kind == markdownOrdered || !m.blank(after) || len(m.indent) < markdownTabSize {
		return false
	}
	text := m.indent[markdownTabSize:] + m.bulletText(cycleMarkdownBullet(m.bullet, false))
	replaceMarkdownLine(d, text)
	return true
}
