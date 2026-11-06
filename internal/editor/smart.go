package editor

import (
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// Smart typing: auto-close, skip-over, surround, electric closers, Enter between
// brackets and smart Backspace. The rules follow Fresh (input/actions.rs:
// insert_char_events, handle_insert_newline, handle_delete_backward). They run for
// every caret, through the same paths as plain typing.

// StringContext reports whether the character at p of d lies inside a string
// literal, as the syntax highlighter classifies it. The editor does not import the
// highlighter, so the app supplies this. Without it, quotes are never suppressed.
type StringContext func(d *Document, p buffer.Pos) bool

// SetStringContext sets the query that decides whether a typed quote is inside a
// string. Typing a quote there inserts only the quote, as Fresh does.
func (e *Editor) SetStringContext(f StringContext) { e.inString = f }

// langOf returns the language id that the quote and markdown exceptions test: the
// id of the settings, where an unknown language reads as plain text once a resolver
// is installed (Fresh names every buffer without a detected language "text").
func (e *Editor) langOf(s LanguageSettings) string {
	if s.Language == "" && e.languages != nil {
		return "text"
	}
	return s.Language
}

// pairFor returns the closer that typing ch inserts, for the languages Fresh
// pairs in (get_auto_close_char). Plain text pairs no quotes, and markdown no '.
func pairFor(ch rune, enabled bool, lang string) (rune, bool) {
	if !enabled {
		return 0, false
	}
	quote := ch == '"' || ch == '\'' || ch == '`'
	if lang == "text" && quote {
		return 0, false
	}
	if (lang == "markdown" || lang == "mdx") && ch == '\'' {
		return 0, false
	}
	switch ch {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case '"', '\'', '`':
		return ch, true
	}
	return 0, false
}

// isCloser reports whether ch is a closing bracket, the characters that re-indent a
// line as they are typed.
func isCloser(ch rune) bool { return ch == '}' || ch == ')' || ch == ']' }

// asciiWordRune is Fresh's test for "alphanumeric after the caret": auto-close does
// not fire in front of a word.
func asciiWordRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// placeFn maps the end of an edit's inserted text, in the final document, to where
// the caret goes. Nil puts the caret at that end.
type placeFn func(end buffer.Pos) buffer.Pos

// editPlaced makes one edit per caret and puts each caret where its place function
// says. With extra carets the edits are one undo step, as editAtCarets makes them.
// A single caret edits the buffer directly, so its typing still merges into the
// undo step of the typing around it. When the planned edits overlap, which two
// carets in one line's leading blanks can cause, plain gives the edits instead.
func (d *Document) editPlaced(plan func(c caret) (edit, placeFn), plain func(c caret) edit) {
	cs := d.allCarets()
	before := d.Cursor
	edits := make([]edit, len(cs))
	places := make([]placeFn, len(cs))
	for i, c := range cs {
		edits[i], places[i] = plan(c)
	}
	for i := 1; i < len(edits); i++ {
		if edits[i].from.Less(edits[i-1].to) {
			for j, c := range cs {
				edits[j], places[j] = plain(c), nil
			}
			break
		}
	}
	var ends []buffer.Pos
	if d.planErr != nil {
		// The planning failed (see failPlan): no edit is made.
		ends = make([]buffer.Pos, len(cs))
	} else if len(cs) == 1 {
		x := edits[0]
		if x.from != x.to {
			d.Buffer.Delete(x.from, x.to)
		}
		end := x.from
		if x.text != "" && !d.editRefused() {
			// A refused delete leaves the text in place, so nothing is typed over it.
			end = d.Buffer.Insert(x.from, x.text)
		}
		ends = []buffer.Pos{end}
	} else {
		ends = d.applyEdits(edits)
	}
	if d.editRefused() {
		return
	}
	out := make([]caret, len(cs))
	for i, c := range cs {
		p := ends[i]
		if places[i] != nil {
			p = places[i](p)
		}
		out[i] = caret{anchor: p, head: p, primary: c.primary}
	}
	d.setCarets(out)
	if len(cs) > 1 {
		d.noteMultiEdit(before, edits)
	}
}

// typeText types text at every caret. A single rune goes through the smart rules
// of the document's language settings. Longer text, which a paste or a plugin
// sends, is inserted as it is.
func (e *Editor) typeText(d *Document, text string) {
	plain := func(c caret) edit { return replaceEdit(c, text) }
	r, size := utf8.DecodeRuneInString(text)
	s := e.SettingsFor(d)
	rules := indentRulesOf(s)
	// A language with a decrease rule watches every typed character for the keyword
	// that completes a dedent trigger.
	electric := s.AutoIndent && rules != nil && rules.decrease != nil
	smart := size == len(text) && (s.AutoClose || s.AutoSurround || (s.AutoIndent && isCloser(r)) || electric)
	if !smart {
		if len(d.extra) == 0 {
			d.insertText(text)
			return
		}
		d.editAtCarets(plain)
		return
	}
	tab := e.tabSizeOf(d)
	d.editPlaced(func(c caret) (edit, placeFn) { return e.planChar(d, s, rules, tab, c, r) }, plain)
}

// planChar plans typing ch at one caret, in Fresh's order: wrap a selection, step
// over a closer that is already there, fix the indent of a typed closer, dedent a
// line that the character makes a trigger keyword, close a typed opener, or type the
// character.
func (e *Editor) planChar(d *Document, s LanguageSettings, rules *indentRules, tab int, c caret, ch rune) (edit, placeFn) {
	lo, hi := c.bounds()
	lang := e.langOf(s)
	text := string(ch)
	if lo != hi {
		if closer, ok := pairFor(ch, s.AutoSurround, lang); ok {
			return edit{from: lo, to: hi, text: text + d.planRange(lo, hi) + string(closer)}, nil
		}
	}
	after, hasAfter := d.Buffer.RuneAt(hi)
	line := []rune(d.Buffer.Line(lo.Line))
	atIndent := blankPrefix(line, lo.Col) && lo.Col > 0
	step := func(end buffer.Pos) buffer.Pos { return buffer.Pos{Line: end.Line, Col: end.Col + 1} }

	if s.AutoClose && hasAfter && after == ch && (ch != '\'' || (lang != "markdown" && lang != "mdx")) {
		switch ch {
		case ')', ']', '}', '"', '\'', '`':
			if isCloser(ch) && atIndent {
				// Fix the indent of the line, then step over the closer.
				want := e.closerIndentFor(d, rules, lo, ch, tab)
				if leadColumns(line, lo.Col, tab) != want {
					return edit{from: buffer.Pos{Line: lo.Line}, to: hi, text: indentString(want, s.UseTabs, tab)}, step
				}
			}
			return edit{from: lo, to: hi}, step
		}
	}
	if isCloser(ch) && s.AutoIndent && atIndent {
		want := e.closerIndentFor(d, rules, lo, ch, tab)
		return edit{from: buffer.Pos{Line: lo.Line}, to: hi, text: indentString(want, s.UseTabs, tab) + text}, nil
	}
	if s.AutoIndent && rules != nil && lo == hi && !isCloser(ch) {
		if x, ok := e.keywordDedent(d, rules, s, tab, lo, ch); ok {
			return x, nil
		}
	}
	if closer, ok := pairFor(ch, s.AutoClose, lang); ok {
		quote := ch == '"' || ch == '\'' || ch == '`'
		inside := quote && e.inString != nil && e.inString(d, lo)
		if !inside && (!hasAfter || !asciiWordRune(after)) {
			back := func(end buffer.Pos) buffer.Pos { return buffer.Pos{Line: end.Line, Col: end.Col - 1} }
			return edit{from: lo, to: hi, text: text + string(closer)}, back
		}
	}
	return edit{from: lo, to: hi, text: text}, nil
}

// newline breaks the line at every caret, replacing each selection. With
// auto_indent the new line gets the indent Fresh's heuristic gives it, and with
// auto_close a caret between a bracket pair opens a three-line block with the
// closer on its own line.
func (e *Editor) newline(d *Document) {
	s := e.SettingsFor(d)
	tab := e.tabSizeOf(d)
	plain := func(c caret) edit { return replaceEdit(c, "\n") }
	if !s.AutoIndent {
		if len(d.extra) == 0 {
			d.deleteSelection()
			d.insertText("\n")
			return
		}
		d.editAtCarets(plain)
		return
	}
	plainText := e.langOf(s) == "text"
	rules := indentRulesOf(s)
	d.editPlaced(func(c caret) (edit, placeFn) {
		lo, hi := c.bounds()
		width := 0
		if rules != nil {
			width = rules.newLineIndent(e.indentViewOf(d, tab), lo)
		} else {
			width = newLineIndent(d.Buffer, lo, tab, plainText)
		}
		indent := indentString(width, s.UseTabs, tab)
		text := "\n" + indent
		before, hasBefore := d.Buffer.RuneAt(buffer.Pos{Line: lo.Line, Col: lo.Col - 1})
		after, hasAfter := d.Buffer.RuneAt(lo)
		if !s.AutoClose || !hasBefore || !hasAfter || !pairedBrackets(before, after) {
			return edit{from: lo, to: hi, text: text}, nil
		}
		opener := []rune(d.Buffer.Line(lo.Line))
		text += "\n" + indentString(leadColumns(opener, len(opener), tab), s.UseTabs, tab)
		middle := func(end buffer.Pos) buffer.Pos {
			return buffer.Pos{Line: end.Line - 1, Col: utf8.RuneCountInString(indent)}
		}
		return edit{from: lo, to: hi, text: text}, middle
	}, plain)
}

// pairedBrackets reports whether open and closer are a matching ( ) [ ] or { }.
func pairedBrackets(open, closer rune) bool {
	return (open == '(' && closer == ')') || (open == '[' && closer == ']') || (open == '{' && closer == '}')
}

// pairedQuotes reports whether the two runes are a pair that auto-close inserts:
// brackets, or the same quote twice.
func pairedQuotes(open, closer rune) bool {
	return pairedBrackets(open, closer) ||
		((open == '"' || open == '\'' || open == '`') && open == closer)
}

// backspace removes each selection. With no selection it removes, at each caret,
// one indent unit when only blanks precede the caret (a tab, or up to tab_size
// spaces), both characters of an empty pair that auto_close inserted, or else the
// character before the caret, which may be a line break.
func (e *Editor) backspace(d *Document) {
	s := e.SettingsFor(d)
	tab := e.tabSizeOf(d)
	plan := func(c caret) edit {
		if lo, hi := c.bounds(); lo != hi {
			return edit{from: lo, to: hi}
		}
		head := c.head
		prev, ok := prevPos(d.Buffer, head)
		if !ok {
			return edit{from: head, to: head}
		}
		line := []rune(d.Buffer.Line(head.Line))
		if head.Col > 0 && blankPrefix(line, head.Col) {
			n := 1
			if line[head.Col-1] != '\t' {
				n = 0
				for i := head.Col - 1; i >= 0 && line[i] == ' ' && n < tab; i-- {
					n++
				}
			}
			return edit{from: buffer.Pos{Line: head.Line, Col: head.Col - n}, to: head}
		}
		if s.AutoClose && head.Col > 0 && head.Col < len(line) && pairedQuotes(line[head.Col-1], line[head.Col]) {
			return edit{from: prev, to: buffer.Pos{Line: head.Line, Col: head.Col + 1}}
		}
		return edit{from: prev, to: head}
	}
	// Carets closer together than an indent unit plan edits that overlap, so then
	// every caret removes one character, which never does.
	single := func(c caret) edit {
		if lo, hi := c.bounds(); lo != hi {
			return edit{from: lo, to: hi}
		}
		prev, _ := prevPos(d.Buffer, c.head)
		return edit{from: prev, to: c.head}
	}
	d.editPlaced(func(c caret) (edit, placeFn) { return plan(c), nil }, single)
}
